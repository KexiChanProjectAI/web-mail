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
 * Send the parsed email summary to every chat id in
 * `TELEGRAM_ID` (comma-separated). Failures are logged and swallowed
 * — the email has already been accepted by Go, so we MUST NOT call
 * `message.setReject(...)`.
 */
async function sendTelegramForCache(
	cache: EmailCache,
	env: Env,
): Promise<void> {
	const token = env.TELEGRAM_TOKEN;
	const ids = env.TELEGRAM_ID;
	if (!token || !ids) {
		// Opt-in: missing token or ids means Telegram is disabled.
		return;
	}
	const api = createTelegramBotAPI(token);
	const rendered = await renderEmail(cache, env);
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
		} catch (err) {
			console.error("Telegram send failed:", {
				chatId,
				cacheId: cache.id,
				error: err instanceof Error ? err.message : String(err),
			});
			// Do NOT rethrow — the email was already accepted by Go.
		}
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

async function email(
	message: ForwardableEmailMessage,
	env: Env,
	ctx: ExecutionContext,
): Promise<void> {
	const ingestUrl = env.INGEST_URL;
	const ingestPSK = env.WORKER_INGEST_PSK;
	const meta = buildIngestMeta(message, ingestUrl);

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

	if (!ingestUrl || !ingestPSK) {
		console.error("Missing required environment configuration:", {
			...meta,
			hasIngestUrl: !!ingestUrl,
			hasIngestPSK: !!ingestPSK,
		});
		message.setReject("Worker misconfigured: missing ingest URL or PSK");
		return;
	}

	// Parse locally (best-effort) for the preview cache + Telegram render.
	// A parse failure does NOT block the raw-MIME ingest — Go remains the
	// source of truth and will still receive the full message body.
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
		// parseEmail itself is defensive (postal-mime is lenient and
		// parse.ts catches throws), so this catch is a belt-and-suspenders
		// guard. We synthesize a minimal cache so cache write + Telegram
		// still have something to operate on.
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

	console.log("Delivering email to ingest endpoint:", meta);

	let ingestStatus: "accepted" | "duplicate" | null = null;
	try {
		const response = await postToIngestWithStatus(
			ingestUrl,
			ingestPSK,
			rawMIME.buffer as ArrayBuffer,
			meta,
		);
		ingestStatus = response;
	} catch (err) {
		const reason =
			err instanceof RejectableDeliveryError
				? err.reason
				: "Mail delivery failed: unexpected error";
		console.error("Email delivery failed:", { ...meta, reason });
		message.setReject(reason);
		return;
	}

	if (ingestStatus === "duplicate") {
		console.log("Duplicate email ignored:", meta);
		// Per task 4 contract: no cache write, no Telegram send.
		return;
	}

	if (ingestStatus !== "accepted") {
		// Defensive: postToIngestWithStatus returns null only for
		// ambiguous "ok but unparseable JSON" responses. Treat them
		// as accepted (parity with previous behavior) so a transient
		// Go response shape change cannot drop Telegram delivery.
		console.warn(
			"Ingest response was not a recognized status; treating as accepted:",
			meta,
		);
	}

	// Accepted (or unparseable-but-ok) path: write preview cache and
	// send Telegram. Both are best-effort — failures are logged and
	// swallowed; we MUST NOT call message.setReject() because the
	// email is already durably stored in Go.
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
		await sendTelegramForCache(cache, env);
	} catch (err) {
		// sendTelegramForCache already swallows per-chat failures and
		// logs them. This outer catch is a final safety net for
		// renderEmail or other unexpected errors.
		console.error("Telegram dispatch failed unexpectedly:", {
			...meta,
			cacheId: cache.id,
			error: err instanceof Error ? err.message : String(err),
		});
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
