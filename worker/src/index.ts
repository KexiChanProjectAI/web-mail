import type {
	ExecutionContext,
	ForwardableEmailMessage,
} from "@cloudflare/workers-types";
import type { Env, EmailCache, ParseInput } from "./types";
import { Dao } from "./db";
import { parseEmail } from "./mail/parse";
import { renderEmail } from "./mail/render";
import { createTelegramBotAPI } from "./telegram/api";
import { getDefaultMailTtl } from "./handler/fetch";

// task 1: the canonical `Env` now lives in `src/types` so the upstream-style
// module surface (handler/mail, handler/fetch, db, mail/parse, telegram/api)
// can import the same type. Re-export it for backwards compatibility with any
// consumer that still does `import { Env } from './index'`.
export type { Env } from "./types";

// task 4 defaults: upstream-style MAX_EMAIL_SIZE / MAX_EMAIL_SIZE_POLICY.
// These match the upstream TBXark/mail2telegram contract.
const DEFAULT_MAX_EMAIL_SIZE = 524288; // 512 KiB
const DEFAULT_MAX_EMAIL_SIZE_POLICY: "truncate" = "truncate";

function resolveMaxEmailSize(env: Env): number {
	const raw = env.MAX_EMAIL_SIZE;
	if (!raw) return DEFAULT_MAX_EMAIL_SIZE;
	const parsed = Number.parseInt(raw, 10);
	if (Number.isFinite(parsed) && parsed > 0) return parsed;
	return DEFAULT_MAX_EMAIL_SIZE;
}

function resolveMaxEmailSizePolicy(
	env: Env,
): "unhandled" | "truncate" | "continue" {
	const p = env.MAX_EMAIL_SIZE_POLICY;
	if (p === "unhandled" || p === "truncate" || p === "continue") return p;
	return DEFAULT_MAX_EMAIL_SIZE_POLICY;
}

/**
 * Build a fresh `ReadableStream` from the collected raw MIME bytes so we
 * can re-parse the same payload with `postal-mime` (the raw stream is a
 * one-shot `ReadableStream` per `ForwardableEmailMessage`).
 */
function streamFromBytes(bytes: Uint8Array): ReadableStream<Uint8Array> {
	return new ReadableStream<Uint8Array>({
		start(controller) {
			controller.enqueue(bytes);
			controller.close();
		},
	});
}

/**
 * Preview + Telegram path. Independent of Go ingest: failures here are
 * logged and swallowed and MUST NOT call `message.setReject(...)`.
 *
 * Retransmits of the same message (Cloudflare retry after an ingest
 * reject) are suppressed via a KV marker keyed by Message-ID, or by
 * SHA-256 of the raw MIME when no stable Message-ID is present.
 */
const TG_SENT_KEY_PREFIX = "tg-sent:";

async function sha256Hex(bytes: Uint8Array): Promise<string> {
	const digest = await crypto.subtle.digest("SHA-256", bytes);
	return [...new Uint8Array(digest)]
		.map((b) => b.toString(16).padStart(2, "0"))
		.join("");
}

async function telegramSentKey(
	cache: EmailCache,
	raw: Uint8Array,
): Promise<string> {
	const id = cache.messageId.trim();
	if (id.length > 0 && id !== cache.id && id !== "unknown") {
		return `${TG_SENT_KEY_PREFIX}${id}`;
	}
	return `${TG_SENT_KEY_PREFIX}sha256:${await sha256Hex(raw)}`;
}

async function sendTelegramForCache(
	cache: EmailCache,
	env: Env,
	raw: Uint8Array,
): Promise<void> {
	const token = env.TELEGRAM_TOKEN;
	const ids = env.TELEGRAM_ID;
	if (!token || !ids) {
		// Opt-in: missing token or ids means Telegram is disabled.
		return;
	}

	const sentKey = await telegramSentKey(cache, raw);
	try {
		if (await env.DB.get(sentKey)) {
			console.log("Telegram already delivered for this message, skipping:", {
				sentKey,
				cacheId: cache.id,
			});
			return;
		}
	} catch (err) {
		console.error("Telegram dedup lookup failed; sending anyway:", {
			sentKey,
			cacheId: cache.id,
			error: err instanceof Error ? err.message : String(err),
		});
	}

	const api = createTelegramBotAPI(token);
	const rendered = await renderEmail(cache, env);
	let anyOk = false;
	for (const rawId of ids.split(",")) {
		const chatId = rawId.trim();
		if (!chatId) continue;
		try {
			await api.sendMessage({
				chat_id: chatId,
				text: rendered.text,
				parse_mode: rendered.parse_mode,
				reply_markup: rendered.reply_markup,
				link_preview_options: rendered.link_preview_options,
			});
			anyOk = true;
		} catch (err) {
			console.error("Telegram send failed:", {
				chatId,
				cacheId: cache.id,
				error: err instanceof Error ? err.message : String(err),
			});
		}
	}

	if (!anyOk) {
		return;
	}
	try {
		await env.DB.put(sentKey, cache.id, {
			expirationTtl: getDefaultMailTtl(env),
		});
	} catch (err) {
		console.error("Telegram dedup marker write failed:", {
			sentKey,
			cacheId: cache.id,
			error: err instanceof Error ? err.message : String(err),
		});
	}
}

class RejectableDeliveryError extends Error {
	reason: string;

	constructor(message: string, reason: string) {
		super(message);
		this.name = "RejectableDeliveryError";
		this.reason = reason;
	}
}

interface IngestMeta {
	from: string;
	to: string;
	rawSize: number;
	messageId: string | null;
	subject: string | null;
	ingestTarget?: string;
}

function buildIngestMeta(
	message: Pick<ForwardableEmailMessage, "from" | "to" | "rawSize" | "headers">,
	ingestUrl?: string,
): IngestMeta {
	return {
		from: message.from,
		to: message.to,
		rawSize: message.rawSize,
		messageId: message.headers.get("message-id"),
		subject: message.headers.get("subject"),
		ingestTarget: ingestUrl,
	};
}

async function collectStream(stream: ReadableStream): Promise<Uint8Array> {
	const reader = stream.getReader();
	const chunks: Uint8Array[] = [];

	while (true) {
		const { done, value } = await reader.read();
		if (done) break;
		chunks.push(value);
	}
	reader.releaseLock();

	const totalLength = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
	const result = new Uint8Array(totalLength);
	let offset = 0;
	for (const chunk of chunks) {
		result.set(chunk, offset);
		offset += chunk.length;
	}
	return result;
}

/**
 * Worker-owned preview + Telegram. Never throws; never rejects the
 * inbound message. Independent of the Go ingest outcome.
 */
async function deliverPreviewAndTelegram(
	cache: EmailCache,
	env: Env,
	raw: Uint8Array,
	meta: IngestMeta,
): Promise<void> {
	try {
		const dao = new Dao(env.DB);
		const ttl = getDefaultMailTtl(env);
		await dao.saveMailCache(cache.id, cache, ttl);
		console.log("Preview cache written:", { ...meta, cacheId: cache.id });
	} catch (err) {
		console.error("Preview cache write failed:", {
			...meta,
			cacheId: cache.id,
			error: err instanceof Error ? err.message : String(err),
		});
	}

	try {
		await sendTelegramForCache(cache, env, raw);
	} catch (err) {
		console.error("Telegram dispatch failed unexpectedly:", {
			...meta,
			cacheId: cache.id,
			error: err instanceof Error ? err.message : String(err),
		});
	}
}

/**
 * Durable ingest into Go. Throws `RejectableDeliveryError` so the
 * caller can `setReject` — that bounce is ingest's own retry signal
 * and does not gate Telegram (already started in parallel).
 */
async function deliverToGo(
	rawMIME: Uint8Array,
	env: Env,
	meta: IngestMeta,
): Promise<void> {
	const ingestUrl = env.INGEST_URL;
	const ingestPSK = env.WORKER_INGEST_PSK;
	if (!ingestUrl || !ingestPSK) {
		console.error("Missing required environment configuration:", {
			...meta,
			hasIngestUrl: !!ingestUrl,
			hasIngestPSK: !!ingestPSK,
		});
		throw new RejectableDeliveryError(
			"Worker misconfigured",
			"Worker misconfigured: missing ingest URL or PSK",
		);
	}

	console.log("Delivering email to ingest endpoint:", meta);
	const ingestStatus = await postToIngestWithStatus(
		ingestUrl,
		ingestPSK,
		rawMIME.buffer as ArrayBuffer,
		meta,
	);

	if (ingestStatus === "duplicate") {
		console.log("Duplicate email ignored:", meta);
		return;
	}

	if (ingestStatus !== "accepted") {
		console.warn(
			"Ingest response was not a recognized status; treating as accepted:",
			meta,
		);
	}
}

async function email(
	message: ForwardableEmailMessage,
	env: Env,
	_ctx: ExecutionContext,
): Promise<void> {
	const meta = buildIngestMeta(message, env.INGEST_URL);

	console.log("Email event received:", meta);

	let rawMIME: Uint8Array;
	try {
		rawMIME = await collectStream(message.raw);
		console.log("Email raw stream collected:", {
			...meta,
			collectedBytes: rawMIME.byteLength,
		});
	} catch (err) {
		console.error("Failed to read email raw stream:", err);
		message.setReject("Failed to read email content");
		return;
	}

	// Parse locally (best-effort) for the preview cache + Telegram render.
	// A parse failure does NOT block either delivery path.
	let cache: EmailCache;
	try {
		const messageForParse: ParseInput = {
			headers: message.headers,
			from: message.from,
			to: message.to,
			rawSize: message.rawSize,
			raw: streamFromBytes(rawMIME),
		};
		cache = await parseEmail(
			messageForParse,
			resolveMaxEmailSize(env),
			resolveMaxEmailSizePolicy(env),
		);
	} catch (err) {
		console.error("Local parse failed unexpectedly:", err);
		cache = {
			id: crypto.randomUUID(),
			messageId: message.headers.get("Message-ID")?.trim() ?? "unknown",
			from: message.from,
			to: message.to,
			subject: message.headers.get("Subject") ?? "",
			text: `Email body could not be parsed: ${err instanceof Error ? err.message : String(err)}`,
			html: `<p>Email body could not be parsed.</p>`,
		};
	}

	// Both paths start immediately. Ingest failure still setRejects so
	// Cloudflare retries durable storage; Telegram does not wait for Go
	// and Go does not wait for Telegram.
	const previewP = deliverPreviewAndTelegram(cache, env, rawMIME, meta);
	const ingestP = deliverToGo(rawMIME, env, meta).then(
		() => null as string | null,
		(err: unknown) =>
			err instanceof RejectableDeliveryError
				? err.reason
				: "Mail delivery failed: unexpected error",
	);

	const [, ingestReject] = await Promise.all([previewP, ingestP]);
	if (ingestReject) {
		console.error("Email delivery failed:", { ...meta, reason: ingestReject });
		message.setReject(ingestReject);
	}
}

type IngestAttemptResult =
	| { kind: "accepted" }
	| { kind: "duplicate" }
	| { kind: "unparseable-ok" }
	| { kind: "http-status"; status: number };

async function attemptIngest(
	url: string,
	psk: string,
	body: ArrayBuffer,
	meta: IngestMeta,
	signal: AbortSignal,
): Promise<IngestAttemptResult> {
	console.log("Posting email to ingest endpoint:", {
		...meta,
		bodyBytes: body.byteLength,
	});

	const response = await fetch(url, {
		method: "POST",
		headers: {
			"Content-Type": "message/rfc822",
			"X-Lite-Mail-Ingest-PSK": psk,
		},
		body,
		signal,
	});

	if (response.status === 401) {
		console.error("Ingest authentication failed:", {
			...meta,
			status: 401,
		});
		throw new RejectableDeliveryError(
			"Ingest authentication failed",
			"Mail delivery failed: ingest authentication rejected the message",
		);
	}

	if (response.status === 413) {
		console.error("Message too large:", {
			...meta,
			status: 413,
		});
		throw new RejectableDeliveryError(
			"Message too large",
			"Mail delivery failed: message exceeds ingest size limits",
		);
	}

	if (response.ok) {
		let responseText = "";
		try {
			responseText = await response.text();
			const data = JSON.parse(responseText);

			if (data.status === "accepted") {
				console.log("Email ingested successfully:", { ...meta });
				return { kind: "accepted" };
			}

			if (data.status === "duplicate") {
				console.log("Duplicate email ignored:", { ...meta });
				return { kind: "duplicate" };
			}
		} catch {
			console.warn("Email ingested (unparseable response):", {
				...meta,
				note: "Could not parse response",
			});
			return { kind: "unparseable-ok" };
		}
	}

	// Non-ok, non-401/413 status — return status so caller can decide on retry
	return { kind: "http-status", status: response.status };
}

/**
 * Task 4 ingest POST that surfaces the Go response status to the caller.
 * Returns `"accepted"` / `"duplicate"` / `null` (null = unparseable-ok).
 * Throws `RejectableDeliveryError` for 401/413/network/timeout/5xx.
 */
async function postToIngestWithStatus(
	url: string,
	psk: string,
	body: ArrayBuffer,
	meta: IngestMeta,
): Promise<"accepted" | "duplicate" | null> {
	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), 30000);

	try {
		let result = await attemptIngest(url, psk, body, meta, controller.signal);

		if (
			result.kind === "http-status" &&
			result.status >= 500 &&
			result.status < 600
		) {
			console.warn("Ingest server returned 5xx, retrying once:", {
				...meta,
				status: result.status,
			});
			const retryController = new AbortController();
			const retryTimeout = setTimeout(() => retryController.abort(), 30000);
			try {
				const retryResult = await attemptIngest(
					url,
					psk,
					body,
					meta,
					retryController.signal,
				);
				if (retryResult.kind !== "http-status") {
					return mapAttemptToStatus(retryResult);
				}
				result = retryResult;
			} finally {
				clearTimeout(retryTimeout);
			}
		}

		if (result.kind === "http-status") {
			throw new RejectableDeliveryError(
				"Ingest server error",
				`Mail delivery failed: ingest server returned status ${result.status}`,
			);
		}
		return mapAttemptToStatus(result);
	} catch (err) {
		if (err instanceof RejectableDeliveryError) {
			throw err;
		}
		if (err instanceof Error && err.name === "AbortError") {
			console.error("Ingest request timed out:", { ...meta, url });
			throw new RejectableDeliveryError(
				"Ingest timeout",
				"Mail delivery failed: ingest server did not respond in time",
			);
		}
		console.error("Ingest request context:", meta);
		console.error("Ingest request failed:", err);
		const message = err instanceof Error ? err.message : "Unknown error";
		throw new RejectableDeliveryError(
			"Ingest request failed",
			`Mail delivery failed: ${message}`,
		);
	} finally {
		clearTimeout(timeout);
	}
}

function mapAttemptToStatus(
	r: IngestAttemptResult,
): "accepted" | "duplicate" | null {
	switch (r.kind) {
		case "accepted":
			return "accepted";
		case "duplicate":
			return "duplicate";
		case "unparseable-ok":
			return null;
		default:
			return null;
	}
}

// task 1: the upstream-style `fetchHandler` (currently a 404 stub) is wired into
// the default export so Cloudflare serves HTTP for the Worker. Real route
// surface (`/email/:id?mode=text|html`) lands in task 2.
async function handlerFetchStub(
	request: Request,
	env: Env,
	_ctx: ExecutionContext,
): Promise<Response> {
	const { fetchHandler } = await import("./handler/fetch");
	return fetchHandler(request, env);
}

export { email, handlerFetchStub as fetch };
export default { email, fetch: handlerFetchStub };
