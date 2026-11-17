package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"lite-mail/internal/config"
)

var ErrSessionNotFound = errors.New("session not found")

// Login providers recorded on sessions and users.
const (
	ProviderGoogle   = "google"
	ProviderCFAccess = "cf_access"
	ProviderAdminPSK = "admin_psk"
)

// Session represents an authenticated principal. Stored sessions carry the
// identity (Email, Provider, ExpiresAt, Token); IsAdmin, IsEnvAdmin, UserID and
// Grants are resolved from the database on every request.
type Session struct {
	Email      string
	IsAdmin    bool
	IsEnvAdmin bool
	Provider   string
	UserID     int64
	Grants     []Grant
	ExpiresAt  time.Time
	Token      string
	// SessionKey identifies one sign-in in the audit log without exposing
	// the cookie token or the Access JWT.
	SessionKey string
}

// Auditor receives sign-in events. SignIn may be called on every request of
// a stateless Cloudflare Access session; implementations de-duplicate on
// Session.SessionKey.
type Auditor interface {
	SignIn(r *http.Request, s *Session)
	SignInFailed(r *http.Request, provider, email, reason string)
	SignOut(r *http.Request, s *Session)
}

// CookieSessionKey derives the audit key of a server-side session.
func CookieSessionKey(token string) string {
	return "s-" + shortHash(token)
}

// AccessSessionKey derives the audit key of a Cloudflare Access session.
func AccessSessionKey(c *AccessClaims) string {
	return "cf-" + shortHash(c.Email+"|"+c.Subject+"|"+c.IssuedAt.UTC().Format(time.RFC3339))
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// HasMailboxAccess reports whether the session may read mail for address.
func (s *Session) HasMailboxAccess(address string) bool {
	if s == nil {
		return false
	}
	if s.IsAdmin {
		return true
	}
	for _, g := range s.Grants {
		if g.Matches(address) {
			return true
		}
	}
	return false
}

// HasAnyMailbox reports whether the session can read at least one mailbox.
func (s *Session) HasAnyMailbox() bool {
	return s != nil && (s.IsAdmin || len(s.Grants) > 0)
}

// SessionStore stores sessions by opaque token.
type SessionStore interface {
	Create(session *Session) error
	Get(token string) (*Session, error)
	Delete(token string) error
}

// CookieSessionStore is an in-memory session store. The browser cookie carries
// only the opaque token; all authorization data remains server-side.
type CookieSessionStore struct {
	cookieName string
	mu         sync.RWMutex
	sessions   map[string]*Session
}

func NewCookieSessionStore(cookieName string) *CookieSessionStore {
	if cookieName == "" {
		cookieName = "lite_mail_session"
	}
	return &CookieSessionStore{
		cookieName: cookieName,
		sessions:   make(map[string]*Session),
	}
}

func (s *CookieSessionStore) Create(session *Session) error {
	if session.Token == "" {
		token, err := GenerateToken()
		if err != nil {
			return err
		}
		session.Token = token
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for token, existing := range s.sessions {
		if now.After(existing.ExpiresAt) {
			delete(s.sessions, token)
		}
	}
	s.sessions[session.Token] = cloneSession(session)
	return nil
}

func (s *CookieSessionStore) Get(token string) (*Session, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[token]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return cloneSession(session), nil
}

func (s *CookieSessionStore) Delete(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
	return nil
}

func (s *CookieSessionStore) CookieName() string {
	return s.cookieName
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func NewSessionCookie(cfg *config.Config, token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName(cfg),
		Value:    token,
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   sessionMaxAge(cfg),
		Expires:  expiresAt,
	}
}

func ExpiredSessionCookie(cfg *config.Config) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName(cfg),
		Value:    "",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
}

func secureCookies(cfg *config.Config) bool {
	return cfg == nil || cfg.AppEnv != "development"
}

func sessionCookieName(cfg *config.Config) string {
	if cfg == nil || cfg.SessionCookieName == "" {
		return "lite_mail_session"
	}
	return cfg.SessionCookieName
}

func sessionMaxAge(cfg *config.Config) int {
	if cfg == nil || cfg.SessionTTLHours <= 0 {
		return 24 * 3600
	}
	return cfg.SessionTTLHours * 3600
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	copy := *session
	copy.Grants = append([]Grant(nil), session.Grants...)
	return &copy
}
