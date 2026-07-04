// Minimal ambient declaration for `html-to-text` (the package does not
// ship its own .d.ts and `@types/html-to-text` is not on the registry).
// The `convert` function is the only export we use in `src/mail/parse.ts`.
//
// Reference: https://github.com/html-to-text/node-html-to-text v10
declare module "html-to-text" {
	export interface HtmlToTextOptions {
		wordwrap?: number | false;
		whitespaceCharacters?: string;
		preserveNewlines?: boolean;
		preserveIndentation?: boolean;
		[key: string]: unknown;
	}

	export function convert(html: string, options?: HtmlToTextOptions): string;
}
