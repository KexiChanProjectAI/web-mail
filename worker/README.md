# Lite Mail Cloudflare Email Worker

## Overview

The Cloudflare Email Worker is the front door for inbound mail. It receives email via Cloudflare Email Routing, forwards the raw MIME to the Go service for durable storage, parses the email locally for preview/Telegram purposes, and dispatches a Telegram notification with `View as TXT` / `View as HTML` buttons that point to Worker-hosted preview links.

**Architecture**: Email → Cloudflare Email Worker → (1) raw MIME POST → Go Service Ingest Endpoint, (2) local parse → KV preview cache → Telegram `sendMessage` with buttons.

This Worker owns Telegram delivery and the public preview-link surface. The Go server is intentionally unaware of Telegram.

## Important Notes

- **Do NOT use `message.forward()` or `message.reply()`** - These are not in scope for this implementation
- **`message.raw` is a ReadableStream** - Must be consumed properly using a reader loop
- **Worker message size limit**: 30 MB (Cloudflare Email Routing limit)
- **Worker runtime limits**: up to 30s CPU (paid Standard plan; Free tier is 10ms), 128MB memory (configurable)
- **Preview links are TTL-bound, not permanent.** `/email/:id?mode=text|html` reads from a KV record whose lifetime is controlled by `MAIL_TTL` (default 86400 = 1 day). After the TTL elapses, the link returns 404. These are cache links, not archival links — durable storage remains in the Go server.

## Cloudflare Email Worker API

### Handler Signature

```typescript
export async function email(
  message: ForwardableEmailMessage,
  env: Env,
  ctx: ExecutionContext
): Promise<void>
```

### ForwardableEmailMessage Interface

```typescript
interface ForwardableEmailMessage {
  readonly from: string;       // Envelope MAIL FROM (sender)
  readonly to: string;         // Envelope RCPT TO (recipient)
  readonly headers: Headers;   // Email headers (Subject, Message-ID, etc.)
  readonly raw: ReadableStream; // Raw MIME email content stream
  readonly rawSize: number;    // Size of raw email in bytes
  readonly canBeForwarded: boolean;

  // Actions
  setReject(reason: string): void;
  forward(rcptTo: string, headers?: Headers): Promise<void>;
  reply(message: EmailMessage): Promise<void>;
}
```

**Reference**: https://developers.cloudflare.com/email-routing/email-workers/runtime-api/

### Environment Variables / Secrets

The Worker environment is partitioned into three groups: secrets (Telegram + ingest PSK), plaintext env vars (size limits, TTL, domain), and the KV namespace binding `DB`. All of these are Worker-side only — the Go server has none of them.

| Variable | Type | Description |
|----------|------|-------------|
| `TELEGRAM_TOKEN` | secret | Telegram Bot API token from [@BotFather](https://t.me/BotFather). Leave blank to disable Telegram sending. The Go ingest path still works. |
| `TELEGRAM_ID` | secret | Comma-separated chat IDs that receive forwarded mail. |
| `DOMAIN` | env var | Worker domain used to build preview URLs (e.g. `mail.example.workers.dev`). |
| `MAIL_TTL` | env var | TTL in seconds for KV preview cache entries. Default `86400` (1 day). |
| `MAX_EMAIL_SIZE` | env var | Max raw email size in bytes; default `524288` (512 KiB). |
| `MAX_EMAIL_SIZE_POLICY` | env var | `unhandled` \| `truncate` \| `continue`. Default `truncate`. |
| `DB` | KV binding | KV namespace used for the preview cache. Binding name MUST be `DB`. |
| `INGEST_URL` | secret | Full URL to the Go service ingest endpoint (e.g. `https://mail.example.com/api/ingest`). |
| `WORKER_INGEST_PSK` | secret | Pre-shared key sent in the `X-Lite-Mail-Ingest-PSK` header. |

Set secrets via:

```bash
npx wrangler secret put TELEGRAM_TOKEN
npx wrangler secret put TELEGRAM_ID
npx wrangler secret put INGEST_URL
npx wrangler secret put WORKER_INGEST_PSK
```

Plaintext env vars can go in `[vars]` in `wrangler.toml` or `.dev.vars` for local development:

```toml
[vars]
DOMAIN = "mail.yourdomain.com"
MAIL_TTL = "86400"
MAX_EMAIL_SIZE = "524288"
MAX_EMAIL_SIZE_POLICY = "truncate"
```

### KV Namespace Setup

The preview cache is backed by a Cloudflare KV namespace bound to the `DB` variable. The binding name `DB` is pinned (it matches the upstream mail2telegram contract) and cannot be renamed without forking the Worker code.

Provision the namespace once per environment:

```bash
# Production
npx wrangler kv namespace create DB
# Paste the returned `id` into [[kv_namespaces]] in wrangler.toml

# Preview (used by `wrangler dev` and PR previews)
npx wrangler kv namespace create DB --preview
# Paste the returned `preview_id` into [[kv_namespaces]] in wrangler.toml
```

The expected `[[kv_namespaces]]` block in `wrangler.toml` is:

```toml
[[kv_namespaces]]
binding = "DB"
id = "your-kv-namespace-id-here"
preview_id = "your-preview-kv-namespace-id-here"
```

Without the `DB` binding, the Worker will fail at deploy time with `DB: KVNamespace` not satisfied.

## Cutover checklist

This is the staged rollout order for moving Telegram delivery from the Go server to this Worker. Execute the steps **in order**. Each step is reversible; the rollback at the end of the list restores the pre-migration state. The same 6 steps are documented in the top-level `README.md` under "Migration cutover (worker-owns-telegram-delivery)" — this Worker-side checklist mirrors it from the Worker's point of view.

### Pre-flight

Run the full cross-repo verification surface once on a clean checkout:

```bash
make verify
```

This runs `go vet ./...`, `go test ./...`, `cd worker && npm test`, and `cd worker && npm run build` in order and fails fast on the first error. Capture the output as your baseline evidence before cutting over.

### Cutover steps

1. **Provision the Worker KV namespace (`DB`) and HTTP route in `wrangler.toml`.**

   ```bash
   npx wrangler kv namespace create DB
   npx wrangler kv namespace create DB --preview
   ```

   Paste the returned `id` and `preview_id` into `[[kv_namespaces]]` in `wrangler.toml`. The binding name MUST be `DB` (pinned by the upstream contract; renaming requires forking the Worker code). Also configure the `[[routes]]` block (e.g. `mail.yourdomain.com/email/*`) so the Worker's `/email/:id?mode=text|html` surface is reachable. Cloudflare Email Routing is configured in the Email Routing dashboard and is **not** the same as the Worker route.

2. **Deploy the Worker code with `TELEGRAM_TOKEN` and `TELEGRAM_ID` unset.**

   ```bash
   npx wrangler deploy
   ```

   At this point the Go ingest path is fully wired and active, but the Worker is silent on Telegram: when `TELEGRAM_TOKEN` is empty/undefined, `email()` skips the Telegram send branch and the email still flows Go-ingest → KV-cache → (no Telegram). This proves the Worker is observably live without exposing any user-visible behavior change yet. The existing Worker integration test suite (`npm test`) covers this exact branch with `TELEGRAM_TOKEN` and `TELEGRAM_ID` absent — re-run it to lock in the proof:

   ```bash
   npm test
   ```

3. **Deploy the Go cleanup release (this branch).**

   Roll out the Go server build that drops `internal/telegram`, the `/share/{token}` routes, the `share_tokens` and `telegram_deliveries` tables (via migration `003_drop_share_and_delivery_tables`), and the `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` config keys. The Go server is now the durable web-mail ingest / storage / UI/API system of record and is intentionally unaware of Telegram.

4. **Set Worker Telegram secrets/env to activate sending.**

   ```bash
   npx wrangler secret put TELEGRAM_TOKEN
   npx wrangler secret put TELEGRAM_ID
   ```

   The Worker's `email()` now sees both env vars set, so each accepted ingest writes the `EmailCache` to the `DB` KV namespace and dispatches a Telegram `sendMessage` to every chat id in `TELEGRAM_ID`.

5. **Verify preview URLs and Telegram messages.**

   Send a real test email through Cloudflare Email Routing and confirm:
   - The Go server received and stored the message (web-mail UI shows it).
   - Telegram delivered a message with the two inline URL buttons (`View as TXT`, `View as HTML`).
   - Following either button returns `200` with the corresponding body, backed by the Worker's `/email/:id?mode=text|html` surface.
   - After `MAIL_TTL` elapses (default 86400s = 1 day), the same URL returns `404`.

   **Reminder:** `MAIL_TTL` makes previews TTL-bound cache links, not permanent archival links. The Worker's KV records are deleted automatically when the TTL elapses — durable storage remains in the Go server. Plan retention, link-sharing semantics, and any user-facing copy accordingly.

6. **Done.** Migration complete.

### Rollback

If anything in step 4–5 misbehaves, reverse the cutover in two commands. The migration is reversible because the Go server still has the previous-release artifact in its deploy history and migration `003_*.down.sql` recreates `share_tokens` and `telegram_deliveries` verbatim from `002_*.up.sql`.

1. Redeploy the **previous Go release** (the build before the cleanup commit) so the Go server once again owns Telegram delivery and serves `/share/{token}` URLs.

2. Unset the Worker's Telegram secrets so the Worker goes silent:

   ```bash
   npx wrangler secret delete TELEGRAM_TOKEN
   npx wrangler secret delete TELEGRAM_ID
   ```

   The Worker continues to serve `/email/:id?mode=text|html` (the KV cache and route are still provisioned) but does not send Telegram because the secrets are absent — and that's the same opt-in behavior as the pre-migration Worker from step 2.

3. (If the database migration has already run) Apply the down migration to re-create the `share_tokens` and `telegram_deliveries` tables. The Go server's embed-FS migration loader exposes the down migration; the standard rollback tooling applies it.

After rollback, the system is in the pre-migration state.

## Preview Surface

### `GET /email/:id?mode=text|html`

After a successful ingest, the Worker writes a preview record (`{id, messageId, from, to, subject, text, html}`) to the `DB` KV namespace with `expirationTtl = MAIL_TTL`. The `id` is a UUID emitted by the parser.

The HTTP fetch handler serves:

- `GET /email/<id>?mode=text` → `200 text/plain; charset=utf-8` with the plain-text body
- `GET /email/<id>?mode=html` → `200 text/html; charset=utf-8` with the HTML body

Other response shapes:

| Case | Response |
|---|---|
| `?mode=text` (cache hit) | `200` + `text/plain; charset=utf-8` |
| `?mode=html` (cache hit) | `200` + `text/html; charset=utf-8` |
| missing `mode` query param | `404 Not found` |
| unsupported `?mode=json` (or any value other than `text`/`html`) | `404 Not found` |
| missing cache entry (id never written OR TTL expired) | `404 Not found` |

The 404-on-missing semantics are deliberate: Telegram buttons always include `?mode=text|html`, and the public preview is meant to behave like a real link (404 on broken), not a soft-fail. Operators can read a 404 in access logs immediately instead of getting silent empty pages.

The route must be reachable on a public hostname. The `[[routes]]` block in `wrangler.toml` maps the route pattern (e.g. `mail.yourdomain.com/email/*`) to this Worker. Cloudflare Email Routing configuration is separate (configured in the Email Routing dashboard) and does NOT use this route.

## Development

### Prerequisites

- Node.js 18+
- npm

### Install Dependencies

```bash
npm install
```

### Local Secrets (.dev.vars)

For local development, create a `.dev.vars` file from the example template:

```bash
cp .dev.vars.example .dev.vars
# Edit .dev.vars with your local values
```

`.dev.vars` is the Cloudflare Workers equivalent of a `.env` file and is automatically loaded by `wrangler dev`. Never commit `.dev.vars` to git.

For production, always use `wrangler secret put`.

### Run Tests

```bash
npm test
```

Tests use Vitest in single-run mode (`vitest run`). The test suite covers:
- Email handler invocation
- Raw MIME stream reading
- Local parse → preview cache write → Telegram POST pipeline
- POST to ingest endpoint with correct headers
- Authentication failure handling (401)
- Message size limit handling (413)
- Worker misconfiguration detection (missing env vars)
- Stream reading failures
- `GET /email/:id?mode=text|html` preview retrieval (404 on missing/expired)
- Telegram send failure does not call `message.setReject(...)`
- Duplicate ingest response from Go does NOT write a cache entry and does NOT send Telegram

### Run Locally

```bash
npx wrangler dev
```

Note: Local development cannot receive actual emails. Use `wrangler dev --local` to test the Worker runtime locally.

## Deployment

### Deploy to Cloudflare

```bash
npm run deploy
```

or

```bash
npx wrangler deploy
```

### Set Secrets

Before deploying, set the required secrets:

```bash
npx wrangler secret put TELEGRAM_TOKEN
npx wrangler secret put TELEGRAM_ID
npx wrangler secret put INGEST_URL
npx wrangler secret put WORKER_INGEST_PSK
```

### Provision the KV namespace

If not already done, create the `DB` KV namespace and paste the id into `wrangler.toml`:

```bash
npx wrangler kv namespace create DB
npx wrangler kv namespace create DB --preview
```

### Configuration

The Worker is configured via `wrangler.toml`. Key settings:

```toml
name = "lite-mail-worker"
main = "src/index.ts"

[limits]
cpu_ms = 100  # Sufficient for reading stream, local parse, KV write, and Telegram POST
```

Note: `INGEST_URL`, `WORKER_INGEST_PSK`, `TELEGRAM_TOKEN`, and `TELEGRAM_ID` must **only** be set via `wrangler secret put`, never in `wrangler.toml`. See [Set Secrets](#set-secrets) above.

## Architecture Details

### Email Flow

1. Cloudflare Email Routing receives an inbound email
2. The Worker `email()` handler is invoked with a `ForwardableEmailMessage`
3. The raw MIME stream is consumed via `collectStream()` helper into a `Uint8Array`
4. The Worker parses the email locally with `postal-mime` + `html-to-text` to produce an `EmailCache` (id, from, to, subject, text, html) for preview + Telegram
5. A POST is made to the Go ingest endpoint with:
   - `Content-Type: message/rfc822`
   - `X-Lite-Mail-Ingest-PSK: <psk>`
   - Raw MIME as body
6. The Worker waits for the ingest POST to complete before acknowledging the email event
7. If the Go response is `{"status":"accepted"}` (or an unparseable-but-2xx), the Worker writes the `EmailCache` to the `DB` KV namespace with `expirationTtl = MAIL_TTL`, then dispatches a Telegram `sendMessage` to every chat id in `TELEGRAM_ID` with the rendered summary and two inline URL buttons (`View as TXT`, `View as HTML`) pointing to `https://<DOMAIN>/email/<id>?mode=text|html`
8. If the Go response is `{"status":"duplicate"}`, the Worker does NOT write a cache entry and does NOT send Telegram
9. The Worker only calls `message.setReject(...)` when ingest itself failed (auth 401, size 413, network, timeout, 5xx after retry). Telegram or cache write failures after accepted ingest are logged and swallowed — the email is already durably stored in Go

### Error Handling

- **Missing env vars** (`INGEST_URL` or `WORKER_INGEST_PSK`): Calls `message.setReject()` to reject the email
- **Stream read failure**: Logs error and rejects
- **Local parse failure**: Synthesizes a minimal cache (`id` from `crypto.randomUUID()`, fallback subject/text) so the cache write + Telegram send still happen. Go still receives the full raw MIME. The `postal-mime` library is lenient and usually returns an empty object rather than throwing; this catch is a belt-and-suspenders guard
- **401 from ingest**: Logs authentication failure and rejects the email
- **413 from ingest**: Logs message size error and rejects the email
- **Other non-OK status from ingest**: Logs error and rejects the email (single retry on 5xx)
- **Fetch timeout**: 30 second timeout on the POST request
- **KV cache write failure after accepted ingest**: Logged and swallowed. The email is already in Go; the preview link will be missing but storage is intact
- **Telegram send failure after accepted ingest**: Logged per chat and swallowed. The loop continues to the next chat id; one bad chat id never blocks delivery to the others
- **Fetch timeout (preview route)**: None — preview reads are bounded by KV read latency

### ReadableStream Collection

The `collectStream()` helper properly handles the ReadableStream:

```typescript
async function collectStream(stream: ReadableStream): Promise<Uint8Array> {
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
  }
  reader.releaseLock();

  const totalLength = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.length;
  }
  return result;
}
```
