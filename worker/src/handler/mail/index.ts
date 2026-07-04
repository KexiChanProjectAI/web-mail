import type {
	ForwardableEmailMessage,
	ExecutionContext,
} from "@cloudflare/workers-types";
import type { Env } from "../../types";
import { email } from "../..";

/**
 * Upstream-style `emailHandler` — task 4.
 *
 * The orchestration (parse-locally → POST raw MIME to Go → only on
 * `status: accepted` write KV cache + send Telegram → swallow any
 * Telegram/cache failure without `setReject`) lives in `email()` in
 * `../../index`. `emailHandler` is the canonical upstream export name
 * and is what other modules of the surface import. We keep this file
 * as a thin re-export so the orchestration is testable from
 * `src/index.ts` while the public surface name lives where the
 * upstream code expects it.
 *
 * The raw-MIME ingest contract is unchanged: this handler does NOT
 * touch the Go payload shape.
 */
export async function emailHandler(
	message: ForwardableEmailMessage,
	env: Env,
	ctx: ExecutionContext,
): Promise<void> {
	await email(message, env, ctx);
}
