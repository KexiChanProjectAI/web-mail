# Authentication and Mailbox Access

lite-mail signs people in with a verified identity and lets administrators decide which mailboxes each identity may read.
Nothing is granted implicitly: an account that signs in without any grant sees an empty "No mailbox assigned" page, even if mail arrives for its own address.

## Sign-in methods

| Method | Enabled | Configuration |
|---|---|---|
| Cloudflare Access | By default | `CF_ACCESS_TEAM_DOMAIN`, `CF_ACCESS_AUD` |
| Google | Only when `AUTH_PROVIDERS` lists `google` | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, optional `GOOGLE_ALLOWED_DOMAINS` |
| Emergency admin key | When `ADMIN_PSK` is set | `ADMIN_PSK` (empty disables it) |

`AUTH_PROVIDERS` defaults to `cf_access`, so Cloudflare Zero Trust is the only sign-in method unless you opt in to Google with `AUTH_PROVIDERS=cf_access,google`.
Google credentials alone do not enable it.
At least one usable method must be configured, or the service refuses to start.

### Google (opt-in)

Set `AUTH_PROVIDERS=cf_access,google` (or `google` alone), then:

1. In the Google Cloud console, open **APIs & Services → OAuth consent screen** and configure it (an Internal app if everyone is in your Workspace).
2. Open **Credentials → Create credentials → OAuth client ID**, type **Web application**.
3. Add the authorized redirect URI `${PUBLIC_BASE_URL}/auth/google/callback`, for example `https://webmail.example.com/auth/google/callback`.
4. Put the client ID and secret in `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`.

`PUBLIC_BASE_URL` must be an `https://` URL when Google is enabled, unless `APP_ENV=development`.
The flow uses state, nonce and PKCE, and accepts only Google accounts whose email is verified.
`GOOGLE_ALLOWED_DOMAINS` (comma-separated) restricts sign-in to those email domains; it is checked against the email address, so personal Gmail accounts are refused when it is set.
Leaving it empty lets any verified Google account sign in, which is safe because such an account still needs a grant to read anything.

### Cloudflare Access

When Cloudflare Access protects the hostname, Cloudflare signs every request it forwards with a JWT in the `Cf-Access-Jwt-Assertion` header and the `CF_Authorization` cookie.
lite-mail verifies that token on every request: RS256 signature against the team keys at `https://<team>.cloudflareaccess.com/cdn-cgi/access/certs`, issuer, audience and expiry.
No lite-mail session is created; signing out goes through `/cdn-cgi/access/logout`.

1. In Zero Trust, open **Access → Applications → Add an application → Self-hosted** and enter the lite-mail hostname.
2. Add the policy that decides who may reach the site at all, for example your identity provider's group.
3. Copy the **Application Audience (AUD) Tag** into `CF_ACCESS_AUD`.
4. Put the team name (`myteam`) or full team domain (`myteam.cloudflareaccess.com`) into `CF_ACCESS_TEAM_DOMAIN`.
5. **Exclude the ingest endpoint.**
   Add a second Access application for the paths `/api/ingest` and `/healthz` on the same hostname with a **Bypass** policy for Everyone.
   Without it, Cloudflare redirects the Email Worker's POST to the Access login page and inbound mail stops arriving.
   The ingest endpoint stays protected by `WORKER_INGEST_PSK`.

Tokens without an email claim, such as service-token tokens, are treated as signed out.
A forged header fails signature verification, but the origin should still accept traffic only from Cloudflare (a Tunnel or a firewall limited to Cloudflare IPs), so nothing bypasses the Access policy.

### Emergency admin key

`POST /api/login` with `ADMIN_PSK` creates an administrator session named `admin`.
It is meant for recovery, and every use is logged at warning level.
Keep it set during the first rollout, then clear it once Google or Cloudflare Access sign-in is confirmed working.

## Administrators

- Every address in `ADMIN_EMAILS` (comma-separated) is always an administrator and cannot be demoted from the UI.
- Administrators can promote or demote other users on the **Manage access** page.
- Administrators read every mailbox.

Set `ADMIN_EMAILS` before the first deploy, or only the emergency key can reach the admin page.

## Mailbox grants

A grant lets one user read mail delivered to:

- one address, written `alice@example.com`, or
- every address of a domain, written `*@example.com` (subdomains are not included).

A message is visible when one of its To, Cc or Bcc recipients matches a grant.
Only a message with no To, Cc or Bcc recipient is matched by its envelope recipient instead, the address Cloudflare Email Routing delivered it to.
Forwarded mail therefore stays with its original recipient: a Microsoft 365 tenant that forwards `yzs@tenant.onmicrosoft.com` to `catch-all@example.com` keeps `To: yzs@tenant.onmicrosoft.com`, and a grant on that address reads it, while a grant on `catch-all@example.com` does not.
The message page shows the envelope recipient as **Delivered to**.

Mail stored before envelope recipients were recorded is filled in once with `lite-mail backfill-envelope` (add `--dry-run` to only report).
It reads the address from the Cloudflare `Received` header of each stored raw message, runs with the same environment as the service, and can be repeated safely.
Grants and admin changes take effect on the user's next request; nobody has to sign in again.

People appear on the **Manage access** page after their first sign-in, with no access.
Administrators can also add an email ahead of time and grant it mailboxes before that person signs in.
The grant field suggests addresses and domains that have already received mail.

## Audit log

Administrators see an **Audit log** tab next to **Users and grants**.
It answers who signed in, with which email, when, from which IP, and which mailboxes and messages they opened.

| Event | Recorded when |
|---|---|
| Signed in | A new sign-in starts: a new Cloudflare Access session, a Google login, or the emergency key |
| Sign-in rejected | An Access token fails verification, a Google login fails, or a wrong emergency key is sent |
| Signed out | The user presses Sign out |
| Opened inbox | The inbox is listed, at most once per mailbox filter per sign-in every 10 minutes |
| Read message, Fetched raw source, Downloaded attachment | A message, its raw source or an attachment is served; one row per granted recipient mailbox it was reached through |
| Access denied | Someone asks for a message or a mailbox filter outside their grants |
| Added user, Removed user, Changed admin rights, Granted mailbox, Removed mailbox grant | An administrator changes access |

Every row carries the login email, the sign-in method, the client IP and a session key that groups all events of one sign-in; **this sign-in** filters to them.
The message page loads the raw source for its Source tab, so opening a message also records **Fetched raw source**.

The client IP comes from Cloudflare's `Cf-Connecting-Ip` header, then `X-Forwarded-For`, then the TCP peer.
lite-mail listens on loopback behind the tunnel, so those headers come from Cloudflare or your own proxy; do not expose the listener directly.

Events are kept for `AUDIT_RETENTION_DAYS` (default 365, `0` keeps them forever) and pruned hourly.
A failed audit write is logged and does not fail the request, so the audit trail cannot take mail offline.
Admins can also query `GET /api/admin/audit` with `actor`, `mailbox`, `event`, `session`, `since`, `until` (RFC 3339 or `YYYY-MM-DD`, UTC), `before` (cursor) and `limit`.

## HTTP endpoints

| Method and path | Purpose |
|---|---|
| `GET /api/auth/providers` | Which sign-in methods are configured |
| `GET /auth/google/start`, `GET /auth/google/callback` | Google sign-in |
| `POST /api/login` | Emergency admin key |
| `POST /api/logout` | Ends the lite-mail session |
| `GET /api/me` | Signed-in identity, admin flag and grants |
| `/api/admin/users`, `/api/admin/users/{id}`, `/api/admin/users/{id}/grants[/{grantId}]`, `/api/admin/mailboxes` | User, admin and grant management (administrators only) |
| `GET /api/admin/audit` | Audit log (administrators only) |

State-changing endpoints accept only same-origin JSON requests, checked through `Sec-Fetch-Site` or `Origin`.
