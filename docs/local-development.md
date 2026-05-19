# Local Development Guide

This guide walks you through setting up and running lite-mail locally for development.

## Prerequisites

- **Go** 1.21 or later
- **Node.js** 18+ (for Cloudflare Worker development)
- **MariaDB** 10.5 or later
- **Git** for version control

## Quick Start

### 1. Clone the Repository

```bash
git clone <repository-url>
cd lite-mail
```

### 2. Configure Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

Edit `.env` with your settings. See [Environment Variables](#environment-variables) for details.

### 3. Set Up the Database

#### Create the Database and User

Connect to MariaDB as root:

```bash
mysql -u root -p
```

Execute the following commands:

```sql
CREATE DATABASE lite_mail;
CREATE USER 'lite_mail'@'localhost' IDENTIFIED BY 'your_password';
GRANT ALL ON lite_mail.* TO 'lite_mail'@'localhost';
FLUSH PRIVILEGES;
EXIT;
```

#### Run Migrations

Migrations run automatically when the server starts for the first time.

To manually trigger migrations (if supported):

```bash
go run ./cmd/server -migrate-only
```

### 4. Start the Server

```bash
go run ./cmd/server
```

Or using Make:

```bash
make run
```

The server starts on **http://localhost:8080**.

### 5. Open in Browser

Navigate to **http://localhost:8080** to access the lite-mail interface.

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | - | MariaDB connection string (e.g., `mysql://user:password@localhost:3306/lite_mail`) |
| `DATA_DIR` | No | `./data` | Directory for email data storage |
| `PUBLIC_BASE_URL` | Yes | - | Public URL of the service (e.g., `http://localhost:8080`) |
| `MAX_MESSAGE_BYTES` | No | `26214400` | Maximum message size in bytes (~25MB) |
| `SESSION_COOKIE_NAME` | No | `lite_mail_session` | Name of the session cookie |
| `SESSION_TTL_HOURS` | No | `24` | Session time-to-live in hours |
| `NORMAL_USER_PSK` | Yes | - | Pre-shared key for normal user authentication |
| `ADMIN_PSK` | Yes | - | Pre-shared key for admin access |
| `WORKER_INGEST_PSK` | Yes | - | Pre-shared key for Worker ingest endpoint |
| `INGEST_URL` | Yes | - | Full URL to Go service ingest endpoint (e.g., `http://localhost:8080/api/ingest`) |

## Database Setup

### Manual Setup

1. Create database:
   ```sql
   CREATE DATABASE lite_mail CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
   ```

2. Create user with appropriate privileges:
   ```sql
   CREATE USER 'lite_mail'@'localhost' IDENTIFIED BY 'strong_password';
   GRANT ALL PRIVILEGES ON lite_mail.* TO 'lite_mail'@'localhost';
   FLUSH PRIVILEGES;
   ```

3. Grant write access to data directory (if using filesystem storage):
   ```bash
   mkdir -p data attachments raw
   chmod -R 775 data attachments raw
   ```

### Using Make

```bash
make db-setup
```

## Worker Development

The Cloudflare Worker handles email ingestion.

### Install Dependencies

```bash
cd worker
npm install
```

### Run Tests

```bash
cd worker
npm test
```

Or from the root:

```bash
make test-worker
```

### Build for Deployment

```bash
cd worker
npm run build
```

### Local Worker Development

To test the worker locally with wrangler:

```bash
cd worker
npx wrangler dev
```

Note: This requires a Cloudflare account and appropriate permissions.

## Testing

### Unit Tests

Run all Go tests:

```bash
make test
```

Or directly:

```bash
go test ./...
```

### Integration Tests

Run integration tests with a test database:

```bash
TEST_DATABASE_URL="user:password@tcp(localhost:3306)/lite_mail_test" go test ./tests/integration/ -v
```

Or using Make:

```bash
make test-integration
```

### Worker Tests

```bash
make test-worker
```

Or directly:

```bash
cd worker && npm test
```

### All Quality Checks

Run all quality gates at once:

```bash
make all
```

This runs: `vet`, `build`, and `test`.

## Useful Commands

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make all` | Run vet, build, and test |
| `make build` | Build the server binary to `bin/lite-mail` |
| `make run` | Run the server with `go run` |
| `make test` | Run all Go tests |
| `make test-integration` | Run integration tests |
| `make test-worker` | Run worker tests |
| `make vet` | Run `go vet` for static analysis |
| `make clean` | Remove build artifacts (`bin/`) |
| `make db-setup` | Database setup instructions |
| `make migrate` | Run database migrations |
| `make help` | Show available targets |

### Manual Server Commands

Build and run manually:

```bash
go build -o bin/lite-mail ./cmd/server
./bin/lite-mail
```

Run with migrations only:

```bash
go run ./cmd/server -migrate-only
```

## Project Structure

```
lite-mail/
├── cmd/server/          # Main server application
├── internal/            # Internal packages
│   ├── api/             # HTTP API handlers
│   ├── auth/            # Authentication
│   ├── config/          # Configuration loading
│   ├── db/              # Database operations
│   ├── ingest/          # Email ingestion
│   ├── middleware/       # HTTP middleware
│   ├── server/          # Server setup
│   └── storage/         # File storage
├── migrations/          # Database migrations
├── static/             # Static assets
├── tests/              # Integration tests
│   └── integration/    # Integration test suite
├── worker/             # Cloudflare Worker
│   └── src/            # Worker source code
├── docs/               # Documentation
├── .env.example        # Environment template
└── Makefile            # Build commands
```

## Troubleshooting

### Database Connection Issues

- Ensure MariaDB is running: `systemctl status mariadb` or `service mariadb status`
- Verify credentials in `.env`
- Check that the user has proper privileges: `GRANT ALL ON lite_mail.* TO 'lite_mail'@'localhost';`

### Port Already in Use

If port 8080 is occupied:

```bash
# Find what's using the port
lsof -i :8080

# Kill the process or change PORT in .env
```

### Worker Tests Failing

- Ensure you're in the `worker/` directory
- Verify Node.js 18+ is installed: `node --version`
- Clear node_modules and reinstall: `rm -rf node_modules && npm install`

### Build Errors

- Ensure Go 1.21+ is installed: `go version`
- Clean and rebuild: `make clean && make build`

### Migration Failures

- Check database user has DDL privileges
- Ensure the database exists: `CREATE DATABASE lite_mail;`
- Review migration files in `migrations/`

## Getting Help

- See `README.md` for project overview
- See `docs/cloudflare.md` for Cloudflare Worker setup
- See `docs/deployment.md` for production deployment
