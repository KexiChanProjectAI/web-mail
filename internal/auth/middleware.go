package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"lite-mail/internal/config"
)

type sessionContextKey struct{}

func RequireAuth(sessionStore SessionStore, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName(cfg))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}

			session, err := sessionStore.Get(cookie.Value)
			if err != nil {
				if !errors.Is(err, ErrSessionNotFound) {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
					return
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}

			if time.Now().After(session.ExpiresAt) {
				_ = sessionStore.Delete(session.Token)
				http.SetCookie(w, ExpiredSessionCookie(cfg))
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}

			next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), session)))
		})
	}
}

func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := SessionFromContext(r.Context())
			if session == nil || !session.IsAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SessionFromContext(ctx context.Context) *Session {
	session, _ := ctx.Value(sessionContextKey{}).(*Session)
	return session
}

func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, session)
}

func CanAccessEmail(session *Session, email string) bool {
	if session == nil {
		return false
	}
	return session.IsAdmin || session.Email == email
}
