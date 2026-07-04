import type { ForwardableEmailMessage } from "@cloudflare/workers-types";
import type { EmailCache, MaxEmailSizePolicy } from "../types";
import PostalMime, { type RawEmail } from "postal-mime";
import { convert } from "html-to-text";

/**
 * Parse a Cloudflare `ForwardableEmailMessage` into the upstream-compatible
 * `EmailCache` shape used for both Telegram rendering and KV preview cache.
 *
 * Adapted from upstream `TBXark/mail2telegram` (`src/mail/parse.ts`) and
 * pinned by task 3 of `.omo/plans/worker-owns-telegram-delivery.md`. The
 * behavior we keep:
 *
 *   - `unhandled` policy: return a cache whose text/html are a placeholder
 *     describing the overage; we do NOT touch the raw stream.
 *   - `truncate` policy: limit the raw stream to `maxSize` bytes before
 *     parsing, then append a `[Truncated]` note to the parsed text.
 *   - `continue` policy (default): parse the full stream; if `postal-mime`
 *     throws, fall back to a `text`/`html` cache entry with the error
 *     message so the caller can still send a Telegram summary.
 *
 * We do NOT use upstream's `useEmlHeaders` knob — lite-mail's surface keeps
 * the envelope `from` / `to` and the `Message-ID` / `Subject` headers as
 * authoritative, which is what task 3 needs for the lite-mail message shape.
 */
export async function parseEmail(
	message: ForwardableEmailMessage,
	maxSize: number,
	maxSizePolicy: MaxEmailSizePolicy,
): Promise<EmailCache> {
	const id = crypto.randomUUID();
	const cache: EmailCache = {
		id,
		messageId: message.headers.get("Message-ID")?.trim() || id,
		from: message.from,
		to: message.to,
		subject: message.headers.get("Subject") ?? "",
	};

	let isTruncate = false;
	let emailRaw: RawEmail = message.raw;

	// `message.rawSize` is the only size signal available before consuming
	// the stream, so it drives the policy decision.
	const overSize = message.rawSize > maxSize;

	switch (overSize ? maxSizePolicy : "continue") {
		case "unhandled": {
			const msg =
				`The original size of the email was ${message.rawSize} bytes, ` +
				`which exceeds the maximum size of ${maxSize} bytes.`;
			cache.text = msg;
			cache.html = msg;
			return cache;
		}
		case "truncate": {
			isTruncate = true;
			emailRaw = truncateStream(message.raw, maxSize);
			break;
		}
		default:
			break;
	}

	try {
		const parser = new PostalMime();
		const email = await parser.parse(emailRaw);

		cache.html = email.html ?? undefined;
		cache.text = email.text ?? undefined;

		if (cache.html && !cache.text) {
			cache.text = convert(cache.html, {});
		}

		// `postal-mime` is lenient and returns an empty object for totally
		// malformed MIME rather than throwing. Guarantee a non-empty `text`
		// so downstream render / KV always has something to show.
		if (!cache.text) {
			cache.text = "Email body is empty or could not be parsed.";
		}
		if (!cache.html) {
			cache.html = "<p>Email body is empty or could not be parsed.</p>";
		}

		if (isTruncate) {
			cache.text =
				(cache.text ?? "") +
				`\n\n[Truncated] The original size of the email was ${message.rawSize} bytes, which exceeds the maximum size of ${maxSize} bytes.`;
		}
	} catch (e) {
		const msg = `Error parsing email: ${(e as Error).message}`;
		cache.text = msg;
		cache.html = msg;
	}

	return cache;
}

/**
 * Cap a `ReadableStream<Uint8Array>` to at most `maxBytes` bytes, then
 * terminate. Adapted from upstream `truncateStream`.
 */
function truncateStream(
	stream: ReadableStream<Uint8Array>,
	maxBytes: number,
): ReadableStream<Uint8Array> {
	let bytesRead = 0;
	const tran = new TransformStream<Uint8Array, Uint8Array>({
		transform(
			chunk: Uint8Array,
			controller: TransformStreamDefaultController<Uint8Array>,
		) {
			if (bytesRead >= maxBytes) {
				controller.terminate();
				return;
			}
			const remainingBytes = maxBytes - bytesRead;
			if (chunk.length <= remainingBytes) {
				controller.enqueue(chunk);
				bytesRead += chunk.length;
			} else {
				const limitedChunk = chunk.slice(0, remainingBytes);
				controller.enqueue(limitedChunk);
				bytesRead += remainingBytes;
				controller.terminate();
			}
		},
	});
	return stream.pipeThrough(tran);
}
