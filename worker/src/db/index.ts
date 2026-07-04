import type { KVNamespace } from "@cloudflare/workers-types";
import type { EmailCache } from "../types";

/**
 * Upstream-style KV DAO for the preview cache.
 *
 * Mirrors the `Dao` class from `TBXark/mail2telegram`'s `src/db/index.ts`
 * (current `master`, since the plan's pinned commit `6d6ffbc...` is not
 * reachable). Task 2 only needs the mail-cache surface; the
 * address-list / mail-status / telegram-id helpers from upstream are
 * intentionally NOT copied because they are out of scope for this
 * migration.
 *
 * Cache entries are stored as the JSON-serialized `EmailCache` object
 * keyed by `id` (the share-link slug). The upstream default
 * `expirationTtl` is supplied by the caller (default 86400 from
 * `MAIL_TTL`); this method itself does not impose a default so the
 * call site can pick the policy.
 */
export class Dao {
	private readonly db: KVNamespace;

	constructor(db: KVNamespace) {
		this.db = db;
	}

	/**
	 * Read a previously-stored `EmailCache` from KV. Returns `null` if
	 * the key is absent or the stored value cannot be parsed.
	 */
	async loadMailCache(id: string): Promise<EmailCache | null> {
		try {
			const raw = await this.db.get(id);
			if (!raw) {
				return null;
			}
			return JSON.parse(raw) as EmailCache;
		} catch (e) {
			console.error("loadMailCache: failed to read/parse cache", e);
			return null;
		}
	}

	/**
	 * Persist an `EmailCache` to KV as JSON. The caller controls TTL via
	 * `ttlSeconds` — when omitted, the entry has no per-call expiration
	 * (the upstream mail2telegram contract lets the binding-level
	 * default apply). The fetchHandler / task 4 default is `MAIL_TTL`
	 * (default 86400) per the plan.
	 */
	async saveMailCache(
		id: string,
		cache: EmailCache,
		ttlSeconds?: number,
	): Promise<void> {
		const options =
			ttlSeconds !== undefined ? { expirationTtl: ttlSeconds } : undefined;
		await this.db.put(id, JSON.stringify(cache), options);
	}

	// task 3+ will add: loadMailStatus, saveMailStatus,
	// telegramIDToMailID, saveTelegramIDToMailID. These are NOT
	// in task 2 scope.

	/** Internal accessor reserved for tests. */
	get _db(): KVNamespace {
		return this.db;
	}
}

export type { EmailCache };
