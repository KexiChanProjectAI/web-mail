# Lite Mail Cloudflare Email Worker

## Overview

The Cloudflare Email Worker handles inbound email reception via Cloudflare Email Routing and forwards raw MIME messages to the Go service ingest endpoint for storage and processing.

**Architecture**: Email → Cloudflare Email Worker → Raw MIME POST → Go Service Ingest Endpoint

## Important Notes

- **Do NOT use `message.forward()` or `message.reply()`** - These are not in scope for this implementation
- **`message.raw` is a ReadableStream** - Must be consumed properly using a reader loop
- **Worker message size limit**: 30 MB (Cloudflare Email Routing limit)
- **Worker runtime limits**: up to 30s CPU (paid Standard plan; Free tier is 10ms), 128MB memory (configurable)

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

| Variable | Description |
|----------|-------------|
| `INGEST_URL` | Full URL to the Go service ingest endpoint (e.g., `https://mail.example.com/api/ingest`) |
| `WORKER_INGEST_PSK` | Pre-shared key for authentication with the ingest endpoint |

Set secrets via:
```bash
npx wrangler secret put INGEST_URL
npx wrangler secret put WORKER_INGEST_PSK
```

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
- POST to ingest endpoint with correct headers
- Authentication failure handling (401)
- Message size limit handling (413)
- Worker misconfiguration detection (missing env vars)
- Stream reading failures

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
npx wrangler secret put INGEST_URL
npx wrangler secret put WORKER_INGEST_PSK
```

### Configuration

The Worker is configured via `wrangler.toml`. Key settings:

```toml
name = "lite-mail-worker"
main = "src/index.ts"

[limits]
cpu_ms = 100  # Sufficient for reading stream and POSTing to ingest
```

Note: `INGEST_URL` and `WORKER_INGEST_PSK` must **only** be set via `wrangler secret put`, never in `wrangler.toml`. See [Set Secrets](#set-secrets) above.

## Architecture Details

### Email Flow

1. Cloudflare Email Routing receives an inbound email
2. The Worker `email()` handler is invoked with a `ForwardableEmailMessage`
3. The raw MIME stream is consumed via `collectStream()` helper
4. A POST request is made to the ingest endpoint with:
   - `Content-Type: message/rfc822`
   - `X-Lite-Mail-Ingest-PSK: <psk>`
   - Raw MIME as body
5. The Worker waits for the ingest POST to complete before acknowledging the email event

### Error Handling

- **Missing env vars**: Calls `message.setReject()` to reject the email
- **Stream read failure**: Logs error and rejects
- **401 from ingest**: Logs authentication failure and rejects the email
- **413 from ingest**: Logs message size error and rejects the email
- **Other non-OK status**: Logs error and throws so the invocation fails visibly
- **Fetch timeout**: 30 second timeout on the POST request

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
