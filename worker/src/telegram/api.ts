/**
 * Telegram Bot API client.
 *
 * Adapted from upstream `TBXark/mail2telegram` (`src/telegram/api.ts`) and
 * pinned by task 3 of `.omo/plans/worker-owns-telegram-delivery.md`.
 *
 * Lite-mail only needs `sendMessage` over JSON; we intentionally do NOT
 * import upstream's multipart / file-upload path, the `Proxy`-based
 * `Telegram.AllBotMethods` widening, or any of the callback / TMA / webhook
 * product surface. The shape here matches the existing Go client
 * (`internal/telegram/client.go`) so the operator-facing error model
 * (`status 2xx = success, anything else throws`) is identical.
 */

export type SendMessageParams = {
	chat_id: string | number;
	text: string;
	parse_mode?: "HTML" | "MarkdownV2" | "Markdown";
	reply_markup?: unknown;
	link_preview_options?: { is_disabled?: boolean };
	disable_web_page_preview?: boolean;
};

export type SendMessageResponse = {
	ok: boolean;
	result?: {
		message_id: number;
		chat: { id: number | string };
		text?: string;
	};
	description?: string;
	error_code?: number;
};

export type TelegramBotAPI = {
	readonly token: string;
	readonly baseURL: string;
	sendMessage(params: SendMessageParams): Promise<SendMessageResponse>;
};

export function createTelegramBotAPI(token: string): TelegramBotAPI {
	const baseURL = "https://api.telegram.org";

	async function call<R = SendMessageResponse>(
		method: string,
		params: unknown,
	): Promise<R> {
		const url = `${baseURL}/bot${token}/${method}`;
		const response = await fetch(url, {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
			},
			body: JSON.stringify(params),
		});

		const responseText = await response.text();
		let parsed: SendMessageResponse | null = null;
		try {
			parsed = responseText
				? (JSON.parse(responseText) as SendMessageResponse)
				: null;
		} catch {
			parsed = null;
		}

		if (!response.ok) {
			const description = parsed?.description ?? responseText ?? "<empty body>";
			throw new Error(
				`telegram api error: status ${response.status}: ${description}`,
			);
		}

		if (parsed && parsed.ok === false) {
			throw new Error(
				`telegram api returned not ok: ${parsed.description ?? "<no description>"}`,
			);
		}

		return (parsed ?? ({} as SendMessageResponse)) as R;
	}

	return {
		token,
		baseURL,
		sendMessage(params) {
			return call<SendMessageResponse>("sendMessage", params);
		},
	};
}
