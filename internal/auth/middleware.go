package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/middleware"
)

type sessionContextKey struct{}

// ErrUnauthenticated means the request carried no valid credential.
var ErrUnauthenticated = errors.New("authentication required")

// accessLoginStampInterval throttles users-row writes for stateless Cloudflare
// Access requests, which would otherwise write on every API call.
const accessLoginStampInterval = 10 * time.Minute

// Authenticator resolves the caller of a request: a server-side session cookie
// first, then a Cloudflare Access JWT, otherwise unauthenticated.
type Authenticator struct {
	sessions SessionStore
	cfg      *config.Config
	resolver *Resolver
	access   *AccessVerifier // nil when Cloudflare Access is not configured
	logins   LoginRecorder   // may be nil
	now      func() time.Time
}

func NewAuthenticator(sessions SessionStore, cfg *config.Config, resolver *Resolver, access *AccessVerifier, logins LoginRecorder) *Authenticator {
	return &Authenticator{sessions: sessions, cfg: cfg, resolver: resolver, access: access, logins: logins, now: time.Now}
}

// Authenticate returns the resolved session or ErrUnauthenticated. It may set
// a cookie-clearing header on w when it finds an expired session.
func (a *Authenticator) Authenticate(w http.ResponseWriter, r *http.Request) (*Session, error) {
	if cookie, err := r.Cookie(sessionCookieName(a.cfg)); err == nil && cookie.Value != "" {
		stored, err := a.sessions.Get(cookie.Value)
		switch {
		case err == nil && a.now().After(stored.ExpiresAt):
			_ = a.sessions.Delete(stored.Token)
			http.SetCookie(w, ExpiredSessionCookie(a.cfg))
		case err == nil:
			session, _, err := a.resolver.Resolve(r.Context(), Identity{Email: stored.Email, Provider: stored.Provider})
			if err != nil {
				return nil, err
			}
			session.Token = stored.Token
			session.ExpiresAt = stored.ExpiresAt
			return session, nil
		}
	}

	if a.access != nil {
		if raw := a.access.TokenFromRequest(r); raw != "" {
			claims, err := a.access.Verify(r.Context(), raw)
			if err != nil {
				slog.Warn("cloudflare access token rejected",
					"request_id", middleware.GetRequestIDFromContext(r.Context()),
					"error", err.Error())
				return nil, ErrUnauthenticated
			}
			session, rec, err := a.resolver.Resolve(r.Context(), Identity{Email: claims.Email, Provider: ProviderCFAccess})
			if err != nil {
				return nil, err
			}
			a.stampAccessLogin(r.Context(), claims.Email, rec)
			return session, nil
		}
	}
	return nil, ErrUnauthenticated
}

func (a *Authenticator) stampAccessLogin(ctx context.Context, email string, rec *PrincipalRecord) {
	if a.logins == nil {
		return
	}
	if rec != nil && rec.LastLoginAt != nil && a.now().Sub(*rec.LastLoginAt) < accessLoginStampInterval {
		return
	}
	if err := a.logins.RecordLogin(ctx, email, ProviderCFAccess); err != nil {
		slog.Warn("failed to record cloudflare access login", "email", email, "error", err.Error())
	}
}

// RequireAuth rejects unauthenticated requests with 401 and stores the
// resolved session in the request context.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := a.Authenticate(w, r)
		if err != nil {
			if !errors.Is(err, ErrUnauthenticated) {
				slog.Error("authentication lookup failed",
					"request_id", middleware.GetRequestIDFromContext(r.Context()),
					"error", err.Error())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), session)))
	})
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
