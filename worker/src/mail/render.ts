import type { EmailCache } from "../types";

/**
 * Render an `EmailCache` into the lite-mail Telegram message shape.
 *
 * Adapted from the Go reference (`internal/telegram/payload.go`) for
 * task 3 of `.omo/plans/worker-owns-telegram-delivery.md`. We deliberately
 * do NOT import upstream `mail2telegram`'s `renderEmailListMode` because
 * that adds callback-data buttons (`Preview`, `Summary`, `Debug`,
 * `Back`, `Delete`) and the AI summary product surface, which the plan
 * explicitly forbids.
 *
 * The shape we emit:
 *   - HTML-escaped summary with `From:`, `To:`, `Subject:`, `Date:`
 *     labels, exactly matching the existing Go payload_test.go expectations.
 *   - 300-character body preview, 3500-character total cap.
 *   - Two URL buttons only: `View as TXT` and `View as HTML`, pointing at
 *     `https://${DOMAIN}/email/${cache.id}?mode=text|html` (the Worker
 *     preview route from task 2 — NOT the retired Go `/share/{token}`).
 *   - No `callback_data` anywhere in the serialized markup.
 */

export const MAX_MESSAGE_LEN = 3500;
export const BODY_PREVIEW_LEN = 300;

export type RenderEnv = {
	DOMAIN?: string;
};

export type RenderedTelegramMessage = {
	text: string;
	parse_mode: "HTML";
	reply_markup: {
		inline_keyboard: Array<Array<{ text: string; url: string }>>;
	};
	link_preview_options: { is_disabled: true };
};

/**
 * Escape `<`, `>`, `&` for Telegram HTML parse mode.
 * Mirrors `internal/telegram/payload.go::EscapeHTML`.
 */
export function escapeHTML(s: string): string {
	return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/**
 * Build the Telegram summary text + inline keyboard for a parsed email.
 */
export async function renderEmail(
	cache: EmailCache,
	env: RenderEnv,
): Promise<RenderedTelegramMessage> {
	const text = buildSummaryText(cache);
	const domain = env.DOMAIN ?? "localhost";
	const baseURL = `https://${domain}`;
	const reply_markup = {
		inline_keyboard: [
			[
				{
					text: "View as TXT",
					url: `${baseURL}/email/${cache.id}?mode=text`,
				},
				{
					text: "View as HTML",
					url: `${baseURL}/email/${cache.id}?mode=html`,
				},
			],
		],
	};

	return {
		text,
		parse_mode: "HTML",
		reply_markup,
		link_preview_options: { is_disabled: true },
	};
}

function buildSummaryText(cache: EmailCache): string {
	const body = (cache.text ?? "").slice(0, BODY_PREVIEW_LEN);
	const date = new Date().toUTCString();

	const headerLines = [
		`From: ${escapeHTML(cache.from)}`,
		`To: ${escapeHTML(cache.to)}`,
		`Subject: ${escapeHTML(cache.subject)}`,
		`Date: ${escapeHTML(date)}`,
	];

	const escapedBody = escapeHTML(body);
	const parts: string[] = [...headerLines];
	if (escapedBody.length > 0) {
		parts.push("", escapedBody);
	}

	let text = parts.join("\n");
	if (text.length > MAX_MESSAGE_LEN) {
		text = text.slice(0, MAX_MESSAGE_LEN);
	}
	return text;
}
