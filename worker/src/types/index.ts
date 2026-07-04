import type { KVNamespace } from "@cloudflare/workers-types";

/**
 * Upstream-style Worker env contract for the worker-owns-telegram-delivery migration.
 *
 * - `TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`, `MAIL_TTL`, `MAX_EMAIL_SIZE`,
 *   `MAX_EMAIL_SIZE_POLICY` are pinned to the upstream mail2telegram names so the
 *   copied/adapted code has no naming fork.
 * - `DB` is the upstream KV namespace binding name; the Worker must use exactly
 *   `DB` (not a project-local alias) so future upstream sync work is mechanical.
 * - `INGEST_URL` and `WORKER_INGEST_PSK` are retained verbatim from the current
 *   lite-mail contract; the raw-MIME ingest path into Go is unchanged.
 *
 * task 1 ships the type surface only. Runtime validation, defaults, and the
 * Telegram / KV behavior live in tasks 2-4.
 */
export interface Env {
	// --- upstream mail2telegram (pinned names) ---
	/** Telegram Bot API token (e.g. "1234567890:AA..."). */
	TELEGRAM_TOKEN?: string;
	/** Comma-separated chat IDs that receive forwarded mail. */
	TELEGRAM_ID?: string;
	/** Worker domain (e.g. "mail.example.workers.dev") used to build preview URLs. */
	DOMAIN?: string;
	/** TTL in seconds for KV preview cache entries. Upstream default: 86400. */
	MAIL_TTL?: string;
	/** Max raw email size in bytes; default 524288 (512 KiB). */
	MAX_EMAIL_SIZE?: string;
	/** `unhandled` | `truncate` | `continue`. Default `truncate`. */
	MAX_EMAIL_SIZE_POLICY?: "unhandled" | "truncate" | "continue";

	// --- upstream KV binding (pinned name) ---
	/** KV namespace used for preview cache. Binding variable name MUST be `DB`. */
	DB: KVNamespace;

	// --- lite-mail ingest contract (unchanged) ---
	/** Full URL to the Go service ingest endpoint. */
	INGEST_URL?: string;
	/** PSK sent in the `X-Lite-Mail-Ingest-PSK` header to Go. */
	WORKER_INGEST_PSK?: string;
}

/**
 * Persisted record in the `DB` KV namespace that backs a preview link.
 * Field set mirrors upstream `EmailCache` so task 2's DAO can plug in directly.
 */
export interface EmailCache {
	id: string;
	messageId: string;
	from: string;
	to: string;
	subject: string;
	html?: string;
	text?: string;
}

export type MaxEmailSizePolicy = "unhandled" | "truncate" | "continue";
