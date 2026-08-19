import type {
	ForwardableEmailMessage,
	ExecutionContext,
} from "@cloudflare/workers-types";
import type { Env } from "../../types";
import { email } from "../..";

/**
 * Upstream-style `emailHandler`.
 *
 * Orchestration lives in `email()` in `../../index`: parse locally,
 * then run Go ingest and preview/Telegram in parallel. Either path
 * failing is logged on its own side — ingest failures `setReject` so
 * Cloudflare can retry durable storage; Telegram/cache failures never
 * `setReject`. `emailHandler` is the canonical upstream export name.
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
