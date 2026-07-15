# Worker-Owns-Telegram-Delivery — Decisions

## task 1: Worker runtime contract scaffold (2026-07-04)

### DEC-1: Re-export `Env` from `src/index.ts` for backwards compatibility

The original `worker/src/index.ts` defined and exported `interface Env`.
External consumers (and the original test) reference it via
`import type { Env } from './index'`. We moved the canonical `Env` to
`src/types/index.ts` and added `export type { Env } from './types'` at
the top of `src/index.ts` so the move is non-breaking. The same
re-export is used by `handler/mail` and `handler/fetch` to import the
shared `Env` type.

### DEC-2: `Env` keeps `INGEST_URL` and `WORKER_INGEST_PSK` optional

The plan's "must have" list says the new env vars are added while the
existing ingest env vars are retained. The existing fields were
declared `?: string` (optional) in the original `index.ts`. We
preserved that, and made every new field optional too — except `DB`,
which is required (it's the upstream `Environment.DB: KVNamespace`
shape). Optional env lets the Worker boot with Telegram disabled
during the staged cutover described in task 8.

### DEC-3: Pinned `DB` is required, not optional

`src/types/index.ts` declares `DB: KVNamespace` (no `?`). This matches
upstream `Environment` where `DB` is required. Even though task 1
does not exercise the KV yet, the type-level requirement is what
guarantees the DAO constructor and the `fetch` handler signatures
type-check correctly. Task 2 will move from "type-only required" to
"runtime required" by reading `env.DB` directly.

### DEC-4: New submodule stubs are minimal and self-documenting

Each new file (`db/index.ts`, `mail/parse.ts`, `telegram/api.ts`,
`handler/fetch/index.ts`) ships the function/class signature the rest
of the surface needs plus a short docstring pointing at the upstream
file and the task that will implement it. No method bodies for tasks
2-4 are written in task 1. This keeps the diff small and the failing
tests honest.

### DEC-5: `handler/mail/emailHandler` delegates to the existing `email()`

Rather than duplicate the raw-MIME ingest pipeline, the new
`emailHandler` does a dynamic `import('..')` and calls the existing
`email()`. This is the smallest change that satisfies
"re-exports handler/mail with emailHandler" while keeping all 16
existing ingest tests green. Task 4 will inline the upstream flow and
delete the dynamic import.

### DEC-6: `fetch` export in `src/index.ts` uses a thin wrapper, not a direct re-export

The default export is `{ email, fetch: handlerFetchStub }` where
`handlerFetchStub(request, env, ctx)` does `const { fetchHandler } =
await import('./handler/fetch'); return fetchHandler(request, env)`.
The wrapper exists for two reasons:
1. It defers the cost of pulling in `itty-router` and the rest of
   the fetch module until an actual HTTP request lands, so the
   `email()` flow does not pay for unused code (Cloudflare Workers
   charge per module loaded).
2. It matches the upstream entrypoint pattern where `index.ts`
   imports the handler factories from the handler modules.

### DEC-7: `wrangler.toml` documents but does not commit real bindings

We updated `wrangler.toml` with comments showing the exact
`[[kv_namespaces]]` and `[[routes]]` blocks the operator needs to
add, but did not commit concrete `id` / `pattern` values. Same for
secrets — we added the `wrangler secret put` lines to the comment
list. The decision is to keep `wrangler.toml` shareable; every
deployment fills it in.

### DEC-8: `.dev.vars.example` includes the new vars with safe defaults

`TELEGRAM_TOKEN` and `TELEGRAM_ID` default to empty (Telegram is
opt-in, matches the README's "disabled when either is empty" rule).
`DOMAIN`, `MAIL_TTL`, `MAX_EMAIL_SIZE`, `MAX_EMAIL_SIZE_POLICY` get
real defaults that match upstream's documented defaults. Operators
copy the file to `.dev.vars` and only fill in what they need.

### DEC-9: Pre-existing TypeScript errors are out of scope

`npx tsc --noEmit` reports pre-existing errors at HEAD that are not
caused by task 1:
- `src/index.ts(70,1): 'ctx' is declared but its value is never read.`
  (The `ctx` param is in the public email signature and the Worker
  runtime passes a real `ExecutionContext`; removing it would
  break the API.)
- `test/index.test.ts(*,33): 'MockContext' is not assignable to
  parameter of type 'ExecutionContext<unknown>'.` (The mock helper
  predates the `props` field being added to `ExecutionContext`.)

These are intentionally left for a follow-up; fixing them is a
refactor that touches the public handler signature, which is
explicitly out of scope for task 1. `npm test` and `npm run build`
both pass, which is the documented acceptance criterion.
