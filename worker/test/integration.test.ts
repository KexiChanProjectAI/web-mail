import { describe, it, expect, beforeEach, vi } from "vitest";
import type {
	ForwardableEmailMessage,
	KVNamespace,
} from "@cloudflare/workers-types";
import type { Env } from "../src/types";

// ---------------------------------------------------------------------------
// Task 4 — Worker email-flow integration.
//
// End-to-end test of the `email()` flow. Required behavior:
//
//   (1) accepted ingest path → one Go POST, one KV write, one Telegram request
//   (2) duplicate ingest       → no KV write, no Telegram request
//   (3) ingest 401 / 413 / 5xx → no KV write, no Telegram request, setReject
//   (4) parse failure          → still POSTs raw MIME; if Go accepts, accepts
//   (5) Telegram send failure  → no setReject; KV still written
//
// These tests must FAIL before the integration is wired and PASS once
// `email()` parses locally, then ingests, then only on `status: accepted`
// saves the cache and sends Telegram.
// ---------------------------------------------------------------------------

// We mock the global `fetch` so we can both
//   (a) capture the Go POST (Ingest endpoint), and
//   (b) capture the Telegram API POST (`api.telegram.org`).
// A test that needs the Go POST to be accepted mocks a 200 JSON response;
// a test that needs to fail the Go POST mocks a 401/413/5xx.

const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

// We also stub console so we can assert log lines without polluting output.
const mockConsoleLog = vi.fn();
const mockConsoleWarn = vi.fn();
const mockConsoleError = vi.fn();
vi.stubGlobal("console", {
	log: mockConsoleLog,
	warn: mockConsoleWarn,
	error: mockConsoleError,
});

// ---------------------------------------------------------------------------
// In-memory KV mock (mirrors the one in fetch.test.ts).
// ---------------------------------------------------------------------------
const createMockKV = (): KVNamespace & {
	__puts: Array<{
		key: string;
		value: string;
		options?: { expirationTtl?: number };
	}>;
	__store: Map<string, string>;
} => {
	const store = new Map<string, string>();
	const puts: Array<{
		key: string;
		value: string;
		options?: { expirationTtl?: number };
	}> = [];
	return {
		__store: store,
		__puts: puts,
		async get(key: string): Promise<string | null> {
			return store.has(key) ? (store.get(key) as string) : null;
		},
		async put(
			key: string,
			value: string,
			options?: { expirationTtl?: number },
		): Promise<void> {
			puts.push({ key, value, options });
			store.set(key, value);
		},
		async delete(): Promise<void> {
			/* not used */
		},
		async list(): Promise<{ keys: { name: string }[] }> {
			return { keys: [] };
		},
		async getWithMetadata(): Promise<{
			value: string | null;
			metadata: unknown;
		}> {
			return { value: null, metadata: null };
		},
	} as unknown as KVNamespace & {
		__puts: Array<{
			key: string;
			value: string;
			options?: { expirationTtl?: number };
		}>;
		__store: Map<string, string>;
	};
};

// ---------------------------------------------------------------------------
// Helpers
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

const SAMPLE_RAW =
	"From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Integration test\r\n" +
	"Message-ID: <integration-1@example.com>\r\n" +
	"Date: Tue, 19 May 2026 12:00:00 +0000\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hello from the integration test.\r\n";

const createMockMessage = (
	overrides: Partial<{
		from: string;
		to: string;
		raw: ReadableStream;
		rawSize: number;
		headers: Headers;
	}> = {},
): ForwardableEmailMessage => {
	const bytes = new TextEncoder().encode(SAMPLE_RAW).byteLength;
	return {
		from: overrides.from ?? "sender@example.com",
		to: overrides.to ?? "rcpt@example.com",
		raw: overrides.raw ?? makeRawStream(SAMPLE_RAW),
		rawSize: overrides.rawSize ?? bytes,
		headers:
			overrides.headers ??
			new Headers({
				"Message-ID": "<integration-1@example.com>",
				Subject: "Integration test",
			}),
		canBeForwarded: true,
		setReject: vi.fn(),
		forward: vi.fn().mockResolvedValue({ uuid: "u" }),
		reply: vi.fn().mockResolvedValue({ uuid: "u" }),
	} as ForwardableEmailMessage;
};

interface MockContext {
	waitUntil: (promise: Promise<unknown>) => void;
	passThroughOnException: () => void;
}

const createMockContext = (): MockContext => {
	const waitUntilFn = vi.fn((promise: Promise<unknown>) => {
		void promise.catch(() => {});
	});
	return {
		waitUntil: waitUntilFn,
		passThroughOnException: vi.fn(),
	};
};

const createMockEnv = (overrides: Partial<Env> = {}): Env => {
	const kv = createMockKV();
	return {
		INGEST_URL: "https://mail.example.com/api/ingest",
		WORKER_INGEST_PSK: "psk-secret",
		TELEGRAM_TOKEN: "bot-token-123",
		TELEGRAM_ID: "111,222",
		DOMAIN: "mail.example.workers.dev",
		MAIL_TTL: "86400",
		MAX_EMAIL_SIZE: "524288",
		MAX_EMAIL_SIZE_POLICY: "truncate",
		DB: kv as unknown as KVNamespace,
		...overrides,
	};
};

/**
 * Find the Go (ingest) POST among the captured fetch calls. The
 * Telegram POST hits `api.telegram.org`; the Go POST hits the
 * `INGEST_URL`. We separate them by URL host.
 */
const findGoCall = () => {
	return mockFetch.mock.calls.find(([url]) =>
		String(url).startsWith("https://mail.example.com/api/ingest"),
	) as [string, RequestInit] | undefined;
};

const findTelegramCalls = () => {
	return mockFetch.mock.calls.filter(([url]) =>
		String(url).startsWith("https://api.telegram.org/"),
	) as Array<[string, RequestInit]>;
};

// ---------------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------------

describe("email() flow — task 4 integration", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockFetch.mockReset();
		mockConsoleLog.mockReset();
		mockConsoleWarn.mockReset();
		mockConsoleError.mockReset();
	});

	describe("happy path: accepted ingest", () => {
		it("(1) one Go POST, one KV write, one Telegram request per chat_id", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockImplementation((url: string) => {
				if (String(url).startsWith("https://mail.example.com/api/ingest")) {
					return Promise.resolve(
						new Response(
							JSON.stringify({ status: "accepted", message_id: 99 }),
							{ status: 200, headers: { "content-type": "application/json" } },
						),
					);
				}
				// Telegram API
				return Promise.resolve(
					new Response(
						JSON.stringify({ ok: true, result: { message_id: 1 } }),
						{
							status: 200,
							headers: { "content-type": "application/json" },
						},
					),
				);
			});

			await email(message, env, ctx);

			// (a) exactly one Go POST
			const goCall = findGoCall();
			expect(goCall).toBeDefined();
			expect(goCall![1].method).toBe("POST");
			expect(
				(goCall![1].headers as Record<string, string>)["Content-Type"],
			).toBe("message/rfc822");
			expect(
				(goCall![1].headers as Record<string, string>)[
					"X-Lite-Mail-Ingest-PSK"
				],
			).toBe("psk-secret");

			// (b) exactly one KV put, key = cache.id, TTL = MAIL_TTL=86400
			expect(kv.__puts).toHaveLength(1);
			const put = kv.__puts[0];
			expect(put.options).toEqual({ expirationTtl: 86400 });
			const stored = JSON.parse(put.value);
			expect(stored.id).toBe(put.key);
			expect(stored.from).toBe("sender@example.com");
			expect(stored.to).toBe("rcpt@example.com");
			expect(stored.text).toContain("Hello from the integration test.");

			// (c) one Telegram POST per chat_id (TELEGRAM_ID = "111,222" → 2)
			const telegramCalls = findTelegramCalls();
			expect(telegramCalls).toHaveLength(2);
			const bodies = telegramCalls.map(([, init]) =>
				JSON.parse(String(init.body)),
			);
			expect(bodies.map((b) => b.chat_id).sort()).toEqual(["111", "222"]);

			// (d) no setReject
			expect(message.setReject).not.toHaveBeenCalled();
		});

		it("TELEGRAM_ID with a single chat id sends one Telegram request", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv({ TELEGRAM_ID: "only-one" });
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockImplementation((url: string) => {
				if (String(url).startsWith("https://mail.example.com/api/ingest")) {
					return Promise.resolve(
						new Response(
							JSON.stringify({ status: "accepted", message_id: 1 }),
							{ status: 200 },
						),
					);
				}
				return Promise.resolve(
					new Response(
						JSON.stringify({ ok: true, result: { message_id: 1 } }),
						{
							status: 200,
						},
					),
				);
			});

			await email(message, env, ctx);

			expect(findTelegramCalls()).toHaveLength(1);
		});
	});

	describe("duplicate ingest", () => {
		it("(2) no KV write, no Telegram request, no setReject", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockResolvedValueOnce(
				new Response(JSON.stringify({ status: "duplicate" }), {
					status: 200,
					headers: { "content-type": "application/json" },
				}),
			);

			await email(message, env, ctx);

			// exactly one Go POST (the duplicate detection call)
			expect(findGoCall()).toBeDefined();
			expect(findTelegramCalls()).toHaveLength(0);
			expect(kv.__puts).toHaveLength(0);
			expect(message.setReject).not.toHaveBeenCalled();
		});
	});

	describe("ingest failure", () => {
		it("(3a) 401 → no KV, no Telegram, setReject called", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockResolvedValueOnce(
				new Response(null, { status: 401, statusText: "Unauthorized" }),
			);

			await email(message, env, ctx);

			expect(findTelegramCalls()).toHaveLength(0);
			expect(kv.__puts).toHaveLength(0);
			expect(message.setReject).toHaveBeenCalledWith(
				expect.stringContaining("ingest authentication rejected"),
			);
		});

		it("(3b) 413 → no KV, no Telegram, setReject called", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockResolvedValueOnce(
				new Response(null, { status: 413, statusText: "Payload Too Large" }),
			);

			await email(message, env, ctx);

			expect(findTelegramCalls()).toHaveLength(0);
			expect(kv.__puts).toHaveLength(0);
			expect(message.setReject).toHaveBeenCalledWith(
				expect.stringContaining("message exceeds ingest size limits"),
			);
		});

		it("(3c) 5xx (after retry) → no KV, no Telegram, setReject called", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch
				.mockResolvedValueOnce(new Response(null, { status: 500 }))
				.mockResolvedValueOnce(new Response(null, { status: 502 }));

			await email(message, env, ctx);

			expect(findTelegramCalls()).toHaveLength(0);
			expect(kv.__puts).toHaveLength(0);
			expect(message.setReject).toHaveBeenCalledWith(
				expect.stringContaining("ingest server returned status"),
			);
		});
	});

	describe("parse failure", () => {
		it("(4) still POSTs raw MIME; if Go accepts, KV write + Telegram request still happen", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			// Intentionally-broken MIME — must still POST to Go, must not throw
			const brokenRaw = "\x00\x01garbage not real mime\xff\xfe";
			const brokenBytes = new TextEncoder().encode(brokenRaw).byteLength;
			const brokenStream = makeRawStream(brokenRaw);
			const message = createMockMessage({
				raw: brokenStream,
				rawSize: brokenBytes,
			});
			const ctx = createMockContext();

			mockFetch.mockImplementation((url: string) => {
				if (String(url).startsWith("https://mail.example.com/api/ingest")) {
					return Promise.resolve(
						new Response(
							JSON.stringify({ status: "accepted", message_id: 1 }),
							{ status: 200 },
						),
					);
				}
				return Promise.resolve(
					new Response(
						JSON.stringify({ ok: true, result: { message_id: 1 } }),
						{
							status: 200,
						},
					),
				);
			});

			await email(message, env, ctx);

			// (a) Go POST happened with raw MIME bytes
			const goCall = findGoCall();
			expect(goCall).toBeDefined();
			const bodyText = new TextDecoder().decode(goCall![1].body as ArrayBuffer);
			expect(bodyText).toBe(brokenRaw);

			// (b) Cache was still written (parse fallback still produced text/html)
			expect(kv.__puts).toHaveLength(1);
			const stored = JSON.parse(kv.__puts[0].value);
			expect(typeof stored.text).toBe("string");
			expect(stored.text.length).toBeGreaterThan(0);

			// (c) Telegram was sent
			expect(findTelegramCalls().length).toBeGreaterThan(0);
			expect(message.setReject).not.toHaveBeenCalled();
		});
	});

	describe("Telegram send failure after accepted ingest", () => {
		it("(5) no setReject is called; KV write still happens", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv();
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockImplementation((url: string) => {
				if (String(url).startsWith("https://mail.example.com/api/ingest")) {
					return Promise.resolve(
						new Response(
							JSON.stringify({ status: "accepted", message_id: 1 }),
							{ status: 200 },
						),
					);
				}
				// Telegram POST: 500
				return Promise.resolve(
					new Response(JSON.stringify({ ok: false, description: "boom" }), {
						status: 500,
						headers: { "content-type": "application/json" },
					}),
				);
			});

			await email(message, env, ctx);

			// Cache was written
			expect(kv.__puts).toHaveLength(1);
			// Telegram was attempted (2 calls — one per chat_id, both 500)
			expect(findTelegramCalls().length).toBeGreaterThanOrEqual(2);
			// CRUCIAL: no setReject
			expect(message.setReject).not.toHaveBeenCalled();
		});
	});

	describe("opt-out: missing Telegram env", () => {
		it("skips Telegram entirely if TELEGRAM_TOKEN is empty", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv({ TELEGRAM_TOKEN: "" });
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockResolvedValueOnce(
				new Response(JSON.stringify({ status: "accepted" }), { status: 200 }),
			);

			await email(message, env, ctx);

			// Go POST happened, KV write happened, but no Telegram call
			expect(findGoCall()).toBeDefined();
			expect(kv.__puts).toHaveLength(1);
			expect(findTelegramCalls()).toHaveLength(0);
			expect(message.setReject).not.toHaveBeenCalled();
		});

		it("skips Telegram entirely if TELEGRAM_ID is empty", async () => {
			const { email } = await import("../src/index");
			const env = createMockEnv({ TELEGRAM_ID: "" });
			const kv = env.DB as unknown as ReturnType<typeof createMockKV>;
			const message = createMockMessage();
			const ctx = createMockContext();

			mockFetch.mockResolvedValueOnce(
				new Response(JSON.stringify({ status: "accepted" }), { status: 200 }),
			);

			await email(message, env, ctx);

			expect(findGoCall()).toBeDefined();
			expect(kv.__puts).toHaveLength(1);
			expect(findTelegramCalls()).toHaveLength(0);
			expect(message.setReject).not.toHaveBeenCalled();
		});
	});
});
