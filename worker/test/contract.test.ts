import { describe, it, expect } from "vitest";
import type { KVNamespace } from "@cloudflare/workers-types";
import type { Env } from "../src/types";

// Task 1 contract tests — these must FAIL before the upstream-style module surface
// is implemented and PASS once worker/src/types, handler/mail, handler/fetch,
// db, mail/parse, and telegram/api stubs are in place.

describe("Worker runtime contract (task 1)", () => {
	describe("module surface", () => {
		it("exposes types/Env with upstream-style fields", async () => {
			const types = await import("../src/types");
			// Structural type assertion: Env must have all upstream-style fields and the
			// existing lite-mail ingest fields. We check via a typed stub to ensure the
			// interface compiles with every required key.
			const sample: Env = {
				TELEGRAM_TOKEN: "bot-token",
				TELEGRAM_ID: "12345",
				DOMAIN: "worker.example.workers.dev",
				MAIL_TTL: "86400",
				MAX_EMAIL_SIZE: "524288",
				MAX_EMAIL_SIZE_POLICY: "truncate",
				INGEST_URL: "https://mail.example.com/api/ingest",
				WORKER_INGEST_PSK: "psk",
				DB: {} as KVNamespace,
			};
			expect(sample).toBeDefined();
			// Env is type-only (no runtime value), but the module must be importable.
			expect(types).toBeDefined();
		});

		it("re-exports db/DAO stub", async () => {
			const db = await import("../src/db");
			expect(db).toBeDefined();
			// Task 2 will implement methods; for now expect a Dao class export.
			expect(typeof db.Dao).toBe("function");
		});

		it("re-exports mail/parse stub with parseEmail function", async () => {
			const mail = await import("../src/mail/parse");
			expect(mail).toBeDefined();
			expect(typeof mail.parseEmail).toBe("function");
		});

		it("re-exports telegram/api stub with createTelegramBotAPI factory", async () => {
			const telegram = await import("../src/telegram/api");
			expect(telegram).toBeDefined();
			expect(typeof telegram.createTelegramBotAPI).toBe("function");
		});

		it("re-exports handler/mail with emailHandler", async () => {
			const mail = await import("../src/handler/mail");
			expect(mail).toBeDefined();
			expect(typeof mail.emailHandler).toBe("function");
		});

		it("re-exports handler/fetch with fetchHandler", async () => {
			const fetch = await import("../src/handler/fetch");
			expect(fetch).toBeDefined();
			expect(typeof fetch.fetchHandler).toBe("function");
		});
	});

	describe("index.ts entrypoint", () => {
		it("exports a default object with both email and fetch handlers", async () => {
			const mod = await import("../src/index");
			expect(mod.default).toBeDefined();
			expect(typeof mod.default.fetch).toBe("function");
			expect(typeof mod.default.email).toBe("function");
		});

		it("keeps the named email export for Cloudflare Email Routing", async () => {
			const mod = await import("../src/index");
			expect(typeof mod.email).toBe("function");
		});
	});
});
