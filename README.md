# lite-mail

PSK-based email service with Go server and Cloudflare Worker.

## Commands

### Go Server

```bash
go build ./...
go test ./...
go run ./cmd/server
```

### Cloudflare Worker

```bash
cd worker
npm install
npm run build
npm run deploy
npm test
```

## Configuration

Copy `.env.example` to `.env` and configure:

- `DATABASE_URL` - MariaDB connection string
- `DATA_DIR` - Data storage directory
- `PUBLIC_BASE_URL` - Public URL of the service
- `MAX_MESSAGE_BYTES` - Max message size (default 26214400)
- `SESSION_COOKIE_NAME` - Session cookie name
- `SESSION_TTL_HOURS` - Session TTL in hours
- `NORMAL_USER_PSK` - PSK for normal users
- `ADMIN_PSK` - PSK for admin access
- `WORKER_INGEST_PSK` - PSK for worker ingest endpoint
## Worker-owned Telegram forwarding

Telegram delivery and the public preview-link surface live in the Cloudflare Worker, not in this Go server. The Go server is the durable web-mail ingest / storage / UI/API system of record and is intentionally unaware of Telegram.

The Worker:

- Receives inbound email via Cloudflare Email Routing.
- Forwards the raw MIME message to the Go server's `/api/ingest` endpoint using the existing `WORKER_INGEST_PSK` contract.
- Parses the email locally, caches a preview record in its KV namespace, and dispatches a Telegram message with two inline URL buttons (`View as TXT`, `View as HTML`) that point to the Worker.
- Runs ingest and Telegram **in parallel and independently**: Go being unreachable still notifies Telegram; Telegram being down still ingests into Go.

**Preview URLs are TTL-bound, not permanent.** Each `/email/:id?mode=text|html` link is backed by a KV record whose lifetime is controlled by the Worker's `MAIL_TTL` env var (default 86400 seconds = 1 day). After the TTL elapses, the link returns 404. These are cache links, not archival links — durable storage remains in the Go server.

**Old Go `/share/{token}` links are intentionally retired.** The Go server no longer serves `/share/{token}`, `/share/{token}/html`, or `/share/{token}/txt`. Anyone hitting an old share link should be redirected to the Worker preview surface (or considered broken if the TTL has elapsed).

Telegram configuration (`TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`, `MAIL_TTL`, `MAX_EMAIL_SIZE`, `MAX_EMAIL_SIZE_POLICY`, the `DB` KV binding) lives entirely in the Worker — see `worker/README.md` for setup. The Go server has no `TELEGRAM_*` env vars.

## Migration cutover (worker-owns-telegram-delivery)

This is the staged rollout order for moving Telegram delivery from the Go server to the Cloudflare Worker. Execute the steps **in order**. Each step is reversible; the rollback at the end of the list restores the pre-migration state.

### Pre-flight

Run the full cross-repo verification surface once on a clean checkout to prove the migration ships green:

```bash
make verify
```

This runs `go vet ./...`, `go test ./...`, `cd worker && npm test`, and `cd worker && npm run build` in order and fails fast on the first error. Capture the output as your baseline evidence before cutting over.

### Cutover steps

1. **Provision the Worker KV namespace (`DB`) and HTTP route in `wrangler.toml`.**

   The Worker's preview cache is backed by a KV namespace bound to the name `DB` (the binding name is pinned by the upstream contract and cannot be renamed). Provision the namespace once per environment and paste the returned `id` / `preview_id` into `[[kv_namespaces]]`:

   ```bash
   npx wrangler kv namespace create DB
   npx wrangler kv namespace create DB --preview
   ```

   Also configure the `[[routes]]` block (e.g. `mail.yourdomain.com/email/*`) so the Worker's `/email/:id?mode=text|html` surface is reachable. Cloudflare Email Routing itself is configured in the Email Routing dashboard and is **not** the same as the Worker route.

2. **Deploy the Worker code with `TELEGRAM_TOKEN` and `TELEGRAM_ID` unset.**

   `cd worker && npx wrangler deploy` with the secrets intentionally empty. At this point the Go ingest path is fully wired and active, but the Worker is silent on Telegram: when `TELEGRAM_TOKEN` is empty/undefined, `email()` skips the Telegram send branch and the email still flows Go-ingest → KV-cache → (no Telegram). This proves the Worker is observably live without exposing any user-visible behavior change yet.

3. **Deploy the Go cleanup release (this branch).**

   Roll out the Go server build that drops `internal/telegram`, the `/share/{token}` routes, the `share_tokens` and `telegram_deliveries` tables (via migration `003_drop_share_and_delivery_tables`), and the `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` config keys. The Go server is now the durable web-mail ingest / storage / UI/API system of record and is intentionally unaware of Telegram.

4. **Set Worker Telegram secrets/env to activate sending.**

   ```bash
   npx wrangler secret put TELEGRAM_TOKEN
   npx wrangler secret put TELEGRAM_ID
   ```

   The Worker's `email()` now sees both env vars set, so each inbound email writes the `EmailCache` to the `DB` KV namespace and dispatches a Telegram `sendMessage` to every chat id in `TELEGRAM_ID`, in parallel with the Go ingest POST. Either path failing does not skip the other.

5. **Verify preview URLs and Telegram messages.**

   Send a real test email through Cloudflare Email Routing and confirm:
   - The Go server received and stored the message (web-mail UI shows it).
   - Telegram delivered a message with the two inline URL buttons (`View as TXT`, `View as HTML`).
   - Following either button returns `200` with the corresponding body, backed by the Worker's `/email/:id?mode=text|html` surface.
   - After `MAIL_TTL` elapses (default 86400s = 1 day), the same URL returns `404`. Preview URLs are TTL-bound cache links, not permanent archival links — durable storage remains in the Go server.

6. **Done.** Migration complete.

### Rollback

If anything in step 4–5 misbehaves, reverse the cutover in two commands. The migration is reversible because the Go server still has the previous-release artifact in its deploy history and migration `003_*.down.sql` recreates `share_tokens` and `telegram_deliveries` verbatim from `002_*.up.sql`.

1. Redeploy the **previous Go release** (the build before the cleanup commit) so the Go server once again owns Telegram delivery and serves `/share/{token}` URLs.

2. Unset the Worker's Telegram secrets so the Worker goes silent:

   ```bash
   npx wrangler secret delete TELEGRAM_TOKEN
   npx wrangler secret delete TELEGRAM_ID
   ```

   (Optional) To also re-route Telegram delivery through the Go server, restore the old `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` env vars on the Go release. The previous Go release's `Config.TelegramEnabled()` will pick them up.

3. If the database migration has already run, run the down migration to re-create the `share_tokens` and `telegram_deliveries` tables:

   ```sql
   -- (the contents of internal/db/migrations/003_drop_share_and_delivery_tables.down.sql)
   ```

   The Go server's embed-FS migration loader exposes the down migration; the standard rollback tooling applies it.

After rollback, the system is in the pre-migration state. The Worker continues to serve `/email/:id?mode=text|html` (the KV cache and route are still provisioned) but does not send Telegram because the secrets are absent — and that's the same opt-in behavior as the pre-migration Worker from step 2.

### Verification commands

- **Happy path (after each step above):** `make verify` — must pass end-to-end.
- **Worker roll-back sanity:** `cd worker && npm test` with `TELEGRAM_TOKEN` and `TELEGRAM_ID` absent — must still pass. This is the existing integration-test proof that the Worker is opt-in for Telegram and degrades to "Go ingests, Worker silent" when secrets are missing.
