---
slug: worker-owns-telegram-delivery
status: planned
intent: clear
review_required: false
pending-action: await user choice: start work or request high-accuracy review
approach: Rebase the Cloudflare Worker onto mail2telegram-style email parsing plus Telegram sending and direct upstream-style Worker share links (`/email/:id?mode=text|html`) backed by Worker-side preview cache, keep Go only as the web-mail ingest/storage/UI backend, and remove Go-side Telegram sending, share-token, and delivery-audit responsibilities entirely.
---

# Draft: worker-owns-telegram-delivery

## Components (topology ledger)
| id | outcome (one line) | status | evidence path |
| --- | --- | --- | --- |
| C1 | Remove Go-side Telegram sender wiring and async post-ingest send path while preserving ingest/storage/share behavior | active | internal/server/server.go:68-78; internal/ingest/handler.go:137-171 |
| C2 | Rework Worker around mail2telegram-style parsing and Telegram send flow, while still posting accepted mail to Go | active | worker/src/index.ts:62-108; worker/test/index.test.ts:89-508; upstream src/telegram/api.ts; upstream src/handler/mail/index.ts |
| C3 | Collapse Go responsibilities down to web-mail only by removing Telegram callback/audit/share-token dependencies from the migration target | active | internal/ingest/handler.go:125-171; internal/db/telegram.go:13-60; internal/db/share.go:22-48; internal/db/migrations/002_add_share_and_delivery_tables.up.sql:1-24 |
| C4 | Add upstream-style Worker share-link/cache surface plus env/config/docs/tests/deploy updates so Worker fully owns Telegram credentials, preview links, and any ephemeral Telegram-side state | active | worker/src/index.ts:62-108; upstream src/handler/fetch/index.ts; upstream src/db/index.ts; upstream src/types/index.ts; worker/.dev.vars.example:1-8; worker/README.md |

## Open assumptions (announced defaults)
| assumption | adopted default | rationale | reversible? |
| --- | --- | --- | --- |
| Go retains Telegram data-plane responsibilities after migration | Do not retain them; Go should keep only web-mail responsibilities | User superseded 1A with “Go只保留web mail部分” | yes |
| Telegram UX after migration | Preserve summary + `View as TXT` / `View as HTML` buttons, but point them at upstream-style Worker preview URLs instead of Go `/share/...` URLs | User first chose 2A, then explicitly changed share-link ownership to the upstream Worker model | yes |
| Test strategy | TDD / contract-first tests before behavior change | User chose 3A; highest-signal path for a cross-runtime migration | yes |

## Findings (cited - path:lines)
- Current Worker contract is ingest-only: env exposes only `INGEST_URL` and `WORKER_INGEST_PSK`, and `email()` only collects raw MIME then calls `postToIngest(...)` with no Telegram path. Evidence: worker/src/index.ts:3-6,62-108,110-251.
- Current Go ingest handler returns `{status:"accepted", message_id}` immediately after persist, then asynchronously performs Telegram-specific work in a goroutine. Evidence: internal/ingest/handler.go:125-171.
- Go Telegram-specific work currently includes share token creation, summary rendering, reply-markup building, and delivery-status persistence, all of which become removable if Go is reduced to web-mail only. Evidence: internal/db/share.go:22-48; internal/telegram/payload.go:29-79; internal/telegram/delivery.go:35-86; internal/db/telegram.go:13-60.
- Go server only wires Telegram sender dependencies when `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` are configured. Evidence: internal/server/server.go:68-78; internal/config/config.go:66-69.
- The persisted audit model already exists and is one-row-per-message via `telegram_deliveries` with states `pending|skipped|sent|failed`. Evidence: internal/db/migrations/002_add_share_and_delivery_tables.up.sql:12-24; internal/db/models.go:14-24.
- Existing Worker tests cover ingest posting, timeout/retry, and rejection semantics, but do not cover any Telegram behavior. Evidence: worker/test/index.test.ts:89-508.
- Existing Go ingest tests explicitly verify current Telegram-enabled/disabled/non-blocking behavior, so they will need to be rewritten around the new contract. Evidence: internal/ingest/handler_test.go:138-324.
- Upstream `mail2telegram` exposes a Worker-native Telegram API client using `fetch()` only and a minimal send pattern `createTelegramBotAPI(...).sendMessageWithReturns(...)`. Evidence: upstream src/telegram/api.ts; upstream src/handler/mail/index.ts:7-23.
- Upstream Worker preview/share links are served by a Worker `fetch` route `GET /email/:id?mode=text|html` backed by KV-stored mail cache (`dao.loadMailCache(id)`), not by Go share tokens. Evidence: upstream src/handler/fetch/index.ts:109-124; upstream src/db/index.ts:52-65.
- Upstream mail parsing stores a Worker-generated cache id plus parsed `text`/`html` payload for later preview retrieval, using `PostalMime` and HTML-to-text fallback. Evidence: upstream src/mail/parse.ts:30-71.
- Worker-local secrets currently do not include any Telegram credentials, preview-cache binding, or public-base-url/domain inputs. Evidence: worker/.dev.vars.example:1-8; worker/wrangler.toml:23-30; upstream src/types/index.ts:17-35.

## Decisions (with rationale)
- Adopt a Worker-owned Telegram sending architecture based on mail2telegram’s Worker-native parsing/sending approach, because the user explicitly requested the Worker be directly based on that upstream project rather than keeping the Go sender.
- Keep Go as the system of record only for ingest, durable message storage, and the existing web-mail UI/API surface, because the user explicitly narrowed Go to “web mail only”.
- Move Telegram preview-link ownership to the Worker by adopting upstream-style `GET /email/:id?mode=text|html` routes backed by Worker-side preview cache, because the user explicitly changed the share-link decision to “use the upstream Worker share-link approach directly”.
- Keep the Go ingest response minimal (`accepted` / `duplicate` plus `message_id` on accepted) and stop requiring Go share-token payloads for Telegram buttons, because preview links will now be Worker-native rather than Go `/share/...` URLs.
- Do not add a Worker->Go Telegram status callback contract; Telegram-side observability belongs to the Worker path once Go is reduced to web-mail only.
- Preserve the current summary/button UX instead of importing upstream callback-data / interactive-preview flows; only the button targets move from Go share URLs to Worker preview URLs.
- Use TDD as the migration guardrail, starting with Worker contract tests and Go endpoint/contract tests before deleting the old Go sender path, because the user chose 3A.

## Scope IN
- Worker env/config expansion for Telegram credentials and public/share-related inputs.
- Worker integration of mail2telegram-inspired Telegram client + email parsing/rendering pieces, adapted to lite-mail’s current summary/button UX.
- Worker `fetch`/preview surface and preview-cache persistence needed to support upstream-style share links.
- Removal of Go-side Telegram sender wiring and its current async post-ingest send path.
- Removal of Go-side Telegram share-token, share-route-for-Telegram, and delivery-audit responsibilities that are no longer part of “web mail only”.
- Full documentation/test/deployment updates for the new ownership split.

## Scope OUT (Must NOT have)
- No reintroduction of Go as an active Telegram sender or fallback sender.
- No import of mail2telegram’s callback-data/interactive-preview/KV product surface into lite-mail for this migration.
- No plan to remove Go ingest/storage/message persistence; Worker remains a sender + preview host, not the source of record.
- No requirement that Telegram buttons keep using Go `/share/{token}` URLs; that decision has been superseded by the user’s upstream Worker share-link request.
- No new Go-only Telegram callback/audit API; Telegram delivery behavior lives entirely on the Worker side.

## Open questions
- None blocking. User resolved the surviving owner-level forks as 1A / 2A / 3A.

## Review receipts
- Metis review session `ses_0d4b3078cffe4708RNviwj3GH7`: identified missing response/callback contracts on the earlier Go-audit-preserving version; findings were superseded by the later approved scope change to upstream Worker preview links + Go web-mail-only ownership.
- Metis review session `ses_0d49e0325ffe1nMOUCqD6SZQNI`: identified final-scope gaps around raw-MIME-vs-JSON ingest, Worker deps/KV/fetch contract, migration cleanup, sequencing, and acceptance criteria; integrated into `.omo/plans/worker-owns-telegram-delivery.md`.

## Approval gate
status: approved-and-written
pending action: await user choice: start work or request high-accuracy review
approach summary: Plan written. It moves Telegram sending and preview-link hosting into the Worker using mail2telegram-style Worker-native logic, keeps Go only for web-mail ingest/storage/UI responsibilities, keeps raw MIME ingest unchanged, and removes Go-side Telegram/share/audit code after TDD-backed replacement coverage is in place.
