# Cloudflare Email Worker - Implementation Notes

## Overview

The Cloudflare Email Worker handles inbound email reception via Cloudflare Email Routing and forwards raw MIME messages to the Go service ingest endpoint for storage and processing.

**Important**: This Worker does NOT use `message.forward()`. The `forward()` method is for routing emails to external email addresses verified in Cloudflare Email Routing. Our architecture uses `fetch()` to POST raw MIME to our own Go service endpoint.

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
  readonly to: string;          // Envelope RCPT TO (recipient)
  readonly headers: Headers;    // Email headers (Subject, Message-ID, etc.)
  readonly raw: ReadableStream; // Raw MIME email content stream
  readonly rawSize: number;     // Size of raw email in bytes
  readonly canBeForwarded: boolean;

  // Actions
  setReject(reason: string): void;
  forward(rcptTo: string, headers?: Headers): Promise<void>;
  reply(message: EmailMessage): Promise<void>;
}
```

**Reference**: https://developers.cloudflare.com/email-routing/email-workers/runtime-api/

### Reading the Raw Stream

```typescript
const reader = message.raw.getReader();
const chunks: Uint8Array[] = [];

while (true) {
  const { done, value } = await reader.read();
  if (done) break;
  chunks.push(value);
}
reader.releaseLock();

// Combine chunks into ArrayBuffer
const totalLength = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
const rawMIME = new ArrayBuffer(totalLength);
const view = new Uint8Array(rawMIME);
let offset = 0;
for (const chunk of chunks) {
  view.set(chunk, offset);
  offset += chunk.length;
}
```

## Limits and Constraints

### Message Size Limits

| Limit Type | Value | Notes |
|------------|-------|-------|
| Email Routing message size | **30 MB** | For inbound emails via Email Routing |
| Worker memory | **128 MB** | Per Worker invocation |
| Worker CPU time (Standard) | **50 ms** default, configurable up to 30,000 ms | For Standard tier |
| Worker subrequests | 50 default, configurable up to 150 | Number of `fetch()` calls |

**Reference**: https://developers.cloudflare.com/workers/wrangler/configuration/index.md#configure-worker-runtime-limits

### Runtime Limits Configuration

```toml
[limits]
cpu_ms = 100  # Sufficient for reading stream and single POST
```

### Request Timeout

The Worker implements a 30-second timeout for the ingest POST request to prevent hanging.

## Required Secrets and Configuration

### Environment Variables

| Variable | Type | Description |
|----------|------|-------------|
| `INGEST_URL` | `string` | Go service ingest endpoint URL (e.g., `https://mail.example.com/ingest`) |
| `WORKER_INGEST_PSK` | `string` | Pre-shared key for worker-to-service authentication |

### Setting Secrets

```bash
# Set the PSK secret (will be prompted for value)
wrangler secret put WORKER_INGEST_PSK

# Verify secrets
wrangler secret list
```

**Never commit real secrets to version control.** The `wrangler.toml` uses placeholders only.

## Ingest Endpoint Protocol

### Request

- **Method**: `POST`
- **URL**: Configured via `INGEST_URL` environment variable
- **Headers**:
  - `Content-Type: message/rfc822` - Raw MIME format
  - `X-Lite-Mail-Ingest-PSK: <WORKER_INGEST_PSK>` - Authentication
  - `X-Lite-Mail-From: <sender@email>` - Original sender
  - `X-Lite-Mail-To: <recipient@email>` - Original recipient
- **Body**: Raw MIME email as `ArrayBuffer`

### Response

The Go service should return:
- `200 OK` on success
- `4xx` or `5xx` on failure (Worker will log error)

## Email Routing Setup

### Steps to Configure

1. **Add domain to Cloudflare Email Routing**
   - Go to Cloudflare Dashboard > Email > Email Routing
   - Add your domain and verify DNS

2. **Create routing rules**
   - Create a custom address (e.g., `*@yourdomain.com` for catch-all)
   - Set the destination to this Worker

3. **Verify destination addresses** (if using `message.forward()`)
   - For our architecture, we POST to Go service, so no verification needed

### Catch-All Configuration

For catch-all inbound email:

1. In Cloudflare Dashboard > Email > Email Routing > Email addresses
2. Create a rule: `*@yourdomain.com` → Worker

**Reference**: https://developers.cloudflare.com/email-routing/

## Deployment

### Prerequisites

- Wrangler CLI installed (`npm install -g wrangler`)
- Cloudflare account with Email Routing enabled
- Domain added to Cloudflare

### Deploy Steps

```bash
cd worker

# Install dependencies
npm install

# Set required secrets
wrangler secret put WORKER_INGEST_PSK

# Deploy
npm run deploy
```

### Testing Locally

```bash
# Start local development server (wrangler dev)
npm run dev

# Send a test email using the local endpoint
curl --request POST 'http://localhost:8787/cdn-cgi/handler/email' \
  --url-query 'from=sender@example.com' \
  --url-query 'to=recipient@example.com' \
  --data-raw 'From: sender@example.com
To: recipient@example.com
Subject: Test Email
Message-ID: <test-id@example.com>

Test body content'
```

**Reference**: https://developers.cloudflare.com/email-routing/email-workers/runtime-api/

## Security Considerations

1. **PSK Authentication**: The Worker authenticates to the Go service using a pre-shared key stored as a Cloudflare secret.

2. **Header Validation**: The Go service should validate `X-Lite-Mail-Ingest-PSK` matches its configured value.

3. **No User Input Parsing**: The Worker does not parse or mutate the MIME content - it only reads the raw stream and forwards it.

4. **Content-Type**: Using `message/rfc822` indicates raw MIME format per RFC 5322.

## File Structure

```
worker/
├── src/
│   └── index.ts       # Email handler implementation
├── test/
│   └── index.test.ts  # Vitest unit tests
├── wrangler.toml      # Configuration template
├── package.json        # Dependencies and scripts
├── tsconfig.json      # TypeScript configuration
└── vitest.config.ts   # Test configuration

docs/
└── cloudflare.md      # This documentation
```

## References

- [Cloudflare Email Routing Documentation](https://developers.cloudflare.com/email-routing/)
- [Email Worker Runtime API](https://developers.cloudflare.com/email-routing/email-workers/runtime-api/)
- [Workers Handler: email](https://developers.cloudflare.com/workers/runtime-apis/handlers/email/)
- [Wrangler Configuration](https://developers.cloudflare.com/workers/wrangler/configuration/)
- [Workers Limits](https://developers.cloudflare.com/workers/wrangler/configuration/index.md#configure-worker-runtime-limits)
