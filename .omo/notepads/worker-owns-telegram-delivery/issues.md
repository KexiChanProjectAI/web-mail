# Worker-Owns-Telegram-Delivery — Issues

## task 1: Worker runtime contract scaffold (2026-07-04)

### ISSUE-1: Pinned upstream commit `6d6ffbc055...` not reachable

The plan references
`https://github.com/mail2telegram/mail2telegram @ 6d6ffbc055280c809939c3faf99074384d540fe5`,
but that `org/repo` does not exist on GitHub (HTTP 404 for every
`raw.githubusercontent.com` URL with that path). The real upstream is
`TBXark/mail2telegram`. We proceeded using the current `master` of
that repo because the `Environment` and `EmailCache` shapes are
unchanged across recent history.

**Impact:** low for task 1 (only the contract surface, no copied
logic). If a future task needs the exact pinned commit to confirm a
specific implementation detail, the executor will need to either
(a) ask the user to provide a working URL or (b) cite a different
upstream commit that contains the same code.

**Suggested remediation:** before tasks 3 and 4 (which copy logic
from upstream), re-verify the upstream commit by searching for the
specific commit hash across GitHub.

### ISSUE-2: Pre-existing tsc errors in HEAD

`tsc --noEmit` reports errors in `src/index.ts` (`ctx` unused) and
`test/index.test.ts` (MockContext missing `props`). These are
pre-existing at the commit I started from. They do NOT block
`npm test` (vitest) or `npm run build` (wrangler's esbuild bundle)
because neither runs `tsc` strict checks. `npx tsc --noEmit` was
used as an additional diagnostic only.

**Impact:** zero for task 1 acceptance criteria. The pre-existing
errors are not regressions.

**Suggested remediation:** a follow-up cleanup that either (a)
prefixes the `ctx` parameter with `_ctx` in the `email` signature
and updates the test mock to provide `props: {} as any`, or (b)
adds a `// @ts-expect-error` line per occurrence. Out of scope
for task 1.

### ISSUE-3: Verification-gap fixes (2026-07-04, follow-up review)

Two gaps were caught in the task 1 review pass:

**3a. `.dev.vars.example` was not actually written.** The previous
turn's `write` call was rejected because the file already existed
(`File already exists. Use edit tool instead.`), and the `edit` step
that should have followed it was missed. The reviewer's claim that
the file was updated was wrong. Fixed by re-applying the full
contents via `write`.

**3b. `contract.test.ts:15` used `types.Env` in a type annotation.**
The `types` variable is the runtime import result and TS treats
`types.Env` as a value lookup, not a type. LSP reported
`Cannot find namespace 'types'`. Fixed by adding
`import type { Env } from '../src/types';` at the top and using
`Env` directly in the annotation.

**Side effect of moving `Env` to `src/types`:** The local `Env`
interface in `test/index.test.ts` did not include `DB`, so once
`index.ts` started using the canonical `Env` type the test
inherited a new error class: `Property 'DB' is missing in type
'Env' but required in type 'import("/.../types/index").Env'`. The
pre-existing MockContext `props` errors stayed but were temporarily
masked by tsc short-circuiting on the first error per call site.
Fixed by adding `DB: KVNamespace` to the local test `Env` interface
and seeding `DB: {} as KVNamespace` in `createMockEnv`. After the
fix, tsc reports the same 17 unique errors as HEAD (1 ctx unused +
16 MockContext) — no new error class introduced.

**Also caught:** `src/mail/parse.ts` had `void maxSize; void
maxSizePolicy;` AFTER a `return` statement, producing an
`unreachable code` hint in LSP. Moved the `void` calls before
the return. No behavior change; this is purely a type-hint fix.

Final verification: `cd worker && npm test` passes 24/24,
`cd worker && npm run build` succeeds (6.81 KiB bundle), `npx tsc
--noEmit` shows the same 17 pre-existing errors as HEAD (1 ctx
unused + 16 MockContext), `lsp_diagnostics` on
`worker/test/contract.test.ts` is clean, `lsp_diagnostics` on
`worker/src/` shows only the pre-existing `ctx` unused error.
Evidence: `.omo/evidence/task-1-worker-owns-telegram-delivery.txt`,
`.omo/evidence/task-1-worker-owns-telegram-delivery-build.txt`,
`.omo/evidence/task-1-worker-owns-telegram-delivery-tsc.txt`.

## worker parse-input fix: ForwardableEmailMessage spread bug (2026-07-04)

### ISSUE-4: Spreading `ForwardableEmailMessage` broke `headers.get(...)`

`worker/src/index.ts` previously built the local parse input with:

```ts
const messageForParse = {
  ...message,
  raw: streamFromBytes(rawMIME),
} as ForwardableEmailMessage;
```

This is unsafe for Cloudflare's `ForwardableEmailMessage` because its runtime
surface includes non-enumerable / getter-backed properties (`headers`, `from`,
`to`, `raw`, etc.). Spreading the object produced a plain object that did not
reliably preserve `headers`, so `parseEmail()` later executed
`message.headers.get("...")` against `undefined`, producing the real failure:

`Email body could not be parsed: Cannot read properties of undefined (reading 'get')`

### Fix applied

- Added `ParseInput` / `ParseHeaders` in `worker/src/types/index.ts`.
- Changed `parseEmail()` to accept `ParseInput` instead of a raw
  `ForwardableEmailMessage`.
- Added `getHeaderValue()` in `worker/src/mail/parse.ts` so header reads are
  normalized through `Headers` or a plain map-like object.
- Changed `worker/src/index.ts` to pass a plain parse object with explicit
  fields:

```ts
{
  headers: message.headers,
  from: message.from,
  to: message.to,
  rawSize: message.rawSize,
  raw: streamFromBytes(rawMIME),
}
```

This preserves the original one-read flow:
1. read `message.raw` once into `rawMIME`
2. POST that exact buffer to Go ingest unchanged
3. locally parse a fresh stream recreated from the same bytes

### Verification

- Added parser coverage in `worker/test/telegram.test.ts` for plain-object
  parse input.
- `cd worker && npm test` → 61/61 passed.
- `cd worker && npm run build` → wrangler dry-run passed.
- `cd worker && npx wrangler deploy` → deployed `web-mail` version
  `f170b317-5cc4-41b0-b558-e84d38e13e6e` at
  `https://web-mail.yuanzhisheng.workers.dev`.
- `lsp_diagnostics worker/src` still shows only the pre-existing unused `ctx`
  parameter in `src/index.ts`; no new diagnostics from this fix.
