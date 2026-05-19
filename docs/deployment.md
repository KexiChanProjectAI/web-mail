# Deployment Guide

This guide covers deploying lite-mail on a Linux server with systemd, MariaDB, and a reverse proxy (Caddy or nginx) for TLS termination.

## Prerequisites

- Go 1.21 or later
- MariaDB 10.5 or later
- Domain name with Cloudflare DNS control
- Caddy or nginx for reverse proxy with TLS
- Linux server with systemd

## Architecture Overview

```
Cloudflare Email Routing → Cloudflare Worker → Your Server (TLS) → lite-mail Go Service → MariaDB
                                                                  → Filesystem (DATA_DIR)
```

## Step 1: Database Setup

### Install MariaDB

```bash
# Debian/Ubuntu
apt install mariadb-server

# RHEL/CentOS
dnf install mariadb-server
```

### Create Database and User

```bash
mysql -u root -p
```

```sql
CREATE DATABASE IF NOT EXISTS lite_mail CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'lite_mail'@'localhost' IDENTIFIED BY 'your_secure_password';
GRANT ALL PRIVILEGES ON lite_mail.* TO 'lite_mail'@'localhost';
FLUSH PRIVILEGES;
EXIT;
```

### Run Migrations

The Go server auto-runs database migrations on startup. To run them manually:

```bash
# Build and run once to trigger migrations
lite-mail &>/var/log/lite-mail/migration.log
sleep 2
kill %1 2>/dev/null
```

Or run migrations manually using your preferred migration tool.

## Step 2: Go Service Configuration

### Create System User

```bash
groupadd --system lite-mail
useradd --system --gid lite-mail --home-dir /var/lib/lite-mail --shell /usr/sbin/nologin lite-mail
```

### Create Directory Structure

```bash
mkdir -p /var/lib/lite-mail/data/raw /var/lib/lite-mail/data/attachments /var/log/lite-mail
chown -R lite-mail:lite-mail /var/lib/lite-mail /var/log/lite-mail
chmod 750 /var/lib/lite-mail /var/log/lite-mail
```

### Copy and Configure Environment File

```bash
cp .env.example /opt/lite-mail/.env
chown root:root /opt/lite-mail/.env
chmod 600 /opt/lite-mail/.env
```

Edit `/opt/lite-mail/.env` with your production values:

```env
DATABASE_URL=mysql://lite_mail:your_secure_password@localhost:3306/lite_mail
DATA_DIR=/var/lib/lite-mail/data
PUBLIC_BASE_URL=https://mail.example.com
MAX_MESSAGE_BYTES=26214400
SESSION_COOKIE_NAME=lite_mail_session
SESSION_TTL_HOURS=24
NORMAL_USER_PSK=your_normal_user_psk
ADMIN_PSK=your_admin_psk
WORKER_INGEST_PSK=your_worker_ingest_psk
INGEST_URL=https://mail.example.com/api/ingest
APP_ENV=production
```

## Step 3: Build and Install

### Build the Binary

```bash
go build -o /usr/local/bin/lite-mail ./cmd/server
chown root:root /usr/local/bin/lite-mail
chmod 755 /usr/local/bin/lite-mail
```

### Verify the Build

```bash
# Test that the binary runs
lite-mail --help
```

## Step 4: Systemd Service

### Install the Service File

```bash
cp contrib/systemd/lite-mail.service /etc/systemd/system/
systemctl daemon-reload
```

### Enable and Start

```bash
systemctl enable --now lite-mail
```

### Check Status

```bash
systemctl status lite-mail
```

### View Logs

```bash
journalctl -u lite-mail -f
```

## Step 5: Reverse Proxy

See [reverse-proxy.md](reverse-proxy.md) for Caddy and nginx configuration examples.

## Step 6: Cloudflare Email Routing

See [cloudflare.md](cloudflare.md) for Cloudflare Email Worker setup and configuration.

## Step 7: Verify

### Check Health Endpoint

```bash
curl https://mail.example.com/healthz
```

### Test Email Flow

1. Send an email to `test@your-domain.com` (must be configured as catch-all in Cloudflare Email Routing)
2. Login to the web interface
3. Verify the message appears

## Environment Variables Reference

| Variable | Description | Required | Default | Example |
|----------|-------------|----------|---------|---------|
| `DATABASE_URL` | MariaDB connection string | Yes | (none) | `mysql://lite_mail:pass@localhost:3306/lite_mail` |
| `DATA_DIR` | Directory for MIME and attachment storage | No | `./data` | `/var/lib/lite-mail/data` |
| `PUBLIC_BASE_URL` | Public URL of the service | Yes | `http://localhost:8080` | `https://mail.example.com` |
| `MAX_MESSAGE_BYTES` | Maximum email size in bytes | No | `26214400` (25 MiB) | `26214400` |
| `SESSION_COOKIE_NAME` | Session cookie name | No | `lite_mail_session` | `lite_mail_session` |
| `SESSION_TTL_HOURS` | Session lifetime in hours | No | `24` | `24` |
| `NORMAL_USER_PSK` | Pre-shared key for normal users | Yes | (none) | `norm_abc123...` |
| `ADMIN_PSK` | Pre-shared key for admin access | Yes | (none) | `admin_xyz789...` |
| `WORKER_INGEST_PSK` | Pre-shared key for Cloudflare Worker | Yes | (none) | `work_ingest_...` |
| `INGEST_URL` | Worker secret: full URL to Go service ingest endpoint | Yes (Worker) | (none) | `https://mail.example.com/api/ingest` |
| `SERVER_ADDR` | Listen address for the server | No | `:8080` | `:8080` |
| `APP_ENV` | Application environment | No | `production` | `production` |

## Unsupported Features

lite-mail has the following limitations by design:

- **No outbound email**: lite-mail is receive-only. It does not send, reply, or forward emails.
- **No SMTP/POP3/IMAP**: lite-mail uses a Cloudflare Worker for inbound, not traditional mail protocols.
- **No labels, folders, or threading**: Messages are organized by recipient email and date only.
- **No read/unread state**: All messages are treated equally.
- **No message deletion UI**: Messages are retained for the lifetime of the mailbox. Database-level cleanup is an administrative task.
- **No multi-user accounts**: Each PSK provides access to a single mailbox (normal) or all mailboxes (admin).
- **No real-time notifications**: The UI requires a page refresh to see new messages.
- **No attachment preview**: Attachments can only be downloaded, not previewed inline.

## Troubleshooting

### Service Fails to Start

Check logs:
```bash
journalctl -u lite-mail -n 50
```

Common issues:
- Database connection failure: verify `DATABASE_URL`
- DATA_DIR permissions: ensure `lite-mail` user can write
- Port already in use: check `SERVER_ADDR` or other services

### Emails Not Received

1. Verify Cloudflare Email Routing catch-all is configured
2. Check Cloudflare Worker logs in Cloudflare dashboard
3. Verify `WORKER_INGEST_PSK` matches between Worker secrets and `.env`
4. Check that the ingest endpoint is reachable from the Worker
