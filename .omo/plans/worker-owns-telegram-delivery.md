# worker-owns-telegram-delivery - Work Plan

## TL;DR (For humans)
**What you'll get:** Telegram delivery moves entirely into the Cloudflare Worker, using the upstream mail2telegram model for parsing, sending, and `/email/:id?mode=text|html` preview links. The Go server is reduced to web-mail ingest, storage, and UI/API responsibilities only.

**Why this approach:** You explicitly asked for the Worker to own Telegram and to use the upstream Worker share-link model directly. That lets the migration delete Go’s Telegram/share plumbing instead of keeping a half-migrated hybrid contract.

**What it will NOT do:** It will not keep Go as a Telegram sender or audit service.
It will not preserve the old permanent `/share/{token}` links.
It will not import upstream callback/TMA/webhook product features beyond the preview-link pattern and Worker-native sending flow.

**Effort:** Large
**Risk:** High - this changes runtime ownership, public preview URLs, data retention shape for Telegram links, and deletes existing Go-side Telegram/share code.
**Decisions to sanity-check:** Preview links become Worker-hosted and TTL-bound (upstream-style cache), not permanent Go share tokens; Go Telegram/share tables and routes are retired rather than preserved.

Your next move: start work now, or ask me to run a high-accuracy review first. Full execution detail follows below.

---

> TL;DR (machine): Large / High - move Telegram sending + preview links into Worker, keep raw MIME ingest into Go, remove Go Telegram/share/audit code, add Worker KV-backed preview cache and fetch surface.

## Scope
### Must have
- Worker keeps posting raw MIME to Go `POST /api/ingest` with the existing PSK contract; Go remains the durable ingest/storage system of record.
- Worker is restructured around upstream mail2telegram-style modules and explicitly pins upstream reference commit `6d6ffbc055280c809939c3faf99074384d540fe5` for the copied/adapted logic.
- Worker gains a `fetch` handler and a KV-backed preview cache with upstream-style routes:
  - `GET /email/:id?mode=text`
  - `GET /email/:id?mode=html`
- Worker owns Telegram credentials and Worker-side preview configuration using upstream-style env names and bindings:
  - `TELEGRAM_TOKEN`
  - `TELEGRAM_ID`
  - `DOMAIN`
  - `MAIL_TTL`
  - `MAX_EMAIL_SIZE`
  - `MAX_EMAIL_SIZE_POLICY`
  - KV binding `DB`
  - existing `INGEST_URL` and `WORKER_INGEST_PSK`
- Worker parses the email locally for Telegram/preview purposes using upstream-style `postal-mime` + `html-to-text`, while still forwarding raw MIME to Go for durable storage.
- Telegram failure remains non-blocking: if ingest succeeds but Telegram send or preview-cache write fails, the email must still be accepted and Go ingest must remain successful.
- Duplicate ingest responses from Go (`status: duplicate`) must not trigger Telegram sending and must not create a fresh preview cache entry.
- Preview links in Telegram must keep the current lite-mail button labels (`View as TXT`, `View as HTML`) but point to Worker `/email/:id?...` URLs.
- Go is reduced to web-mail only: remove Telegram sender wiring, Telegram/share-token DB helpers, public `/share/{token}` routes, Telegram/share migrations, and related tests/docs/config.
- Worker preview links are explicitly TTL-bound cache links, not permanent archival links; this behavior change must be documented.
### Must NOT have (guardrails, anti-slop, scope boundaries)
- No Go fallback sender, no Go-side Telegram audit callback API, and no partial “hybrid” Telegram ownership after cutover.
- No parsed-JSON ingest rewrite for Go in this migration; keep the raw MIME ingest contract unchanged.
- No upstream callback query, webhook, TMA, blocklist, summary-AI, or command features.
- No preservation promise for existing `/share/{token}` links; retiring them is part of scope.
- No ambiguous cache binding name; use upstream-style `DB` for Worker KV so the copied/adapted code has no naming fork.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: TDD + Vitest (Worker) + Go test suite + go vet
- Evidence: .omo/evidence/task-<N>-worker-owns-telegram-delivery.<ext>
- Worker golden-parity rules to preserve from current lite-mail UX / upstream flow:
  - summary contains From/To/Subject/Date labels and HTML escaping
  - TXT button label stays `View as TXT`
  - HTML button label stays `View as HTML`
  - preview URLs use Worker route `/email/:id?mode=text|html`
  - duplicate ingest means no Telegram send and no cache write
  - Telegram send failure does not call `message.setReject(...)`
- Go cleanup parity rules:
  - `go test ./...` passes with Telegram/share code removed
  - server no longer registers `/share/{token}` routes
  - config no longer loads Go `TELEGRAM_*` env vars
  - migration set reflects removal of `share_tokens` and `telegram_deliveries`

## Execution strategy
### Parallel execution waves
> Target 5-8 todos per wave. Fewer than 3 (except the final) means you under-split.

Wave 1 establishes the Worker runtime, preview cache, and Worker-owned Telegram flow while keeping the raw MIME ingest contract unchanged.

Wave 2 removes Go’s Telegram/share surface, rewrites cleanup-sensitive tests, and documents the cutover/deprecation behavior.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1 | none | 2,3,4,7,8 | none |
| 2 | 1 | 4,8 | 3 |
| 3 | 1 | 4,8 | 2 |
| 4 | 1,2,3 | 5,8 | none |
| 5 | 4 | 6,7,8 | none |
| 6 | 5 | 7,8 | none |
| 7 | 1,5,6 | 8 | none |
| 8 | 1,2,3,4,5,6,7 | F1-F4 | none |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [x] 1. Worker runtime contract: add upstream-style module surface, env schema, KV binding, and failing tests first
  What to do / Must NOT do: Restructure `worker/` around the minimum upstream-compatible surface needed for this migration: keep `worker/src/index.ts` as the entrypoint, but split or introduce exact modules for `handler/mail`, `handler/fetch`, `mail/parse`, `telegram/api`, and `db/index` so copied/adapted upstream logic has stable homes. Add runtime deps `postal-mime`, `html-to-text`, `itty-router`, and type/dev deps as needed for Telegram payload typing. Extend Worker env/types to include `TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`, `MAIL_TTL`, `MAX_EMAIL_SIZE`, `MAX_EMAIL_SIZE_POLICY`, and KV binding `DB`, while keeping `INGEST_URL` and `WORKER_INGEST_PSK`. Update `wrangler.toml` comments/templates to require an HTTP route plus `[[kv_namespaces]]` binding named `DB`. Must NOT change the Go ingest payload shape or add any Go callback contract.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 2,3,4,7,8
  References (executor has NO interview context - be exhaustive): worker/src/index.ts:1-254; worker/package.json:1-19; worker/wrangler.toml:1-30; worker/.dev.vars.example:1-8; worker/test/index.test.ts:1-508; upstream commit `6d6ffbc055280c809939c3faf99074384d540fe5`; upstream `src/index.ts`; upstream `src/types/index.ts`; upstream `src/db/index.ts`; upstream `src/handler/fetch/index.ts`.
  Acceptance criteria (agent-executable): `cd worker && npm test` fails first on new env/fetch/KV contract expectations, then passes after implementation; `cd worker && npm run build` succeeds with the new dependencies and module surface; the Worker entrypoint exports both `email` and `fetch`.
  QA scenarios (name the exact tool + invocation): happy - `cd worker && npm test` includes new assertions that `Env`/bindings and `fetch` export are present, Evidence .omo/evidence/task-1-worker-owns-telegram-delivery.txt; failure - `cd worker && npm run build` must fail before deps/bindings are wired and pass after, Evidence .omo/evidence/task-1-worker-owns-telegram-delivery-build.txt.
  Commit: Y | feat(worker): scaffold upstream-style telegram runtime and kv contract

- [x] 2. Worker preview cache + `/email/:id` fetch surface: adopt upstream share-link model directly
  What to do / Must NOT do: Implement the upstream-style preview-cache DAO using Worker KV binding `DB`, storing parsed `{id,messageId,from,to,subject,text,html}` entries with TTL from `MAIL_TTL` (default 86400 if unset). Add a Worker `fetch` route that serves `GET /email/:id?mode=text|html` exactly like upstream in behavior, but only for preview retrieval; no TMA/webhook/admin routes. For `mode=text`, return `text/plain; charset=utf-8`; for `mode=html`, return `text/html; charset=utf-8`; return 404 for missing cache entries. Must NOT proxy these routes to Go or keep Go share-token URLs in the Worker markup.
  Parallelization: Wave 1 | Blocked by: 1 | Blocks: 4,8 | Can parallelize with: 3
  References (executor has NO interview context - be exhaustive): upstream `src/db/index.ts:8-65`; upstream `src/handler/fetch/index.ts:109-124`; upstream `src/types/index.ts:3-35`; worker/src/index.ts:253-254; internal/server/share.go:28-115 (behavior being retired, not reused).
  Acceptance criteria (agent-executable): `cd worker && npm test` passes route tests proving (a) cached text preview returns 200/plain text, (b) cached HTML preview returns 200/html, (c) missing cache returns 404, and (d) route path is `/email/:id?mode=...`; `cd worker && npm run build` succeeds.
  QA scenarios (name the exact tool + invocation): happy - `cd worker && npm test` covers both `mode=text` and `mode=html` with seeded KV cache, Evidence .omo/evidence/task-2-worker-owns-telegram-delivery.txt; failure - `cd worker && npm test` covers missing-id and unsupported-mode handling, Evidence .omo/evidence/task-2-worker-owns-telegram-delivery-404.txt.
  Commit: Y | feat(worker): add kv-backed preview cache and email fetch routes

- [x] 3. Worker Telegram parser/client/renderer: copy upstream flow, preserve lite-mail message shape
  What to do / Must NOT do: Add Worker-local Telegram send support by adapting upstream `src/telegram/api.ts` and `src/mail/parse.ts` from pinned commit `6d6ffbc055280c809939c3faf99074384d540fe5`. Keep upstream-style `PostalMime` + `html-to-text` parsing, including `MAX_EMAIL_SIZE` and `MAX_EMAIL_SIZE_POLICY`, but render Telegram text/markup to match current lite-mail semantics: HTML-escaped summary with From/To/Subject/Date lines, 300-char body preview, 3500-char cap, and exactly two URL buttons labeled `View as TXT` and `View as HTML`. Button URLs must point to `https://${DOMAIN}/email/${cache.id}?mode=text` and `...?mode=html`. Must NOT import upstream callback buttons, AI summary buttons, webhooks, or multi-feature router logic.
  Parallelization: Wave 1 | Blocked by: 1 | Blocks: 4,8 | Can parallelize with: 2
  References (executor has NO interview context - be exhaustive): upstream `src/telegram/api.ts`; upstream `src/mail/parse.ts:30-71`; upstream `src/mail/render.ts:13-51`; upstream `src/handler/mail/index.ts:7-23`; internal/telegram/payload.go:21-79; internal/telegram/payload_test.go:10-177; worker/src/index.ts:18-39.
  Acceptance criteria (agent-executable): `cd worker && npm test` passes parser/render tests proving HTML escaping, truncation, button labels, button URL targets, and non-import of callback_data; `cd worker && npm run build` succeeds with the new Telegram client modules.
  QA scenarios (name the exact tool + invocation): happy - `cd worker && npm test` covers `View as TXT` / `View as HTML` URL generation and escaped summary output, Evidence .omo/evidence/task-3-worker-owns-telegram-delivery.txt; failure - `cd worker && npm test` covers parse fallback / truncated-body / malformed-email behavior and confirms no callback-data fields appear in serialized markup, Evidence .omo/evidence/task-3-worker-owns-telegram-delivery-failure.txt.
  Commit: Y | feat(worker): add telegram send and render flow from pinned upstream

- [x] 4. Worker email-flow integration: keep raw MIME ingest, parse locally, cache/send only on accepted ingest
  What to do / Must NOT do: Rework the Worker `email()` flow so it still collects raw MIME and posts the raw message body to Go with the existing `X-Lite-Mail-Ingest-PSK` contract, but also parses locally for Telegram/preview use. The Worker must: (a) parse locally, (b) POST raw MIME to Go, (c) only after Go returns `status: accepted` save preview cache and send Telegram, (d) if Go returns `status: duplicate`, skip cache write and skip Telegram send, and (e) if Telegram/cache work fails after accepted ingest, log and swallow the failure without calling `message.setReject(...)`. Duplicate parsing is an accepted tradeoff in this migration; do not invent a new parsed-JSON ingest API.
  Parallelization: Wave 1 | Blocked by: 1,2,3 | Blocks: 5,8
  References (executor has NO interview context - be exhaustive): worker/src/index.ts:62-251; internal/ingest/handler.go:90-137; upstream `src/handler/mail/index.ts:57-113`; worker/test/index.test.ts:89-508.
  Acceptance criteria (agent-executable): `cd worker && npm test` passes integration-style unit tests proving raw MIME is still POSTed to Go, Telegram send only happens on `accepted`, no send/cache on `duplicate`, and Telegram failure does not reject the email; `cd worker && npm run build` succeeds.
  QA scenarios (name the exact tool + invocation): happy - `cd worker && npm test` covers accepted-ingest path with one Go POST, one KV write, and one Telegram request, Evidence .omo/evidence/task-4-worker-owns-telegram-delivery.txt; failure - `cd worker && npm test` covers duplicate response, ingest 401/413/5xx, parse failure, and Telegram send failure with no `setReject` after accepted ingest, Evidence .omo/evidence/task-4-worker-owns-telegram-delivery-failure.txt.
  Commit: Y | feat(worker): integrate preview caching and telegram send into email flow

- [x] 5. Go ingest/server cleanup: strip Telegram sender wiring while keeping raw MIME web-mail ingest stable
  What to do / Must NOT do: Remove all Go runtime wiring that treats Telegram as part of ingest: delete `telegramService` construction from `internal/server/server.go`, remove the post-`writeJSON` goroutine from `internal/ingest/handler.go`, and simplify tests so Go ingest only validates PSK/auth, duplicate detection, MIME persistence, and web-mail storage. Keep the ingest response contract minimal: `{"status":"accepted","message_id":<id>}` on accepted and `{"status":"duplicate"}` on duplicate. Must NOT add any new Go callback or preview API.
  Parallelization: Wave 2 | Blocked by: 4 | Blocks: 6,7,8
  References (executor has NO interview context - be exhaustive): internal/server/server.go:68-79; internal/ingest/handler.go:125-171; internal/ingest/handler_test.go:138-324; internal/config/config.go:66-69.
  Acceptance criteria (agent-executable): `go test ./...` passes with Telegram-enabled/disabled ingest tests rewritten to ingest-only assertions; `go vet ./...` passes; grep/reference checks show `NewDeliveryService` and `telegramService` are no longer wired by server startup.
  QA scenarios (name the exact tool + invocation): happy - `go test ./...` covers accepted and duplicate ingest with no Telegram dependency, Evidence .omo/evidence/task-5-worker-owns-telegram-delivery.txt; failure - `go test ./...` covers unauthorized/body-size/duplicate branches and confirms no goroutine-side Telegram expectations remain, Evidence .omo/evidence/task-5-worker-owns-telegram-delivery-failure.txt.
  Commit: Y | refactor(go): remove telegram from ingest runtime

- [x] 6. Go share/telegram code retirement: remove routes, DB helpers, tables, and tests that no longer belong to web mail
  What to do / Must NOT do: Remove Go’s Telegram/share-only code paths entirely: `internal/telegram/*`, `internal/db/share.go`, `internal/db/telegram.go`, `internal/server/share.go`, share/telegram-specific tests, and route registrations for `/share/{token}`. Add a new migration that drops `share_tokens` and `telegram_deliveries`, plus any down migration needed by the repo’s migration convention. Document in code/tests that old `/share/{token}` URLs are intentionally retired. Must NOT leave dead tables, handlers, or tests implying Go still owns public preview links.
  Parallelization: Wave 2 | Blocked by: 5 | Blocks: 7,8
  References (executor has NO interview context - be exhaustive): internal/server/share.go:1-140; internal/server/server.go:115-118; internal/db/share.go:1-76; internal/db/telegram.go:1-61; internal/db/migrations/002_add_share_and_delivery_tables.up.sql:1-24; internal/db/migrations/002_add_share_and_delivery_tables.down.sql:1-2; internal/server/share_test.go; internal/db/share_test.go; internal/db/telegram_test.go; internal/telegram/client_test.go; internal/telegram/delivery_test.go; internal/telegram/payload_test.go.
  Acceptance criteria (agent-executable): `go test ./...` passes after share/telegram code removal; migration tests pass; route-level tests confirm `/share/*` is no longer served; repository search finds no remaining production references to `share_tokens`, `telegram_deliveries`, or `internal/telegram` except in historical migration cleanup notes.
  QA scenarios (name the exact tool + invocation): happy - `go test ./...` covers remaining web-mail routes and migrations, Evidence .omo/evidence/task-6-worker-owns-telegram-delivery.txt; failure - route/migration tests prove removed `/share/*` behavior and dropped table references fail if still registered, Evidence .omo/evidence/task-6-worker-owns-telegram-delivery-failure.txt.
  Commit: Y | refactor(go): retire share and telegram subsystems

- [x] 7. Config/docs cleanup: move Telegram ownership fully into Worker and document TTL-bound preview behavior
  What to do / Must NOT do: Update top-level README, Worker README, `.env.example`, `worker/.dev.vars.example`, and `worker/wrangler.toml` comments so the docs match the new runtime split: Go has no Telegram env vars; Worker owns `TELEGRAM_TOKEN`, `TELEGRAM_ID`, `DOMAIN`, `DB`, `MAIL_TTL`, and preview-link behavior. Explicitly document that preview URLs are Worker-hosted and expire with `MAIL_TTL`, and that old Go `/share/{token}` links are intentionally retired. Must NOT leave stale “server sends Telegram” text anywhere.
  Parallelization: Wave 2 | Blocked by: 1,5,6 | Blocks: 8
  References (executor has NO interview context - be exhaustive): README.md:1-74; worker/README.md:1-194; .env.example:1-13; worker/.dev.vars.example:1-8; worker/wrangler.toml:1-30; Makefile:14-29.
  Acceptance criteria (agent-executable): text search of repo docs/config finds no Go `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` guidance; Worker docs include KV binding + route setup + TTL warning; `cd worker && npm run build` still succeeds after config/doc changes.
  QA scenarios (name the exact tool + invocation): happy - repo text search and `cd worker && npm run build` show Worker-owned config only, Evidence .omo/evidence/task-7-worker-owns-telegram-delivery.txt; failure - targeted search proves old Go Telegram wording is removed and `/share/{token}` docs are retired, Evidence .omo/evidence/task-7-worker-owns-telegram-delivery-failure.txt.
  Commit: Y | docs(config): move telegram ownership to worker

- [x] 8. Cutover harness + final regression surface: make the migration safe to deploy and verify end-to-end
  What to do / Must NOT do: Add the final safety rails needed to execute the migration cleanly: if helpful, add a combined repo verification target or documented command sequence (`go vet ./...`, `go test ./...`, `cd worker && npm test`, `cd worker && npm run build`), and codify the rollout order in docs/tests: (1) provision Worker KV namespace and HTTP route, (2) deploy Worker code with Telegram env absent, (3) deploy Go cleanup release, (4) set Worker Telegram secrets/env to activate sending, (5) verify preview URLs and Telegram messages, (6) keep rollback instructions limited to restoring the previous Go release and unsetting Worker Telegram env. Must NOT create a new feature flag or callback API unless a failing regression test proves the documented ordering is insufficient.
  Parallelization: Wave 2 | Blocked by: 1,2,3,4,5,6,7 | Blocks: F1-F4
  References (executor has NO interview context - be exhaustive): Makefile:1-69; worker/package.json:1-19; worker/README.md:102-145; worker/wrangler.toml:16-30; README.md deployment/config sections; draft decisions in `.omo/drafts/worker-owns-telegram-delivery.md`.
  Acceptance criteria (agent-executable): documented cutover order exists in repo docs/plan-facing docs; `go vet ./...`, `go test ./...`, `cd worker && npm test`, and `cd worker && npm run build` all pass; final regression tests cover Worker preview route + Telegram flow and confirm Go still serves the authenticated web-mail UI/API without share/telegram dependencies.
  QA scenarios (name the exact tool + invocation): happy - run the full verification command set and capture outputs, Evidence .omo/evidence/task-8-worker-owns-telegram-delivery.txt; failure - validate rollback instructions against the same command set after unsetting Worker Telegram env or restoring prior Go release in a test environment, Evidence .omo/evidence/task-8-worker-owns-telegram-delivery-rollback.txt.
  Commit: Y | chore(release): codify worker telegram cutover and verification

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [x] F1. Plan compliance audit
- [x] F2. Code quality review
- [x] F3. Real manual QA
- [x] F4. Scope fidelity

## Commit strategy
- Prefer one commit per todo unless two adjacent todos become inseparable during refactor; keep Worker ownership commits separate from Go retirement commits for rollback clarity.
- Expected commit order:
  1. Worker runtime scaffold
  2. Worker preview cache/fetch routes
  3. Worker parse/render/Telegram send
  4. Worker email-flow integration
  5. Go ingest cleanup
  6. Go share/telegram retirement + migration
  7. Config/docs cleanup
  8. Cutover/verification harness
- Do not squash Worker introduction and Go deletion into a single monolithic commit; the executor needs a reversible cutover path.

## Success criteria
- Worker owns Telegram sending end-to-end and no production Go code sends Telegram anymore.
- Worker serves preview URLs at `/email/:id?mode=text|html` from KV-backed cached mail content and the Telegram buttons point there.
- Raw MIME ingest into Go remains unchanged and continues to populate web-mail storage/UI successfully.
- Telegram send failure after accepted ingest does not reject the email.
- Duplicate ingest does not send Telegram and does not create a new preview cache entry.
- Go no longer exposes `/share/{token}` routes, no longer loads Go `TELEGRAM_*` config, and no longer keeps `share_tokens` / `telegram_deliveries` as active schema.
- `go vet ./...`, `go test ./...`, `cd worker && npm test`, and `cd worker && npm run build` all pass.
