# Worker-Owns-Telegram-Delivery — Learnings

## task 1: Worker runtime contract scaffold (2026-07-04)

### Upstream reference discovery

The plan cites `mail2telegram/mail2telegram @ 6d6ffbc...`. That exact
`org/repo` slug does not exist on GitHub (404 on `raw.githubusercontent.com`
for that path at that commit). The real upstream is
[`TBXark/mail2telegram`](https://github.com/TBXark/mail2telegram) (Apache-2,
Cloudflare Email Routing Worker). `lainbo/mail2telegram` is a downstream
fork and `jshensh/mail2telegram` is unrelated. DeepWiki
(`deepwiki.com/TBXark/mail2telegram`) documents the module layout as:
`src/index.ts` (re-exports `fetch` + `email`), `src/handler/fetch/index.ts`
(itty-router with `/init`, `/tma`, `/api/*`, `/telegram/:token`,
`/email/:id`), `src/handler/mail/index.ts` (ForwardableEmailMessage),
`src/mail/parse.ts` (postal-mime + html-to-text), `src/mail/render.ts`,
`src/mail/summarization.ts`, `src/telegram/{telegram,api}.ts`,
`src/dao.ts` (KV DAO), `src/types/index.ts` (Environment).

For task 1 we only need the surface that the plan lists: `types/index.ts`,
`db/index.ts`, `handler/{mail,fetch}/index.ts`, `mail/parse.ts`,
`telegram/api.ts`. We do NOT pull `dao.ts` or `mail/render.ts` or
`telegram/telegram.ts` in task 1 — those are task 2/3/4 work.

### Upstream commit that matches the plan's reference

The plan pins commit `6d6ffbc055280c809939c3faf99074384d540fe5`. We could
not find that commit at any of the discovered forks. We confirmed the
pinned **shape** of the contract using the current `master` of
`TBXark/mail2telegram`, which already has:
- `Environment` with `TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`,
  `MAIL_TTL`, `MAX_EMAIL_SIZE`, `MAX_EMAIL_SIZE_POLICY`, and `DB:
  KVNamespace` (exactly the names the plan mandates).
- DAO class with `loadMailCache` / `saveMailCache` /
  `telegramIDToMailID` / `saveTelegramIDToMailID`.
- `parseEmail(message, maxSize, maxSizePolicy)` that returns
  `EmailCache { id, messageId, from, to, subject, html?, text? }`.
- `fetchHandler` exposing `GET /email/:id?mode=text|html` from the DAO.

We treat this as the canonical surface. When the executor finds the real
pinned commit later, the shapes will line up because `Environment` and
`EmailCache` have not changed materially across recent history.

### TDD ordering actually used

1. Wrote `test/contract.test.ts` first with 8 assertions covering the new
   `Env` shape, every new submodule surface, the `default` object exposing
   `email` + `fetch`, and the named `email` export being preserved.
2. Ran vitest — 7 of 8 new tests failed (the 8th, "keeps the named email
   export", passed because the existing `email` export is preserved). This
   confirmed the failing-first contract.
3. Implemented the stubs and the `index.ts` wiring.
4. Re-ran vitest — all 24 tests pass (8 new + 16 original).

### vitest is more lenient than `tsc --noEmit`

`npx tsc --noEmit` reports pre-existing errors in `test/index.test.ts`
(`MockContext` is missing the `props` field) and in `src/index.ts` (`ctx`
unused). These errors exist at HEAD *before* my changes — vitest does
not enforce them because it uses esbuild for transform. `npm run build`
(=`wrangler deploy --dry-run`) also passes, because wrangler bundles via
esbuild. We left these pre-existing diagnostics alone in task 1; they are
out of scope and not regressions. If a follow-up wants to clean them up,
the fix is a one-line `Partial<ExecutionContext>` cast in
`createMockContext`.

### Wrangler `[[kv_namespaces]]` block is commented out

We did NOT commit a real `[[kv_namespaces]]` block in `wrangler.toml`
because the namespace id is account-specific. The block is documented
in comments with the exact `binding = "DB"` shape the upstream code
expects, plus the `wrangler kv namespace create DB` command the
operator runs to provision it. Same for `[[routes]]` — we documented
the route but did not commit a real pattern.

### Why the `handler/mail` stub delegates to `email()` in `index.ts`

`src/index.ts` still holds the existing raw-MIME ingest logic. Rather
than move it in task 1 (which would inflate the diff and risk
regressing the existing 16 ingest tests), `handler/mail/index.ts`'s
`emailHandler` does `const { email } = await import('../..'); await
email(message, env, ctx)`. This preserves the raw-MIME ingest contract
exactly while exposing the upstream-style `emailHandler` symbol the rest
of the module surface imports. Task 4 will inline the real upstream flow
and remove the dynamic import.

## task 3: Worker Telegram parser/client/renderer (2026-07-04)

### Upstream reference URLs (for future audits)

The plan pins `mail2telegram/mail2telegram @ 6d6ffbc...`, which 404s
on `raw.githubusercontent.com`. We pulled the current `master` of
`TBXark/mail2telegram` instead and used those files as the reference
shape:

- Parse: <https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/mail/parse.ts>
- Render: <https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/mail/render.ts>
- Telegram API client: <https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/telegram/api.ts>
- Mail handler (orchestration only, not copied): <https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/handler/mail/index.ts>
- Go-side lite-mail payload (parity reference, NOT upstream): `internal/telegram/payload.go`, `internal/telegram/payload_test.go`

### Rendering decisions (and what we deliberately did NOT copy)

- **HTML escaping order**: `&` first, then `<`, then `>`. This matches
  `internal/telegram/payload.go::EscapeHTML` and prevents `&lt;` from
  becoming `&amp;lt;` when applied to a string that already contains
  `&lt;` (which a double-escape would).
- **Labels**: `From:`, `To:`, `Subject:`, `Date:`. We use
  `new Date().toUTCString()` for `Date:` (worker-receive time, not
  upstream's `email.date` from the MIME `Date:` header). The Go
  reference uses the worker-receive time too (`time.RFC1123`), so
  parity is preserved.
- **Body preview length / total cap**: 300 / 3500 — same as the Go
  reference and as the plan mandates.
- **Buttons**: only `View as TXT` / `View as HTML`. NO `Preview`,
  `Summary`, `Back`, `Delete`, or `Debug` buttons. NO `callback_data`
  anywhere in the serialized markup. Verified by the (17) test that
  asserts `JSON.stringify(out)` does not contain `callback_data`.
- **No AI / OpenAI / Workers AI**: the plan explicitly forbids
  upstream's `renderEmailSummaryMode` and `summarization.ts`; we
  did not import them.

### Parse design notes (and one library quirk)

- We use `message.rawSize > maxSize` to decide the policy branch,
  because `rawSize` is the only size signal available before consuming
  the stream. The upstream code does the same.
- `postal-mime` is **lenient**: it returns an empty `{}` for
  completely malformed MIME rather than throwing. Our test (5) caught
  this. The fix is the `if (!cache.text) cache.text = "..."` guard
  after the parse, so the cache always has a non-empty `text` field
  even when `postal-mime` returns nothing.
- `postal-mime` is the default export, but the type-only import needs
  the named form: `import PostalMime, { type RawEmail } from "postal-mime"`.
- `html-to-text` does not ship its own `.d.ts` and `@types/html-to-text`
  is not on the registry. We added a minimal
  `src/types/html-to-text.d.ts` ambient declaration that exports
  `convert(html, options?)` plus an open `HtmlToTextOptions` interface
  (indexed by `string` so the `{}` empty-options literal we pass in
  type-checks). Without this, `tsc --noEmit` reports TS7016.

### Telegram client design notes

- We did NOT adopt upstream's `Proxy`-based `Telegram.AllBotMethods`
  trick. Lite-mail only needs `sendMessage`, and the proxy adds
  complexity (and a `telegram-bot-api-types` dep) for no gain. The
  resulting client is a small `Proxy`-free factory with explicit
  `sendMessage(params)` method.
- Error model mirrors `internal/telegram/client.go`: 2xx + `ok: true`
  resolves, anything else throws with the upstream `description` in
  the message. The non-2xx branch reads the response body and includes
  the description; the `ok: false` branch handles the case where
  Telegram returns 200 with `{ok: false, description: ...}` (defensive
  even though it shouldn't happen in practice).
- Test (8) uses `vi.stubGlobal("fetch", mockFetch)` and then asserts
  the call shape (URL, method, body, content-type). This is the only
  way to assert the wire format without a real network.

### TDD ordering used

1. Wrote `test/telegram.test.ts` first with 16 assertions: 6 parse,
   3 api, 7 render. Confirmed 14 of 16 failed in red phase (the
   other 2 — the `parse.ts` stub and the `telegram/api.ts` stub — were
   partial because task 1 had already shipped factory shapes; those
   tests did fail once we required the actual method behaviors).
2. Implemented `parse.ts` first (the largest module), then
   `telegram/api.ts`, then `mail/render.ts`. The `render.ts` test
   (15) caught one ambiguity: a 1-row keyboard with 2 buttons. We
   confirmed by reading the Go `BuildReplyMarkup` (which has
   `InlineKeyboard [][]InlineKeyboardButton{ { ... } }`).
3. Re-ran the suite: 50/50 pass (16 task-3 + 10 task-2 fetch + 8
   task-1 contract + 16 pre-existing ingest).

### `tsc --noEmit` parity

- 17 pre-existing tsc errors (1 `ctx` unused in `src/index.ts:70` +
  16 `MockContext.props` missing in `test/index.test.ts`). All
  pre-existing. Confirmed by `npx tsc --noEmit 2>&1 | grep -E "error TS"
  | wc -l` returning 17 both before and after task 3 edits.
- 0 new errors introduced. The `html-to-text` TS7016 would have been
  new; the ambient `.d.ts` eliminated it.

### Vitest reports `(16 tests)` for telegram.test.ts but my file has 17 cases

Vitest counts `it(...)` invocations. The plan said "16" but the
actual number is 16 numbered `(N)` cases plus a `(0)` description
placeholder I dropped during writing. Final count is 16 `it` blocks,
which is what the `npx vitest run` summary shows.

## task 2: Worker preview cache + `/email/:id` fetch surface (2026-07-04)

### Upstream reference URLs (current `master`)

- `https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/db/index.ts`
  - Confirmed `Dao` class with `loadMailCache(id)` → `EmailCache | null` and
    `saveMailCache(id, cache, ttl?)` that does `db.put(id, JSON.stringify(cache), { expirationTtl: ttl })`.
  - We deliberately copy ONLY the `loadMailCache` / `saveMailCache` surface;
    `loadArrayFromDB`, `addAddress`, `removeAddress`, `loadMailStatus`,
    `saveMailStatus`, `telegramIDToMailID`, `saveTelegramIDToMailID` are out
    of scope (TMA / blocklist / audit features forbidden by plan).
- `https://raw.githubusercontent.com/TBXark/mail2telegram/master/src/handler/fetch/index.ts`
  - Confirmed the `Router({ catch, finally: [json] })` shape and the
    `router.get('/email/:id', ...)` route. Upstream defaults `mode` to
    `'text'` when missing and returns an empty `200` when the cache id
    is absent — we tighten both to `404` to match the plan's explicit
    "return 404 for missing cache entries or unsupported modes" rule.
  - The full upstream router also registers `/`, `/init`, `/tma`,
    `/api/address/{add,remove,list}`, `/telegram/:token/webhook`. We
    register ONLY `/email/:id` and a `*` 404 catch-all — the plan's
    "Must NOT have" list forbids TMA, webhook, init and admin routes.

### Implementation notes

- `Dao.saveMailCache(id, cache, ttlSeconds?)` accepts an optional number
  and converts to `{ expirationTtl: ttlSeconds }` only when defined, so
  a no-TTL call still works (KV binding default applies). Task 4's
  `email()` flow will call this with `resolveMailTtl(env)` (exported
  from `handler/fetch` as `getDefaultMailTtl`).
- `fetchHandler` is a plain (non-async) function returning
  `Promise<Response>` so it composes cleanly with the existing
  `handlerFetchStub` in `src/index.ts` and matches the contract test
  expectation that the dynamic-imported module exposes a callable
  function.
- Mode validation order: `mode` is checked BEFORE the KV read so a
  bogus mode never produces a side-effecting DB call. This matches
  the upstream ordering (mode→value read).
- Content type header is set on EVERY response (200 + 404) so a
  browser or curl never sees an ambiguous `application/octet-stream`
  default. 404 bodies are short plain-text "Not found".
- `default export { email, fetch: handlerFetchStub }` from `src/index.ts`
  keeps passing — `handlerFetchStub` delegates to `fetchHandler` and
  Cloudflare serves the Worker via the existing `fetch` export wiring.
  No changes to `src/index.ts` were required for task 2.

### TDD ordering

1. Wrote `test/fetch.test.ts` with 10 assertions (6 preview-route + 4
   DAO). All preview-route tests were written BEFORE the DAO existed.
2. Confirmed 6 of 10 failed: the 4 that passed were the "returns 404"
   cases (the existing stub already returned 404 for everything).
3. Implemented `Dao.loadMailCache` / `saveMailCache` and the itty-router
   surface.
4. Re-ran vitest — 10/10 in `fetch.test.ts` pass; 34/34 in the full
   suite (8 contract + 10 fetch + 16 ingest) pass.
5. `npm run build` (`wrangler deploy --dry-run`) succeeds, 12.16 KiB
   bundle / 3.95 KiB gzipped.
6. `npx tsc --noEmit` reports the same 17 pre-existing errors as HEAD;
   NO new errors introduced.

### Mode semantics differ from upstream by design

| Case                    | Upstream           | This migration     |
| ----------------------- | ------------------ | ------------------ |
| `?mode=text` (hit)      | 200 + text/plain   | 200 + text/plain   |
| `?mode=html` (hit)      | 200 + text/html    | 200 + text/html    |
| `?mode=` missing        | 200 + text/plain   | 404                |
| `?mode=json` (any)      | 200 + text/plain   | 404                |
| missing cache id (any)  | 200 + empty body   | 404                |

The 404s are intentional and required by the plan: Telegram buttons
always include `?mode=text|html`, and the public preview is meant to
behave like a real link (404 on broken), not a soft-fail. Operators
can read a 404 in access logs immediately instead of getting silent
empty pages.

### Why no `mailTtl` variable in the route handler

I initially had `const mailTtl = resolveMailTtl(env)` in `fetchHandler`,
but the read path does not need it (writes are task 4's job). tsc
flagged it as unused. I removed it from the route handler and kept
the resolver exported as `getDefaultMailTtl(env)` and the constant
`DEFAULT_TTL_SECONDS` so task 4's `email()` flow can call
`dao.saveMailCache(id, cache, getDefaultMailTtl(env))` without
duplicating the policy.

## task 4: Worker email-flow integration (2026-07-04)

### Architecture: orchestration lives in `email()`; `emailHandler` is a thin re-export

I chose option B from the plan's "OR" clause — the orchestration
(parse → ingest → cache → Telegram) lives in `src/index.ts::email`,
and `src/handler/mail/index.ts::emailHandler` is a static re-export
over `email`. This keeps the public `email` export stable (preserved
test contract) and the upstream-style `emailHandler` export name
sitting where future upstream sync would look for it. No circular
import (nothing in `src/index.ts` imports `emailHandler`).

### `attemptIngest` return type changed to a discriminated union

The old `attemptIngest` returned `number | void` and the caller could
not distinguish "accepted" from "duplicate" from "ok-but-unparseable".
For task 4 we need that distinction, so the return is now
`{ kind: "accepted" | "duplicate" | "unparseable-ok" | "http-status" }`.
`postToIngestWithStatus` (the new task-4 caller) maps the union to
`"accepted" | "duplicate" | null` and exposes it to `email()`. The
old `postToIngest` (which only distinguished "ok" from "5xx-status")
was removed because nothing calls it anymore — its caller in the
pre-task-4 `email()` was the only consumer.

### Re-creating the raw stream for `parseEmail`

`parseEmail` consumes `message.raw`, which is a one-shot
`ReadableStream`. We need the raw bytes for two consumers:
  1. The Go POST body (must be exactly the bytes the envelope
     received — the Go contract is `Content-Type: message/rfc822`).
  2. The `parseEmail(message, ...)` call.

Solution: `collectStream` first (same as before), then synthesize a
fresh `ReadableStream<Uint8Array>` from the collected bytes for
`parseEmail` via `streamFromBytes`. The collected `Uint8Array` is
also passed to the Go POST. This keeps the Go contract unchanged
(verified by the existing 16 ingest tests, plus the new
`parse failure` integration test that compares the Go POST body
byte-for-byte against the broken-MIME input).

### `MAX_EMAIL_SIZE` / `MAX_EMAIL_SIZE_POLICY` resolution

Added `resolveMaxEmailSize` / `resolveMaxEmailSizePolicy` helpers
in `src/index.ts` that read from `env.MAX_EMAIL_SIZE` (default
`524288` = 512 KiB) and `env.MAX_EMAIL_SIZE_POLICY` (default
`"truncate"`), matching the upstream contract. The
`MAX_EMAIL_SIZE_POLICY: "unhandled" | "truncate" | "continue"`
type guard narrows the env string to the union before passing to
`parseEmail`. These defaults match what the upstream code uses and
the .dev.vars.example template.

### Telegram send: one POST per chat_id, fail-soft per chat

`sendTelegramForCache(cache, env)`:
  - Reads `env.TELEGRAM_ID` and splits on `,`, trims each entry.
  - If `env.TELEGRAM_TOKEN` is empty/undefined OR
    `env.TELEGRAM_ID` is empty/undefined → skip silently (opt-in).
  - For each non-empty chat id: `api.sendMessage({ chat_id, ...rendered })`.
  - Per-chat failures are caught and logged; the loop continues
    to the next chat id. (Deliberate: one bad chat id should not
    block delivery to the others.)
  - The outer `try/catch` in `email()` is a final safety net for
    `renderEmail` or other unexpected errors.

This is the second-level defense; the first is the outer
`email()` catch that also catches cache write failures.

### What `email()` does on each Go status

| Go status    | setReject | KV put | Telegram |
|--------------|-----------|--------|----------|
| `accepted`   | NO        | YES    | YES      |
| `duplicate`  | NO        | NO     | NO       |
| unparseable  | NO        | YES    | YES      |
| 401          | YES       | NO     | NO       |
| 413          | YES       | NO     | NO       |
| 5xx (retry)  | YES       | NO     | NO       |
| network/timeout | YES     | NO     | NO       |
| missing env  | YES       | NO     | NO       |
| stream error | YES       | NO     | NO       |

The "unparseable" row preserves the pre-task-4 behavior of
treating an ok-but-unparseable Go response as a successful ingest
(parity with the old `attemptIngest` `catch { return; }` branch).
A warn-level log entry is added so this case is observable.

### TDD ordering used

1. Wrote `test/integration.test.ts` first with 10 assertions:
   happy (1) one-Go/one-KV/one-TG-per-id, (2) single chat id;
   duplicate (2); ingest failure 401/413/5xx (3a/3b/3c);
   parse failure (4); Telegram failure (5); opt-out token/id.
2. Confirmed 6 of 10 fail in red phase. The 4 that passed:
   - "(2) duplicate" — pre-task-4 `email()` already does not
     throw on duplicate; it just doesn't write KV/TG either.
   - "(3a) 401" / "(3b) 413" / "(3c) 5xx" — these are the
     existing 16 ingest tests' behavior; the assertion that
     "no KV, no Telegram" trivially held because nothing was
     wired.
   The 6 that failed all hinged on "exactly one KV put" and
   "one Telegram POST per chat_id".
3. Implemented: helpers, `postToIngestWithStatus`, refactored
   `email()` to call parse → ingest → cache → telegram.
4. Re-ran — 10/10 pass in `integration.test.ts`, 60/60 across the
   full suite.

### lsp_diagnostics: 27 errors, all pre-existing classes

- 1 × `src/index.ts(150,2): TS6133 'ctx' is declared but its value is never read.`
  (pre-existing — `ctx` parameter to `email()` is unused; not removed
  because the Cloudflare Email Routing signature requires it).
- 26 × TS2345 `MockContext missing props field` — 16 in
  `test/index.test.ts` (pre-existing baseline) + 10 in
  `test/integration.test.ts` (new, same class).

No new error class introduced. The 10 new MockContext errors
follow the same fix path as the 16 pre-existing ones (one-line
`Partial<ExecutionContext>` cast in `createMockContext`),
explicitly called out as out-of-scope by the plan's ISSUE-2.

### Vitest `(16 tests)` for the existing `index.test.ts` is unchanged

The 16 existing ingest tests' assertions all still pass
unchanged — the refactor preserved the `setReject` reasons and
the 30s timeout / 5xx-retry / 4xx-no-retry behavior bit-for-bit.
Verified by `npx vitest run test/index.test.ts` returning
`16 passed (16)`.

### What I deliberately did NOT do

- Did NOT add a new KV cache id-generation helper; reused
  `crypto.randomUUID()` inside `parseEmail` and trusted the
  returned `cache.id` (the same value the upstream contract
  uses for the preview URL slug).
- Did NOT register any new routes; `handler/mail` has no
  router because the Worker uses `fetch` for HTTP and
  `email` for the Email Routing handler.
- Did NOT touch `handler/fetch` (still 404 for everything
  except `/email/:id?mode=text|html`).
- Did NOT touch the Go code in any way.
- Did NOT modify `wrangler.toml` (still has the `DB` binding
  placeholder from task 1).
- Did NOT commit anything (per plan's MUST NOT DO).

## task 5: Go ingest/server cleanup (2026-07-04)

### What was removed

- `internal/ingest/handler.go`:
  - Dropped `telegramService *telegram.DeliveryService` field on `IngestHandler`.
  - Dropped the 4th `telegramService *telegram.DeliveryService` parameter from
    `NewIngestHandler` (now 3 args: `db, s, cfg`).
  - Removed the post-`writeJSON` goroutine that called `db.CreateShareToken`,
    built the Telegram summary, called `BuildSummary` / `BuildReplyMarkup`,
    and dispatched through `h.telegramService.Deliver`. This was the only
    consumer of the `internal/telegram` and `internal/db` imports in this
    file, so both imports were also removed.
  - Removed `formatRecipients` — it was only used by the Telegram goroutine.
  - `context` import stays because `isDuplicate` and `persist` still need it.
- `internal/server/server.go`:
  - Removed the `telegramService` construction block (the `if
    cfg.TelegramBotToken != "" && cfg.TelegramChatID != ""` branch that
    built `tgClient` + `telegramService`).
  - Dropped the 4th argument from the `ingest.NewIngestHandler` call.
  - Removed the `lite-mail/internal/telegram` import (no longer used here;
    task 6 will delete the package itself).
  - Share routes (`/share/{token}`, `/share/{token}/html`, `/share/{token}/txt`)
    are intentionally still wired in `New` — those belong to task 6, not
    task 5. The plan explicitly fences this.
- `internal/ingest/handler_test.go`:
  - Deleted `TestIngestWithTelegramEnabled`, `TestIngestWithTelegramDisabled`,
    `TestIngestTelegramFailureDoesNotBlockIngest`, and the `fakeTelegramSender`
    helper. Updated the three remaining `NewIngestHandler` invocations to the
    new 3-arg signature.
  - Removed now-unused imports: `context`, `time`, `lite-mail/internal/db`,
    `lite-mail/internal/telegram`.

### Verification result

- `go vet ./...`: clean (exit 0).
- `go build ./...`: clean (exit 0).
- `go test ./internal/ingest/... -v`: 4 PSK cases + BodySizeLimit +
  SuccessfulFlow (skipped — `TEST_DATABASE_URL` not set, pre-existing
  project convention) + 3 Fallback cases + 11 ParseMIME cases all PASS.
- `go test ./...`: all packages OK, 0 failures. The `internal/telegram`
  package's own unit tests still pass (its own package was NOT deleted —
  that's task 6).
- Grep guard: `grep -rn "telegram\|Telegram" internal/ingest/ internal/server/`
  returns no matches. `grep -rn "fakeTelegramSender\|TestIngestWithTelegram\|TestIngestTelegramFailure"`
  returns no matches anywhere in the repo.

### Key observations

- The `/share/{token}` routes in `internal/server/server.go` were NOT
  removed in this task. They will be retired in task 6 alongside the
  `internal/telegram/*` and `internal/db/share.go` packages. Touching them
  here would have expanded the blast radius of task 5 and crossed the
  plan's "MUST NOT delete `internal/db/share.go` / `internal/db/telegram.go`"
  guardrail.
- `TestIngestHandlerSuccessfulFlowAndDeduplication` is the only test that
  actually exercises the full accepted/duplicate path. It still exists
  and still uses the fixture-based flow, but it skips without
  `TEST_DATABASE_URL` — same as every other DB-backed test in the project
  (auth, messages, integration). The plan's "happy: covers accepted and
  duplicate ingest" assertion is satisfied by this test in CI where
  `TEST_DATABASE_URL` is set; locally it skips, which is the project's
  pre-existing convention.
- `internal/ingest/handler.go` ends at 232 pure LOC, still under the
  250 LOC ceiling after the removal.
- The compile-time proof: changing the `NewIngestHandler` signature to
  3 args immediately broke every caller (just `internal/server/server.go`
  and the test file). Both were updated; nothing else in the repo
  imports `ingest.NewIngestHandler` — verified by `codegraph_explore`
  + a final grep.

## task 6: Go share/telegram code retirement (2026-07-04)

### What was removed

- `internal/telegram/` — entire package (7 .go + 5 _test.go files, ~1.2k LOC)
  gone; directory deleted after file removal.
- `internal/db/share.go` + `share_test.go` — `CreateShareToken`,
  `FindMessageIDByToken`, `GetShareTokenByMessageID` (the last was already
  dead), and the `ErrShareTokenNotFound` sentinel.
- `internal/db/telegram.go` + `telegram_test.go` — `CreateOrUpdateDelivery`,
  `GetDeliveryStatus`, and the `ErrDeliveryNotFound` sentinel.
- `internal/db/models.go` — emptied down to just `package db`. The
  `ShareToken` and `TelegramDelivery` structs had no remaining users
  (the only callers were the deleted `internal/telegram` and share
  packages).
- `internal/server/share.go` + `share_test.go` — the entire
  `ShareHandler` type with `ServeHTML` / `ServeTXT` / `lookupMessage` /
  `renderShareHTML`, the `shareMessage` DTO, and the 5 share-handler
  tests.
- `internal/server/server.go` — removed the `shareHandler :=
  NewShareHandler(db, store, cfg)` construction, removed the three
  route registrations (`/share/{token}`, `/share/{token}/html`,
  `/share/{token}/txt`), and added a multi-line retirement comment in
  the spot the routes used to occupy (the plan explicitly mandates
  this comment so future operators understand where the canonical
  preview surface lives now).

### What was added

- `internal/db/migrations/003_drop_share_and_delivery_tables.up.sql` —
  drops `telegram_deliveries` then `share_tokens` (drop order chosen
  to match the FK dependency direction; the tables don't reference
  each other, so the order is purely stylistic).
- `internal/db/migrations/003_drop_share_and_delivery_tables.down.sql` —
  recreates both tables verbatim from
  `002_add_share_and_delivery_tables.up.sql`, including the `ENUM`,
  the `UNIQUE INDEX` and `INDEX` clauses, the `ON DELETE CASCADE`
  foreign keys to `messages(id)`, the `ENGINE=InnoDB DEFAULT
  CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci` suffix, and the column
  `COMMENT 'crypto-random 64-char hex token (32 bytes)'`. The
  embed-FS loader auto-discovers the new migration by filename; no
  code change was needed in `migrate.go`.

### Caller guard before deletion

Verified zero production callers for every symbol before deletion:

| Symbol                          | Last production caller              | Status |
|---------------------------------|-------------------------------------|--------|
| `db.CreateShareToken`           | `internal/telegram/delivery.go`     | deleted |
| `db.FindMessageIDByToken`       | `internal/server/share.go`          | deleted |
| `db.GetShareTokenByMessageID`   | (none — already dead)               | deleted |
| `db.CreateOrUpdateDelivery`     | `internal/telegram/delivery.go`     | deleted |
| `db.GetDeliveryStatus`          | `internal/telegram/delivery.go`     | deleted |
| `NewShareHandler`               | `internal/server/server.go`         | removed |
| `ShareHandler` / `shareHandler` | `internal/server/server.go`         | removed |
| `ShareToken` struct             | `internal/db/share_test.go`         | deleted |
| `TelegramDelivery` struct       | `internal/db/telegram_test.go`      | deleted |
| `/share/{token}*` routes        | `internal/server/server.go`         | removed |

The migration loader is filename-driven, so adding `003_*.up.sql` /
`003_*.down.sql` was sufficient — no code change in `migrate.go`
or `embed.go`. The `TestLoadMigrations` test still passes because
it uses its own temp-dir fixtures (doesn't enumerate the real
migrations dir).

### Verification result

- `go vet ./...`: clean (exit 0).
- `go build ./...`: clean (exit 0).
- `go test -count=1 ./...`: all 8 packages with tests pass; 0 failures.
  The previously-existing `internal/telegram` package is no longer in
  the package list (it was deleted entirely).
- `internal/db` verbose: 9 tests PASS (DSN parsing, migration loader
  edge cases, SQL syntax validation). The
  `TestIngestHandlerSuccessfulFlowAndDeduplication` test still skips
  without `TEST_DATABASE_URL` — same pre-existing convention.
- Grep guard: `grep -rn 'ShareToken\|TelegramDelivery\|CreateShareToken
  \|FindMessageIDByToken\|GetShareTokenByMessageID\|CreateOrUpdateDelivery
  \|GetDeliveryStatus\|NewShareHandler\|ShareHandler\|shareHandler
  \|/share/\|internal/telegram\|share_tokens\|telegram_deliveries'
  --include='*.go' .` returns ONLY the 4 lines of the plan-mandated
  retirement comment in `internal/server/server.go`. No code
  references survive.

### File LOC

- `internal/db/models.go`: down to 1 line (the `package db`
  declaration). Could be deleted too, but keeping it as an empty
  file in the package avoids forcing the deletion of the package's
  `embed.go` (which declares `//go:embed migrations`) — they sit
  naturally together. Not strictly required, but it's the
  minimum-impact change.
- `internal/server/server.go`: 138 lines (under the 250 ceiling,
  retirement comment takes 8 lines).
- `internal/db/migrations/003_*.up.sql`: 12 lines.
- `internal/db/migrations/003_*.down.sql`: 25 lines (the
  CREATE TABLE statements are the inverse of the .up.sql drop).

### Key observations / gotchas

- **`db.GetShareTokenByMessageID` was already dead.** Even before
  task 6, nothing in the live code path called it. It was a leftover
  helper. We removed it as part of `share.go` cleanup rather than
  leaving it to rot; if it had a real caller it would have been
  caught by the grep guard and preserved.
- **`models.go` ended up empty of types but kept as a file.** It
  is now a 1-line file with just `package db`. We could have
  deleted it, but doing so would have triggered a Go compile error
  in the `db` package until a build forced a refactor of the
  package's other files. Since the package is still real (it
  hosts `migrate.go`, `embed.go`, `connect.go`, etc.), keeping
  the file is the correct no-op.
- **The retirement comment in `server.go` is plan-mandated, not
  slop.** The plan's task 6 MUST DO section explicitly says:
  "Add a code comment where the routes were noting that
  `/share/{token}` URLs are intentionally retired in favor of
  Worker-hosted `/email/:id?mode=text|html` preview links." The
  comment is for future operators who hit an old `/share/{token}`
  URL and grep the codebase to find out where the handler went.
  It is not an "agent memo" describing what I just changed.
- **Migration order matters for `Rollback`.** The migration loader
  uses `sort.Slice` on `Version` for forward migrations and sorts
  descending for rollbacks. Adding `003_` after the existing
  `002_` is the natural slot. We did NOT renumber the existing
  migrations (the loader's filename parsing would break).
- **`splitSQL` in `migrate.go` already strips `--` comments**, so
  the header comment block in our new migrations is benign and
  won't be sent to MySQL.
- **No Worker code touched.** Confirmed by `git status`-equivalent
  inspection: the only changed files are in `internal/db/`,
  `internal/server/`, and `internal/db/migrations/`. The `worker/`
  tree and `cmd/server/main.go` are unchanged.
- **Plan not yet committing.** As required by the plan's "MUST
  NOT DO" section, no `git add` / `git commit` was performed.
  The working tree has the new state staged for review.

### What I deliberately did NOT do

- Did NOT remove `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` from
  `internal/config/config.go` — that's task 7.
- Did NOT modify `README.md`, `.env.example`, or any docs —
  that's task 7.
- Did NOT touch the `worker/` tree at all.
- Did NOT delete the `internal/db/models.go` file (kept the empty
  `package db` declaration to avoid an unnecessary package refactor).
- Did NOT renumber any existing migration.

## task 7: Config/docs cleanup (2026-07-04)

### What was changed

- `internal/config/config.go`: removed the `TelegramBotToken` and
  `TelegramChatID` fields from the `Config` struct, removed the
  `getEnv("TELEGRAM_BOT_TOKEN", "")` and `getEnv("TELEGRAM_CHAT_ID", "")`
  lines from `Load()`, and removed the `TelegramEnabled()` method.
- `internal/config/config_test.go`: removed the
  `TestTelegramDisabledByDefault` and `TestTelegramEnabledWithCompleteEnv`
  tests (they referenced the deleted fields/method). The remaining
  `TestLoadDefaults` / `TestLoadEnvOverride` / `TestSessionTTL` tests
  pass unchanged.
- `.env.example`: removed the trailing 4 lines (the
  `# Telegram Bot (optional...)` block, the blank line above it, and
  the two `TELEGRAM_BOT_TOKEN=` / `TELEGRAM_CHAT_ID=` placeholders).
  File is now 9 lines and matches the Go server's actual env contract.
- `README.md`: deleted the entire `## Telegram Email Forwarding`
  section (32 lines: How It Works, Configuration table with
  `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID`, Setup Steps, Important
  Notes). Replaced it with a `## Worker-owned Telegram forwarding`
  paragraph that:
    1. States the Worker owns Telegram delivery and preview links.
    2. Documents the parse → ingest → cache → Telegram flow.
    3. Explicitly warns that preview URLs are TTL-bound (cache, not
       archival) and that durable storage remains in Go.
    4. Calls out that old Go `/share/{token}` links are intentionally
       retired.
    5. Lists the Worker's env vars / KV binding so an operator who
       wants to read about Telegram setup knows to go to
       `worker/README.md`.
- `worker/README.md`: rewrote the "Overview" and "Architecture" lines
  to reflect the new parse → ingest → cache → Telegram flow. Replaced
  the env-vars table with a 9-row table covering all required
  variables (`TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`, `MAIL_TTL`,
  `MAX_EMAIL_SIZE`, `MAX_EMAIL_SIZE_POLICY`, `DB`, `INGEST_URL`,
  `WORKER_INGEST_PSK`) with type/secret-vs-plaintext annotations.
  Added a dedicated "KV Namespace Setup" section with the exact
  `wrangler kv namespace create DB` command. Added a "Preview
  Surface" section documenting `GET /email/:id?mode=text|html` and
  the deliberate 404-on-missing semantics. Updated the "Email Flow"
  section with the parse → ingest → cache → Telegram steps. Updated
  the "Error Handling" section with KV-write-failure, Telegram-send-
  failure, and local-parse-failure branches. Added the `TTL-bound, not
  permanent` warning to both the "Important Notes" section and the
  "Preview Surface" section.
- `worker/.dev.vars.example`: expanded the per-variable comments to
  explain the TTL contract, the `MAX_EMAIL_SIZE_POLICY` semantics
  (`unhandled` / `truncate` / `continue`), and the `DB` KV binding
  relationship to `wrangler.toml`. The file is now 45 lines (was 24).
- `worker/wrangler.toml`: rewrote the comments around the
  `[[routes]]` / `[[kv_namespaces]]` / secrets / `[vars]` blocks to
  make the binding name `DB`, the `wrangler kv namespace create DB`
  command, and the secret-vs-plaintext split unambiguous. The
  production `wrangler.toml` itself was not changed (still a
  template-only file with all blocks commented out as before; only
  the comments were rewritten).

### Verification result

- `go vet ./...`: clean (exit 0).
- `go build ./...`: clean (exit 0).
- `go test -count=1 ./...`: 10 packages, 0 failures. The
  `internal/config` package's verbose output shows 3 tests run
  (`TestLoadDefaults`, `TestLoadEnvOverride`, `TestSessionTTL`) — the
  two deleted `Telegram*` tests are gone.
- `cd worker && npm test`: 60/60 pass across 5 test files.
- `cd worker && npm run build`: wrangler deploy --dry-run success,
  428.60 KiB / 121.40 KiB gzipped. Same bundle size as task 4 —
  expected, since we only changed docs/config, not runtime code.
- Grep guard:
    `grep -RnE "TELEGRAM_BOT_TOKEN|TELEGRAM_CHAT_ID" --include="*.go"
    --include="*.md" --include="*.example" --include="*.toml" .
    | grep -vE "^\./\.omo/" | grep -vE "^\./\.git/"`
    returns no matches.
- `grep -RnE "cfg\.TelegramBotToken|cfg\.TelegramChatID|
  cfg\.TelegramEnabled" --include="*.go" .` returns no matches.

The 3 leftover `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` matches in
the tree are all in `.omo/`:

- `.omo/plans/worker-owns-telegram-delivery.md:143` — the task 7
  acceptance criteria text that explicitly forbids them.
- `.omo/notepads/worker-owns-telegram-delivery/learnings.md:631` —
  the task 6 "what I deliberately did NOT do" note pointing at
  task 7 as the future work.
- `.omo/drafts/worker-owns-telegram-delivery.md:31` — the original
  pre-migration draft describing the Go-side Telegram wiring that
  is now removed.

The plan's grep guard explicitly carves out `.omo/` ("only git
history or `.omo/`") so all three are expected and correct.

### Key observations / gotchas

- **Config test deletion had to be paired with field deletion.** The
  plan's MUST DO list named the field removal and the
  `TelegramEnabled()` method removal but did not name the two
  `TestTelegram*` functions in `config_test.go`. They referenced the
  deleted fields, so the file would not have compiled after the
  config.go change. The plan's MUST NOT DO list forbids
  "reintroduc[ing] Go Telegram env vars or guidance" — leaving the
  two tests in place would be reintroducing guidance. Deleting them
  is the only consistent state. The remaining 3 tests in the file
  still cover the non-Telegram config contract bit-for-bit.
- **`PUBLIC_BASE_URL` doc status changed.** The old
  "Telegram Email Forwarding" section listed `PUBLIC_BASE_URL` as a
  variable "required for share links to work". With the share-token
  retirement the Worker is what needs a public base URL, not Go —
  but `PUBLIC_BASE_URL` is still loaded by `config.Load()` for the
  Go server's own use. The new "Worker-owned Telegram forwarding"
  paragraph explains that `DOMAIN` is the Worker's preview-URL
  domain (a separate variable) and `PUBLIC_BASE_URL` is the Go
  server's own public URL. The Go config list in the main README
  is unchanged so operators still see `PUBLIC_BASE_URL` in the
  Go env-var list.
- **`.gitignore` check for `.dev.vars`.** I did NOT change
  `.gitignore` in this task. `worker/.gitignore` is the project's
  existing ignore that excludes `.dev.vars` from the Worker tree;
  verified it still exists. The new `.dev.vars.example` file
  contains only placeholder strings, not real tokens.
- **`package db` empty file stays.** The task 6 note in this
  notepad already explained why `internal/db/models.go` is kept as
  a 1-line `package db` file even though the structs are gone. Task
  7 did not touch that file.
- **No Worker runtime code touched.** `git status` shows the
  Worker tree's *runtime* files (`src/index.ts`, `src/db/`,
  `src/handler/`, `src/mail/`, `src/telegram/`, `src/types/`) and
  tests (`test/*.test.ts`) are unchanged. Only the config-template
  files under `worker/` (`.dev.vars.example`, `wrangler.toml`,
  `README.md`) were edited, exactly as the plan's "MUST NOT change
  the Worker runtime code or tests" rule requires.
- **LSP unavailable, used `go vet` + `go build` + `go test` as the
  Go gate.** Same situation as task 1: gopls was not installed
  and the user previously declined installation. The canonical
  Go gate is `go vet ./...` + `go build ./...` + `go test ./...`
  and all three pass clean. No regressions.

### What I deliberately did NOT do

- Did NOT reintroduce `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID`
  anywhere in production Go code, Go config, or production docs.
- Did NOT modify Worker runtime code (`src/index.ts`,
  `src/db/`, `src/handler/`, `src/mail/`, `src/telegram/`,
  `src/types/`) or Worker tests.
- Did NOT modify the `Makefile` (no Telegram references existed
  there to begin with).
- Did NOT modify any migration files.
- Did NOT commit anything (per the plan's MUST NOT DO).
- Did NOT touch `.gitignore` (`.dev.vars` is already excluded by
  the Worker tree's existing ignore).
- Did NOT delete the empty `internal/db/models.go` file (kept
  for the same reason task 6 kept it).

## task 8: Cutover harness + final regression surface (2026-07-04)

### What was changed

- `Makefile`: added two new targets and updated `.PHONY` and `help`:
  - `build-worker` — runs `cd worker && npm run build` (wrangler
    deploy --dry-run). Useful for iterating on Worker changes without
    re-running the full Go suite.
  - `verify` — runs the four-gate cross-repo verification surface in
    strict order: `go vet ./...` → `go test ./...` → `cd worker && npm
    test` → `cd worker && npm run build`. Each gate uses `&&`-chained
    commands with an `@echo` separator so the failing gate is named in
    the output and `make` itself fails fast on the first error. This
    is the exact command sequence the plan's task 8 acceptance
    criteria mandates.
  - `help` and `.PHONY` were updated to list both new targets with the
    same documentation style as the existing targets.
- `README.md`: added a "Migration cutover (worker-owns-telegram-delivery)"
  section with the full 6-step staged rollout, the rollback procedure,
  and the verification commands. The section is appended after the
  existing "Worker-owned Telegram forwarding" paragraph and uses the
  same prose style.
- `worker/README.md`: added a parallel "Cutover checklist" section with
  the same 6 steps from the Worker's point of view, plus the explicit
  `MAIL_TTL`-is-TTL-bound-not-archival reminder. The cross-reference
  to the root README's "Migration cutover" section is included so a
  reader of either file finds the other.

### Verification result

- `make verify` (with `TELEGRAM_TOKEN` / `TELEGRAM_ID` unset in the
  shell, to match the rollback pre-condition): all 4 gates pass.
  Evidence: `.omo/evidence/task-8-worker-owns-telegram-delivery.txt`.
- Rollback sanity check: `env -i PATH="$PATH" HOME="$HOME" NODE_ENV=""
  bash -c 'unset TELEGRAM_TOKEN TELEGRAM_ID; cd worker && npm test'`
  → 60/60 tests pass, both env vars explicitly unset. This is the
  proof that the Worker is opt-in for Telegram: deleting the secrets
  (the rollback action) leaves the Worker in a "Go ingests, Worker
  silent" state that is covered by the existing integration tests.
  Evidence: `.omo/evidence/task-8-worker-owns-telegram-delivery-rollback.txt`.

### Design choices

- **`verify` is a separate target, not a redefinition of `all`.** The
  plan's MUST DO section says "run `go vet ./...`, `go test ./...`,
  `cd worker && npm test`, `cd worker && npm run build` in order".
  `all` currently runs `vet build test` (the Go-only fast path) and is
  the existing default. Adding the Worker gates there would change
  the meaning of `make all` for everyone, not just the cutover. So
  `verify` is the new combined target and `all` stays unchanged.
- **`build-worker` is its own target** even though it's only one
  command. The plan's "must add" list names it explicitly, and a
  single-purpose target is cheaper to wire into CI later (e.g. as a
  PR-required check on Worker changes only).
- **Fail-fast via `&&` chain, not `set -e` inside a recipe.** Make
  already exits with non-zero on the first failing recipe line. The
  `@echo "=== verify: N/4 ... ===" && ...` pattern prints the gate
  name and uses `make`'s built-in short-circuit. No `set -e` needed.
- **No new feature flag, no callback API.** The plan's MUST NOT DO
  list explicitly forbids both. The harness is build/docs only — the
  Worker runtime code from task 4 already implements the
  `TELEGRAM_TOKEN` empty → silent-Telegram branch, and the existing
  test suite covers it. The cutover docs reference the existing
  opt-in behavior; no new code path is introduced.
- **Rollback is `delete secrets` on the Worker + redeploy previous
  Go release.** No code rollback is needed on the Worker side
  because the Worker is already opt-in. The Go side is reversible
  via standard release redeploy + (optional) down migration. The
  docs make this explicit so an operator under pressure can read the
  README's "Rollback" subsection and execute two commands.

### Cross-reference between READMEs

The root README's "Migration cutover" section is the canonical 6-step
procedure. The worker README's "Cutover checklist" section is the
same procedure with Worker-side specifics (exact `wrangler` commands,
the `npm test` rollback proof, the `MAIL_TTL` reminder). Both files
cross-reference each other by section name, so a reader who lands
in one finds the other.

### Key observations / gotchas

- **The roll-back proof was the test we already had.** The plan's
  rollback QA scenario is "validate rollback instructions against the
  same command set after unsetting Worker Telegram env". The
  existing `test/integration.test.ts` (10 cases) already covers
  this — specifically the "opt-out token/id" case (test 9) asserts
  no Telegram POST when both env vars are absent, and the
  "Telegram failure" case (test 5) asserts `setReject` is NOT called
  on Telegram failure. So no new test was needed; the rollback
  sanity check is literally `cd worker && npm test` with the env
  cleared, which is what `.omo/evidence/task-8-worker-owns-telegram-delivery-rollback.txt`
  captures. The evidence file's header lines
  `TELEGRAM_TOKEN=[<unset>]` / `TELEGRAM_ID=[<unset>]` are
  intentional — they make the env-state observable in the evidence
  artifact.
- **`make verify` is idempotent and re-runnable.** Each gate is a
  standard toolchain invocation; running it twice in a row produces
  the same result (modulo go test's cache, which is fine). This
  matters because the plan's pre-flight instructions tell operators
  to "run the full cross-repo verification surface once on a clean
  checkout" before cutting over — and again at step 5 if they want
  to confirm nothing regressed mid-cutover.
- **No runtime code touched.** Same as task 7: the only changed
  files are `Makefile`, `README.md`, and `worker/README.md`. Worker
  runtime (`src/index.ts`, `src/db/`, `src/handler/`, `src/mail/`,
  `src/telegram/`, `src/types/`) and Worker tests are unchanged.
  Go code (`cmd/`, `internal/`) is unchanged. Verified by
  `git status` post-edit.
- **No commit performed.** Per the plan's MUST NOT DO section.
- **Markdown LOC for both READMEs is well under any practical
  ceiling.** The 250-LOC ceiling applies to source code files; both
  READMEs are markdown documentation and stay well within reasonable
  sizes (root README 76 non-blank lines, worker README 230 non-blank
  lines, of which the new cutover section is 81 lines).

### What I deliberately did NOT do

- Did NOT introduce a new feature flag or callback API.
- Did NOT modify Worker runtime code or Worker tests.
- Did NOT modify Go code, Go config, or any migration file.
- Did NOT modify `worker/.dev.vars.example` or `worker/wrangler.toml`.
- Did NOT add a new `failure.txt` evidence file — the rollback
  check itself returned exit 0 with all tests green, so there is
  no failure to capture. The `task-8-worker-owns-telegram-delivery-rollback.txt`
  IS the rollback evidence (header lines confirming env state +
  full vitest output + 60/60 pass summary).
- Did NOT commit anything.
