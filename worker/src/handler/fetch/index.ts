import { Router } from "itty-router";
import type { Env } from "../../types";
import { Dao } from "../../db";

/**
 * Upstream-style preview fetch handler.
 *
 * Adopts the `GET /email/:id?mode=text|html` route shape from
 * `TBXark/mail2telegram`'s `src/handler/fetch/index.ts` (current
 * `master`) but DELIBERATELY drops every other upstream surface:
 *
 *   - NO `/`, `/init`, `/tma`, `/api/*`, `/telegram/:token/webhook`:
 *     TMA, webhook, address-list and bot-init routes are out of scope
 *     for this migration. The plan's "Must NOT have" list forbids
 *     callback / TMA / webhook / admin routes.
 *   - NO upstream TMA auth middleware: the public preview is meant to
 *     be reachable without bot init-data.
 *
 * Response semantics (locked by task 2 acceptance criteria):
 *   - `mode=text`   → `text/plain; charset=utf-8` with `cache.text`
 *   - `mode=html`   → `text/html; charset=utf-8` with `cache.html`
 *   - missing cache → 404
 *   - missing mode / unsupported mode → 404
 *
 * TTL default for `saveMailCache` callers is `MAIL_TTL` env (or 86400).
 */
const DEFAULT_MAIL_TTL_SECONDS = 86400;

const resolveMailTtl = (env: Env): number => {
	const raw = env.MAIL_TTL;
	if (!raw) return DEFAULT_MAIL_TTL_SECONDS;
	const parsed = Number.parseInt(raw, 10);
	if (Number.isFinite(parsed) && parsed > 0) return parsed;
	return DEFAULT_MAIL_TTL_SECONDS;
};

export function fetchHandler(request: Request, env: Env): Promise<Response> {
	const router = Router();
	const dao = new Dao(env.DB);

	router.get("/email/:id", async (req) => {
		const id = (req.params as { id: string }).id;
		const url = new URL(req.url);
		const modeRaw = url.searchParams.get("mode");
		// Per task 2 spec: missing mode → 404.
		// (Upstream defaults to `text`; we tighten to 404 to make the
		// surface explicit. Telegram buttons always set `mode=text|html`.)
		if (modeRaw !== "text" && modeRaw !== "html") {
			return new Response("Not found", {
				status: 404,
				headers: { "content-type": "text/plain; charset=utf-8" },
			});
		}

		const cache = await dao.loadMailCache(id);
		if (!cache) {
			return new Response("Not found", {
				status: 404,
				headers: { "content-type": "text/plain; charset=utf-8" },
			});
		}

		const contentType =
			modeRaw === "html"
				? "text/html; charset=utf-8"
				: "text/plain; charset=utf-8";
		const body = modeRaw === "html" ? (cache.html ?? "") : (cache.text ?? "");

		return new Response(body, {
			headers: { "content-type": contentType },
		});
	});

	router.all(
		"*",
		() =>
			new Response("Not found", {
				status: 404,
				headers: { "content-type": "text/plain; charset=utf-8" },
			}),
	);

	return router.fetch(request).then(
		(res) => res as Response,
		(err: unknown) => {
			console.error("fetchHandler: router error", err);
			return new Response("Internal error", {
				status: 500,
				headers: { "content-type": "text/plain; charset=utf-8" },
			});
		},
	) as unknown as Promise<Response>;
}

/**
 * Resolve the default mail TTL in seconds. Exported for task 4
 * (`email()` flow) so the writer and the reader agree on the policy.
 */
export function getDefaultMailTtl(env: Env): number {
	return resolveMailTtl(env);
}

/** Default TTL in seconds when `MAIL_TTL` env is unset/invalid. */
export const DEFAULT_TTL_SECONDS = DEFAULT_MAIL_TTL_SECONDS;
