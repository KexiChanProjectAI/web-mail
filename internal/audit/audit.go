// Package audit records who signed in, with which email, and which mailboxes
// and messages they opened, plus admin changes to access.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lite-mail/internal/auth"
	"lite-mail/internal/middleware"
)

// Event names.
const (
	SignIn             = "sign_in"
	SignInFailed       = "sign_in_failed"
	SignOut            = "sign_out"
	MailboxList        = "mailbox_list"
	MessageView        = "message_view"
	RawDownload        = "raw_download"
	AttachmentDownload = "attachment_download"
	AccessDenied       = "access_denied"
	AdminUserCreate    = "admin_user_create"
	AdminUserDelete    = "admin_user_delete"
	AdminRoleChange    = "admin_role_change"
	AdminGrantAdd      = "admin_grant_add"
	AdminGrantRemove   = "admin_grant_remove"
)

// Events lists every event name, for validation and the admin filter.
var Events = []string{
	SignIn, SignInFailed, SignOut, MailboxList, MessageView, RawDownload, AttachmentDownload,
	AccessDenied, AdminUserCreate, AdminUserDelete, AdminRoleChange, AdminGrantAdd, AdminGrantRemove,
}

const (
	maxDetail    = 2000
	maxUserAgent = 500
	// listThrottle collapses repeated inbox listings (paging, search as you
	// type) of the same mailbox scope within one session.
	listThrottle = 10 * time.Minute
	// seenTTL bounds the in-memory caches; the database is the fallback.
	seenTTL = 48 * time.Hour
)

// Event is one audit row.
type Event struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	Actor      string    `json:"actor"`
	Provider   string    `json:"provider"`
	SessionKey string    `json:"session_key"`
	Event      string    `json:"event"`
	Mailbox    string    `json:"mailbox,omitempty"`
	MessageID  int64     `json:"message_id,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	ClientIP   string    `json:"client_ip,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

// Option sets an optional field on a recorded event.
type Option func(*Event)

func WithMailbox(mailbox string) Option { return func(e *Event) { e.Mailbox = mailbox } }
func WithMessage(id int64) Option       { return func(e *Event) { e.MessageID = id } }
func WithDetail(format string, args ...any) Option {
	return func(e *Event) { e.Detail = fmt.Sprintf(format, args...) }
}

// Logger writes audit events. A nil *Logger records nothing, so handlers can
// be built without one in tests.
type Logger struct {
	db  *sql.DB
	now func() time.Time

	mu       sync.Mutex
	signedIn map[string]time.Time // session key -> first seen
	listed   map[string]time.Time // session|mailbox -> last recorded
}

func New(database *sql.DB) *Logger {
	return &Logger{db: database, now: time.Now, signedIn: map[string]time.Time{}, listed: map[string]time.Time{}}
}

var _ auth.Auditor = (*Logger)(nil)

// Record writes an event for the signed-in session. Failures are logged and
// never fail the request: the audit trail must not take mail offline.
func (l *Logger) Record(r *http.Request, s *auth.Session, event string, opts ...Option) {
	if l == nil {
		return
	}
	e := l.base(r, event)
	if s != nil {
		e.Actor, e.Provider, e.SessionKey = s.Email, s.Provider, s.SessionKey
	}
	for _, opt := range opts {
		opt(&e)
	}
	l.write(r.Context(), e)
}

// RecordList records an inbox listing at most once per session and mailbox
// scope every listThrottle.
func (l *Logger) RecordList(r *http.Request, s *auth.Session, mailbox, scope string) {
	if l == nil || s == nil {
		return
	}
	key := s.SessionKey + "|" + s.Email + "|" + mailbox
	now := l.now()
	l.mu.Lock()
	last, ok := l.listed[key]
	if ok && now.Sub(last) < listThrottle {
		l.mu.Unlock()
		return
	}
	l.listed[key] = now
	l.sweepLocked(now)
	l.mu.Unlock()
	opts := []Option{WithDetail("%s", scope)}
	if mailbox != "" {
		opts = append(opts, WithMailbox(mailbox))
	}
	l.Record(r, s, MailboxList, opts...)
}

// SignIn implements auth.Auditor. Cloudflare Access is stateless, so this is
// called on every request; it records each session key once, checking the
// database after a restart.
func (l *Logger) SignIn(r *http.Request, s *auth.Session) {
	if l == nil || s == nil {
		return
	}
	key := s.Email + "|" + s.SessionKey
	now := l.now()
	l.mu.Lock()
	if _, ok := l.signedIn[key]; ok {
		l.mu.Unlock()
		return
	}
	l.signedIn[key] = now
	l.sweepLocked(now)
	l.mu.Unlock()

	if s.SessionKey != "" && l.db != nil {
		var exists int
		err := l.db.QueryRowContext(r.Context(),
			"SELECT 1 FROM audit_events WHERE actor_email = ? AND session_key = ? AND event = ? LIMIT 1",
			s.Email, s.SessionKey, SignIn).Scan(&exists)
		if err == nil {
			return
		}
	}
	l.Record(r, s, SignIn, WithDetail("%s", signInDetail(s)))
}

// SignInFailed implements auth.Auditor.
func (l *Logger) SignInFailed(r *http.Request, provider, email, reason string) {
	if l == nil {
		return
	}
	e := l.base(r, SignInFailed)
	e.Actor, e.Provider, e.Detail = email, provider, reason
	l.write(r.Context(), e)
}

// SignOut implements auth.Auditor.
func (l *Logger) SignOut(r *http.Request, s *auth.Session) {
	l.Record(r, s, SignOut)
}

// signInDetail summarises what the session may read. Grants are resolved per
// request, so they are only known here for Cloudflare Access sign-ins; the
// first mailbox_list of a Google session carries its scope instead.
func signInDetail(s *auth.Session) string {
	switch {
	case s.IsAdmin:
		return "admin"
	case s.Provider != auth.ProviderCFAccess:
		return ""
	case len(s.Grants) == 0:
		return "no mailbox granted"
	}
	patterns := make([]string, 0, len(s.Grants))
	for _, g := range s.Grants {
		patterns = append(patterns, g.Pattern)
	}
	return "grants: " + strings.Join(patterns, ", ")
}

func (l *Logger) base(r *http.Request, event string) Event {
	return Event{
		Time:      l.now().UTC(),
		Event:     event,
		ClientIP:  ClientIP(r),
		UserAgent: truncate(r.UserAgent(), maxUserAgent),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
	}
}

func (l *Logger) write(ctx context.Context, e Event) {
	if l.db == nil {
		return
	}
	_, err := l.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO audit_events
(occurred_at, actor_email, provider, session_key, event, mailbox, message_id, detail, client_ip, user_agent, request_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Time, e.Actor, e.Provider, e.SessionKey, e.Event,
		nullString(e.Mailbox), nullInt(e.MessageID), nullString(truncate(e.Detail, maxDetail)),
		nullString(e.ClientIP), nullString(e.UserAgent), nullString(e.RequestID))
	if err != nil {
		slog.Error("audit write failed", "event", e.Event, "actor", e.Actor, "request_id", e.RequestID, "error", err.Error())
	}
}

func (l *Logger) sweepLocked(now time.Time) {
	if len(l.signedIn)+len(l.listed) < 10000 {
		return
	}
	for k, t := range l.signedIn {
		if now.Sub(t) > seenTTL {
			delete(l.signedIn, k)
		}
	}
	for k, t := range l.listed {
		if now.Sub(t) > listThrottle {
			delete(l.listed, k)
		}
	}
}

// ClientIP returns the visitor address. Behind the Cloudflare tunnel the TCP
// peer is 127.0.0.1, so Cloudflare's Cf-Connecting-Ip comes first. lite-mail
// listens on loopback only, so these headers come from the tunnel or a local
// proxy, not from the internet.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" {
		return truncate(ip, 64)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return truncate(strings.TrimSpace(first), 64)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return truncate(r.RemoteAddr, 64)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && n < len(s) && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}

func nullString(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }
func nullInt(v int64) sql.NullInt64      { return sql.NullInt64{Int64: v, Valid: v != 0} }
