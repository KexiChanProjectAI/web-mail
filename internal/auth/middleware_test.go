package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lite-mail/internal/config"
)

func TestRequireAuthWithValidSession(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "user@example.test", ExpiresAt: mustFuture()}
	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		got := SessionFromContext(r.Context())
		if got == nil || got.Email != session.Email {
			t.Fatalf("context session = %+v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rr := runRequireAuth(cfg, store, NewSessionCookie(cfg, session.Token, session.ExpiresAt), next)
	if rr.Code != http.StatusNoContent || !called {
		t.Fatalf("status = %d called = %v", rr.Code, called)
	}
}

func TestRequireAuthWithNoCookie(t *testing.T) {
	cfg := testConfig()
	rr := runRequireAuth(cfg, NewCookieSessionStore(cfg.SessionCookieName), nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))

	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "authentication required") {
		t.Fatalf("status/body = %d %s", rr.Code, rr.Body.String())
	}
}

func TestRequireAuthWithExpiredSession(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "user@example.test", ExpiresAt: time.Now().Add(-time.Hour)}
	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	rr := runRequireAuth(cfg, store, NewSessionCookie(cfg, session.Token, session.ExpiresAt), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if _, err := store.Get(session.Token); err != ErrSessionNotFound {
		t.Fatalf("expired session still stored, err = %v", err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("clear cookie = %+v", cookies)
	}
}

func TestRequireAdminWithAdminSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithSession(req.Context(), &Session{IsAdmin: true}))
	rr := httptest.NewRecorder()

	RequireAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestRequireAdminWithNormalSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithSession(req.Context(), &Session{Email: "user@example.test"}))
	rr := httptest.NewRecorder()

	RequireAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "admin access required") {
		t.Fatalf("status/body = %d %s", rr.Code, rr.Body.String())
	}
}

func TestSessionContextHelpers(t *testing.T) {
	if got := SessionFromContext(context.Background()); got != nil {
		t.Fatalf("SessionFromContext(empty) = %+v, want nil", got)
	}
	session := &Session{Email: "user@example.test"}
	ctx := WithSession(context.Background(), session)
	if got := SessionFromContext(ctx); got != session {
		t.Fatalf("SessionFromContext() = %+v, want %+v", got, session)
	}
}

func TestCanAccessEmail(t *testing.T) {
	if !CanAccessEmail(&Session{IsAdmin: true}, "any@example.test") {
		t.Fatal("admin should access any email")
	}
	if !CanAccessEmail(&Session{Email: "user@example.test"}, "user@example.test") {
		t.Fatal("normal user should access own email")
	}
	if CanAccessEmail(&Session{Email: "user@example.test"}, "other@example.test") {
		t.Fatal("normal user should not access another email")
	}
}

func runRequireAuth(cfg *config.Config, store SessionStore, cookie *http.Cookie, next http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	RequireAuth(store, cfg)(next).ServeHTTP(rr, req)
	return rr
}

func mustFuture() time.Time {
	return time.Now().Add(time.Hour)
}
