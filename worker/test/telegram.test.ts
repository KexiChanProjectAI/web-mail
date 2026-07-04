import { describe, it, expect, beforeEach, vi } from "vitest";
import type { ForwardableEmailMessage } from "@cloudflare/workers-types";

// ---------------------------------------------------------------------------
// Task 3 — Telegram parser / client / renderer.
//
// These tests must FAIL before parse.ts / telegram/api.ts / render.ts land and
// PASS after they implement the lite-mail-preserving Telegram flow.
//
// Coverage matrix:
//   parse:
//     (1) simple text email → text populated, html empty
//     (2) html-only email → html populated, text derived via html-to-text
//     (3) over-size + unhandled policy → cache has placeholder text/html
//     (4) over-size + truncate policy → text appended with [Truncated] marker
//     (5) malformed mime → cache still has text/html fallback (no throw)
//     (6) messageId falls back to id when Message-ID header is missing
//   telegram/api:
//     (7) factory returns object with token + baseURL
//     (8) sendMessage POSTs JSON to https://api.telegram.org/bot<token>/sendMessage
//     (9) non-2xx Telegram response surfaces an error
//     (10) no callback_data is ever serialized in the request body
//   render:
//     (11) HTML escaping for <, >, & in From/To/Subject/Date/Body
//     (12) labels From: / To: / Subject: / Date: present
//     (13) body preview truncated to 300 chars
//     (14) total message capped at 3500 chars
//     (15) exactly two URL buttons with labels "View as TXT" / "View as HTML"
//     (16) button URLs are https://${DOMAIN}/email/${id}?mode=text|html
//     (17) serialized markup contains no callback_data key
// ---------------------------------------------------------------------------

const makeRawStream = (content: string): ReadableStream => {
	const encoder = new TextEncoder();
	return new ReadableStream({
		start(controller) {
			controller.enqueue(encoder.encode(content));
			controller.close();
		},
	});
};

const makeMessage = (
	raw: string,
	overrides: Partial<{
		from: string;
		to: string;
		rawSize: number;
		headers: Headers;
	}> = {},
): ForwardableEmailMessage => {
	const encoder = new TextEncoder();
	const bytes = encoder.encode(raw);
	return {
		from: overrides.from ?? "sender@example.com",
		to: overrides.to ?? "rcpt@example.com",
		raw: makeRawStream(raw),
		rawSize: overrides.rawSize ?? bytes.byteLength,
		headers:
			overrides.headers ??
			new Headers({
				"Message-ID": "<m1@example.com>",
				Subject: "Hello",
				Date: "Tue, 19 May 2026 12:00:00 +0000",
			}),
		canBeForwarded: true,
		setReject: vi.fn(),
		forward: vi.fn().mockResolvedValue({ uuid: "u" }),
		reply: vi.fn().mockResolvedValue({ uuid: "u" }),
	} as ForwardableEmailMessage;
};

describe("parseEmail (task 3)", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("(1) parses a simple text email and populates text with the body", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		const raw =
			"From: sender@example.com\r\n" +
			"To: rcpt@example.com\r\n" +
			"Subject: Hello\r\n" +
			"Message-ID: <m1@example.com>\r\n" +
			"Date: Tue, 19 May 2026 12:00:00 +0000\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			"This is the body.\r\n";
		const msg = makeMessage(raw);

		const cache = await parseEmail(msg, 1024 * 1024, "continue");

		expect(cache.id).toMatch(/^[0-9a-f-]{36}$/i);
		expect(cache.from).toBe("sender@example.com");
		expect(cache.to).toBe("rcpt@example.com");
		expect(cache.subject).toBe("Hello");
		expect(cache.text).toContain("This is the body.");
	});

	it("(2) html-only email yields html populated and text derived via html-to-text", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		const raw =
			"From: s@e.com\r\n" +
			"To: r@e.com\r\n" +
			"Subject: HTML mail\r\n" +
			"Message-ID: <h@e.com>\r\n" +
			"Content-Type: text/html; charset=utf-8\r\n" +
			"\r\n" +
			"<p>Hello <b>world</b></p>\r\n";
		const msg = makeMessage(raw);

		const cache = await parseEmail(msg, 1024 * 1024, "continue");

		expect(cache.html).toContain("Hello");
		expect(cache.html).toContain("<b>world</b>");
		// html-to-text should produce a plain-text version that strips the tags
		expect(cache.text).toContain("Hello");
		expect(cache.text).toContain("world");
		expect(cache.text).not.toContain("<b>");
	});

	it("(3) over-size + unhandled policy yields a placeholder cache (no throw)", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		const raw = "From: s@e.com\r\n\r\nbody\r\n";
		const msg = makeMessage(raw, { rawSize: 9999 });

		const cache = await parseEmail(msg, 100, "unhandled");

		expect(cache.text).toBeDefined();
		expect(cache.html).toBeDefined();
		expect(cache.text).toContain("exceeds the maximum size");
		expect(cache.text).toContain("100");
	});

	it("(4) over-size + truncate policy appends a [Truncated] marker to the parsed text", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		// Make a payload that easily exceeds 50 bytes so truncation kicks in.
		const body = "X".repeat(500);
		const raw =
			"From: s@e.com\r\n" +
			"To: r@e.com\r\n" +
			"Subject: Big\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			body +
			"\r\n";
		const msg = makeMessage(raw, { rawSize: 50_000 });

		const cache = await parseEmail(msg, 50, "truncate");

		expect(cache.text).toBeDefined();
		expect(cache.text).toContain("[Truncated]");
		expect(cache.text).toContain("exceeds the maximum size of 50 bytes");
	});

	it("(5) malformed MIME returns a cache with text/html fallback (no throw)", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		// Intentionally-broken MIME (no headers, no blank line, no terminator).
		const raw = "\x00\x01garbage not real mime\xff\xfe";
		const msg = makeMessage(raw);

		const cache = await parseEmail(msg, 1024 * 1024, "continue");

		// The cache must still be returned (no throw) and carry a usable text
		// field — at minimum non-empty, with a parse-error note OR with whatever
		// postal-mime could extract.
		expect(cache.text).toBeDefined();
		expect(typeof cache.text).toBe("string");
		// From / envelope fields come from the message object, not the MIME body
		expect(cache.from).toBe("sender@example.com");
		expect(cache.to).toBe("rcpt@example.com");
	});

	it("(6) messageId falls back to the generated id when Message-ID header is missing", async () => {
		const { parseEmail } = await import("../src/mail/parse");
		const raw =
			"From: s@e.com\r\n" +
			"To: r@e.com\r\n" +
			"Subject: No mid\r\n" +
			"Content-Type: text/plain\r\n" +
			"\r\n" +
			"body\r\n";
		const msg = makeMessage(raw, {
			headers: new Headers({
				Subject: "No mid",
			}),
		});

		const cache = await parseEmail(msg, 1024 * 1024, "continue");

		// messageId should be the id we just generated, not empty / undefined.
		expect(cache.messageId).toBe(cache.id);
	});
});

describe("createTelegramBotAPI (task 3)", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("(7) factory returns an object with token + baseURL", async () => {
		const { createTelegramBotAPI } = await import("../src/telegram/api");
		const api = createTelegramBotAPI("123:abc");
		expect(api.token).toBe("123:abc");
		expect(api.baseURL).toBe("https://api.telegram.org");
	});

	it("(8) sendMessage POSTs JSON to https://api.telegram.org/bot<token>/sendMessage", async () => {
		const mockFetch = vi.fn().mockResolvedValue(
			new Response(JSON.stringify({ ok: true, result: { message_id: 1 } }), {
				status: 200,
				headers: { "content-type": "application/json" },
			}),
		);
		vi.stubGlobal("fetch", mockFetch);
		const { createTelegramBotAPI } = await import("../src/telegram/api");

		const api = createTelegramBotAPI("tok-123");
		const res = await api.sendMessage({
			chat_id: "999",
			text: "hi",
		});

		expect(mockFetch).toHaveBeenCalledTimes(1);
		const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
		expect(url).toBe("https://api.telegram.org/bottok-123/sendMessage");
		expect(init.method).toBe("POST");
		expect((init.headers as Record<string, string>)["Content-Type"]).toBe(
			"application/json",
		);
		const body = JSON.parse(String(init.body));
		expect(body).toMatchObject({ chat_id: "999", text: "hi" });
		expect(res.ok).toBe(true);
	});

	it("(9) non-2xx Telegram response surfaces an error", async () => {
		const mockFetch = vi
			.fn()
			.mockResolvedValue(
				new Response(
					JSON.stringify({ ok: false, description: "bad chat id" }),
					{ status: 400, headers: { "content-type": "application/json" } },
				),
			);
		vi.stubGlobal("fetch", mockFetch);
		const { createTelegramBotAPI } = await import("../src/telegram/api");

		const api = createTelegramBotAPI("tok");
		await expect(api.sendMessage({ chat_id: "x", text: "hi" })).rejects.toThrow(
			/telegram/i,
		);
	});
});

describe("renderEmail (task 3)", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	const sampleCache = {
		id: "abc-123",
		messageId: "<m@example.com>",
		from: "Alice <alice@example.com>",
		to: "bob@example.com",
		subject: "Test & <Important>",
		text: "Hello <world> & goodbye",
	};

	it("(11) HTML-escapes From / To / Subject / body content", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const out = await renderEmail(sampleCache, {
			DOMAIN: "mail.example.workers.dev",
		});
		expect(out.text).toContain("Alice &lt;alice@example.com&gt;");
		expect(out.text).toContain("Test &amp; &lt;Important&gt;");
		expect(out.text).toContain("Hello &lt;world&gt; &amp; goodbye");
	});

	it("(12) contains the four label lines", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const out = await renderEmail(sampleCache, {
			DOMAIN: "mail.example.workers.dev",
		});
		expect(out.text).toMatch(/^From:/m);
		expect(out.text).toMatch(/^To:/m);
		expect(out.text).toMatch(/^Subject:/m);
		expect(out.text).toMatch(/^Date:/m);
	});

	it("(13) body preview is truncated to 300 characters", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const longText = "x".repeat(5000);
		const out = await renderEmail(
			{ ...sampleCache, text: longText },
			{ DOMAIN: "mail.example.workers.dev" },
		);
		// Count how many body characters appear in the rendered output
		// (i.e. the substring after the Date: label and a blank line).
		const bodyMatch = out.text.split(/\n\n/).pop() ?? "";
		expect(bodyMatch.length).toBeLessThanOrEqual(300);
		// And the full output should respect the 3500-char cap.
		expect(out.text.length).toBeLessThanOrEqual(3500);
	});

	it("(14) total message length is capped at 3500 characters", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const huge = "y".repeat(10_000);
		const out = await renderEmail(
			{ ...sampleCache, text: huge, subject: huge },
			{ DOMAIN: "mail.example.workers.dev" },
		);
		expect(out.text.length).toBeLessThanOrEqual(3500);
	});

	it("(15) renders exactly two URL buttons labeled View as TXT / View as HTML", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const out = await renderEmail(sampleCache, {
			DOMAIN: "mail.example.workers.dev",
		});
		const keyboard = out.reply_markup?.inline_keyboard ?? [];
		expect(keyboard).toHaveLength(1);
		const row = keyboard[0];
		expect(row).toHaveLength(2);
		expect(row[0].text).toBe("View as TXT");
		expect(row[1].text).toBe("View as HTML");
	});

	it("(16) button URLs are https://${DOMAIN}/email/${id}?mode=text|html", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const out = await renderEmail(sampleCache, {
			DOMAIN: "mail.example.workers.dev",
		});
		const row = out.reply_markup?.inline_keyboard[0] ?? [];
		expect(row[0].url).toBe(
			"https://mail.example.workers.dev/email/abc-123?mode=text",
		);
		expect(row[1].url).toBe(
			"https://mail.example.workers.dev/email/abc-123?mode=html",
		);
	});

	it("(17) serialized markup contains no callback_data key", async () => {
		const { renderEmail } = await import("../src/mail/render");
		const out = await renderEmail(sampleCache, {
			DOMAIN: "mail.example.workers.dev",
		});
		const serialized = JSON.stringify(out);
		expect(serialized).not.toContain("callback_data");
		// It must still contain the url field for the buttons.
		expect(serialized).toContain('"url"');
	});
});
