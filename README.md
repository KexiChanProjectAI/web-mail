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
## Telegram Email Forwarding

The server can forward email summaries to a Telegram chat via a bot. This is entirely optional and disabled by default.

### How It Works
- After each email is successfully ingested, the server generates a permanent public share link for that message.
- A summary (sender, subject, body preview) is sent to the configured Telegram chat.
- The Telegram message includes **two inline URL buttons**:
  - **View as TXT** — opens the plain-text version in your browser
  - **View as HTML** — opens the HTML version in your browser

### Configuration

| Variable | Required | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | No | Telegram Bot API token from [@BotFather](https://t.me/BotFather) |
| `TELEGRAM_CHAT_ID` | No | Target chat ID (user, group, or channel) |
| `PUBLIC_BASE_URL` | Yes* | Public base URL (required for share links to work) |

*`PUBLIC_BASE_URL` is already required for the service — ensure it's set to your public-facing URL.

**Telegram forwarding is automatically disabled** when either `TELEGRAM_BOT_TOKEN` or `TELEGRAM_CHAT_ID` is empty.

### Setup Steps

1. Create a bot via [@BotFather](https://t.me/BotFather) and get the bot token.
2. Find your chat ID (send a message to the bot, then visit `https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates`).
3. Set `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in your environment.
4. Set `PUBLIC_BASE_URL` to your server's public URL.
5. Restart the server.

### Important Notes

- **No webhook required**: This uses only the outbound Bot API `sendMessage` method with inline URL buttons. No webhook setup, polling, or callback handlers are needed.
- **Non-blocking**: If the Telegram API is unreachable or returns an error, the email is still accepted and stored. The delivery failure is recorded for diagnostics.
- **Public share links**: Each message gets a permanent, cryptographically-random public URL. No login is required to view shared messages.
- **Content safety**: User-controlled content (sender, subject, body) is HTML-escaped before rendering in Telegram messages and public HTML views.
