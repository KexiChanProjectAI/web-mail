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
