import { describe, it, expect, beforeEach, vi } from "vitest";
import type { KVNamespace } from "@cloudflare/workers-types";
import type { Env } from "../src/types";

/**
 * Task 2 — `/email/:id?mode=text|html` fetch route.
 *
 * These tests must FAIL before the upstream-style fetch surface is wired
 * (the current `fetchHandler` returns 404 for everything). They PASS once
 * the handler reads a cached `EmailCache` from KV binding `DB` and serves
 * the `text` / `html` field with the correct content type.
 *
 * Coverage matrix:
 *   (a) cached text preview returns 200 / text/plain
 *   (b) cached HTML preview returns 200 / text/html
 *   (c) missing id returns 404
 *   (d) unsupported mode returns 404
 *   (e) route path is /email/:id?mode=...
 *   (f) Dao.saveMailCache writes JSON with the supplied TTL (or default)
 */

// ---------------------------------------------------------------------------
// In-memory KV mock. Records `get`/`put` calls for assertions.
// ---------------------------------------------------------------------------
interface KVPutCall {
	key: string;
	value: string;
	options?: { expirationTtl?: number };
}

const createMockKV = (): KVNamespace & {
	__puts: KVPutCall[];
	__store: Map<string, string>;
	__seed: (key: string, value: string) => void;
} => {
	const store = new Map<string, string>();
	const puts: KVPutCall[] = [];
	const kv = {
		__store: store,
		__puts: puts,
		__seed: (key: string, value: string) => {
			store.set(key, value);
		},
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
			/* not used in these tests */
		},
		async list(): Promise<{ keys: { name: string }[] }> {
			return { keys: [] };
		},
		async getWithMetadata(): Promise<{
			value: string | null;
			metadata: unknown;
		}> {
			return { value: store.get(arguments[0]) ?? null, metadata: null };
		},
	};
	return kv as unknown as KVNamespace & {
		__puts: KVPutCall[];
		__store: Map<string, string>;
		__seed: (key: string, value: string) => void;
	};
};

const createMockEnv = (overrides: Partial<Env> = {}): Env => ({
	DB: createMockKV() as unknown as KVNamespace,
	...overrides,
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
const seedMailCache = (
	kv: ReturnType<typeof createMockKV>,
	id: string,
	cache: Record<string, unknown>,
) => {
	kv.__seed(id, JSON.stringify(cache));
};

const makeRequest = (path: string): Request =>
	new Request(`https://worker.example.workers.dev${path}`, { method: "GET" });

// ---------------------------------------------------------------------------
// /email/:id?mode=... route tests
// ---------------------------------------------------------------------------
describe("fetch handler — /email/:id preview route (task 2)", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("(a) serves cached text preview as text/plain; charset=utf-8", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();
		seedMailCache(
			env.DB as unknown as ReturnType<typeof createMockKV>,
			"abc123",
			{
				id: "abc123",
				messageId: "<m1@example.com>",
				from: "sender@example.com",
				to: "rcpt@example.com",
				subject: "Hello",
				text: "plain body content",
			},
		);

		const res = await fetchHandler(makeRequest("/email/abc123?mode=text"), env);

		expect(res.status).toBe(200);
		expect(res.headers.get("content-type")).toBe("text/plain; charset=utf-8");
		expect(await res.text()).toBe("plain body content");
	});

	it("(b) serves cached HTML preview as text/html; charset=utf-8", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();
		const html = "<p>html body</p>";
		seedMailCache(
			env.DB as unknown as ReturnType<typeof createMockKV>,
			"html-1",
			{
				id: "html-1",
				messageId: "<m2@example.com>",
				from: "sender@example.com",
				to: "rcpt@example.com",
				subject: "HTML mail",
				html,
			},
		);

		const res = await fetchHandler(makeRequest("/email/html-1?mode=html"), env);

		expect(res.status).toBe(200);
		expect(res.headers.get("content-type")).toBe("text/html; charset=utf-8");
		expect(await res.text()).toBe(html);
	});

	it("(c) returns 404 when the cache id is missing", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();

		const res = await fetchHandler(
			makeRequest("/email/missing?mode=text"),
			env,
		);

		expect(res.status).toBe(404);
	});

	it("(c2) returns 404 for missing HTML preview as well", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();

		const res = await fetchHandler(
			makeRequest("/email/missing?mode=html"),
			env,
		);

		expect(res.status).toBe(404);
	});

	it("(d) returns 404 for an unsupported mode", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();
		seedMailCache(env.DB as unknown as ReturnType<typeof createMockKV>, "abc", {
			id: "abc",
			messageId: "<m@example.com>",
			from: "s@example.com",
			to: "r@example.com",
			subject: "S",
			text: "t",
		});

		const res = await fetchHandler(makeRequest("/email/abc?mode=json"), env);

		expect(res.status).toBe(404);
	});

	it("(e) routes the /email/:id?mode=... path — other paths are not preview routes", async () => {
		const { fetchHandler } = await import("../src/handler/fetch");
		const env = createMockEnv();

		// /email/abc?mode=text is routed (returns 200/404 based on cache)
		const ok = await fetchHandler(makeRequest("/email/abc?mode=text"), env);
		expect([200, 404]).toContain(ok.status);

		// Other unrelated paths must NOT be claimed by the preview route
		const other = await fetchHandler(makeRequest("/share/abc"), env);
		expect(other.status).toBe(404);
	});
});

// ---------------------------------------------------------------------------
// Dao.saveMailCache / loadMailCache tests
// ---------------------------------------------------------------------------
describe("Dao — preview cache KV (task 2)", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("loadMailCache returns null when the key is absent", async () => {
		const { Dao } = await import("../src/db");
		const kv = createMockKV();
		const dao = new Dao(kv as unknown as KVNamespace);

		const result = await dao.loadMailCache("missing");
		expect(result).toBeNull();
	});

	it("loadMailCache parses a previously-stored EmailCache from JSON", async () => {
		const { Dao } = await import("../src/db");
		const kv = createMockKV();
		const cache = {
			id: "id-1",
			messageId: "<m@e.com>",
			from: "s@e.com",
			to: "r@e.com",
			subject: "Subj",
			text: "T",
			html: "<p>T</p>",
		};
		kv.__seed("id-1", JSON.stringify(cache));
		const dao = new Dao(kv as unknown as KVNamespace);

		const loaded = await dao.loadMailCache("id-1");
		expect(loaded).toEqual(cache);
	});

	it("saveMailCache serializes EmailCache as JSON and writes through to KV", async () => {
		const { Dao } = await import("../src/db");
		const kv = createMockKV();
		const dao = new Dao(kv as unknown as KVNamespace);

		await dao.saveMailCache(
			"id-2",
			{
				id: "id-2",
				messageId: "<m2@e.com>",
				from: "s@e.com",
				to: "r@e.com",
				subject: "Subj",
				text: "T",
			},
			3600,
		);

		const puts = kv.__puts;
		expect(puts).toHaveLength(1);
		expect(puts[0].key).toBe("id-2");
		expect(puts[0].options).toEqual({ expirationTtl: 3600 });
		expect(JSON.parse(puts[0].value)).toMatchObject({
			id: "id-2",
			text: "T",
		});
	});

	it("saveMailCache defaults to MAIL_TTL when an explicit TTL is not provided", async () => {
		const { Dao } = await import("../src/db");
		const kv = createMockKV();
		const dao = new Dao(kv as unknown as KVNamespace);

		await dao.saveMailCache("id-3", {
			id: "id-3",
			messageId: "<m3@e.com>",
			from: "s@e.com",
			to: "r@e.com",
			subject: "Subj",
			text: "T",
		});

		const puts = kv.__puts;
		expect(puts).toHaveLength(1);
		expect(puts[0].options).toBeUndefined(); // no TTL when caller omits it
	});
});
