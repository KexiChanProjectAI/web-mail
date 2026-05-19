package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"lite-mail/internal/config"
)

var ErrSessionNotFound = errors.New("session not found")

// Session represents a server-side authenticated browser session.
type Session struct {
	Email     string
	IsAdmin   bool
	ExpiresAt time.Time
	Token     string
}

// SessionStore stores sessions by opaque token.
type SessionStore interface {
	Create(session *Session) error
	Get(token string) (*Session, error)
	Delete(token string) error
}

// CookieSessionStore is an in-memory session store for the MVP. The browser
// cookie carries only the opaque token; all authorization data remains server-side.
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
		Secure:   cfg == nil || cfg.AppEnv != "development",
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
		Secure:   cfg == nil || cfg.AppEnv != "development",
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
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
	return &copy
}
