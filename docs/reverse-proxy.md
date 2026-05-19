# Reverse Proxy Configuration

lite-mail runs on `localhost:8080` by default and needs a reverse proxy in front for TLS termination. Both Caddy and nginx are supported.

## Caddy Configuration (Recommended)

Caddy handles TLS automatically and has a simple configuration format.

### Caddyfile

```caddy
mail.example.com {
    reverse_proxy localhost:8080

    # Optional: additional headers if needed
    header X-Real-IP {remote_host}
}
```

### Key Points

- Caddy automatically obtains and renews TLS certificates via Let's Encrypt
- The `reverse_proxy` directive passes all requests to the Go service
- No need to configure WebSocket support (this application does not use WebSockets)
- The Go service sets its own security headers (CSP, X-Frame-Options, etc.)

### Install Caddy

```bash
# Debian/Ubuntu
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list > /dev/null
apt update
apt install caddy
```

## nginx Configuration

### Server Block

```nginx
server {
    listen 80;
    server_name mail.example.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name mail.example.com;

    # TLS Configuration
    ssl_certificate /etc/letsencrypt/live/mail.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mail.example.com/privkey.pem;

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;

    # Client max body size: must accommodate email attachments up to MAX_MESSAGE_BYTES (default 25 MiB)
    client_max_body_size 30m;

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeouts for email-sized request bodies
        proxy_connect_timeout 60s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;
    }
}
```

### Obtain TLS Certificate

```bash
# Using certbot with nginx plugin
certbot --nginx -d mail.example.com

# Or use standalone mode if nginx is running
certbot certonly --standalone -d mail.example.com
```

## Security Notes

### Headers

The Go service sets its own security headers:
- `Content-Security-Policy`
- `X-Frame-Options: DENY`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`

Your reverse proxy can add additional headers if needed, but this is optional.

### TLS Recommendations

- Use TLS 1.2 or higher only
- Prefer modern cipher suites (see nginx config above)
- HSTS header can be added at the reverse proxy level if desired

### Network Considerations

The Go service listens on `localhost:8080` by default. It should only be reachable via the reverse proxy, not directly from the internet. Ensure your firewall allows port 443 (and optionally 80 for ACME challenges) but blocks direct access to port 8080 from external IPs.
