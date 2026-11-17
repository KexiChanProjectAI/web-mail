package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/testutil/jwttest"
)

// fakePrincipals is an in-memory PrincipalSource and LoginRecorder.
type fakePrincipals struct {
	records map[string]*PrincipalRecord
	logins  []string
	err     error
}

func (f *fakePrincipals) LookupPrincipal(_ context.Context, email string) (*PrincipalRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records[email], nil
}

func (f *fakePrincipals) RecordLogin(_ context.Context, email, provider string) error {
	f.logins = append(f.logins, email+"|"+provider)
	return nil
}

func newAuthenticator(cfg *config.Config, store SessionStore, users *fakePrincipals, access *AccessVerifier) *Authenticator {
	return NewAuthenticator(store, cfg, NewResolver(users, cfg), access, users)
}

func TestRequireAuthWithValidSessionResolvesGrants(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "user@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	users := &fakePrincipals{records: map[string]*PrincipalRecord{
		"user@example.test": {UserID: 7, Patterns: []string{"box@example.test", "*@example.org"}},
	}}

	var got *Session
	rr := runRequireAuth(newAuthenticator(cfg, store, users, nil), func(r *http.Request) {
		r.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = SessionFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	if rr.Code != http.StatusNoContent || got == nil {
		t.Fatalf("status = %d session = %+v", rr.Code, got)
	}
	if got.UserID != 7 || got.IsAdmin || len(got.Grants) != 2 || !got.HasMailboxAccess("x@example.org") || got.HasMailboxAccess("x@example.com") {
		t.Fatalf("resolved session = %+v", got)
	}
}

func TestRequireAuthEnvAdminMerged(t *testing.T) {
	cfg := testConfig()
	cfg.AdminEmails = []string{"boss@example.test"}
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "boss@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
	_ = store.Create(session)

	var got *Session
	runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, nil), func(r *http.Request) {
		r.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = SessionFromContext(r.Context()) }))
	if got == nil || !got.IsAdmin || !got.IsEnvAdmin {
		t.Fatalf("env admin not merged: %+v", got)
	}
}

func TestRequireAuthUserWithoutRowHasNoAccess(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "stranger@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
	_ = store.Create(session)

	var got *Session
	runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, nil), func(r *http.Request) {
		r.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = SessionFromContext(r.Context()) }))
	if got == nil || got.HasAnyMailbox() || got.HasMailboxAccess("stranger@example.test") {
		t.Fatalf("stranger must not get implicit access: %+v", got)
	}
}

func TestRequireAuthWithNoCredentials(t *testing.T) {
	cfg := testConfig()
	rr := runRequireAuth(newAuthenticator(cfg, NewCookieSessionStore(cfg.SessionCookieName), &fakePrincipals{}, nil), nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next should not be called") }))
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

	rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, nil), func(r *http.Request) {
		r.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next should not be called") }))

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

func TestRequireAuthLookupErrorIs500(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	session := &Session{Email: "user@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
	_ = store.Create(session)
	rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{err: errors.New("db down")}, nil), func(r *http.Request) {
		r.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next should not be called") }))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

func TestRequireAuthCloudflareAccess(t *testing.T) {
	jwks := jwttest.NewFakeJWKS(t)
	issuer, aud := "https://team.cloudflareaccess.com", "app-aud"
	access := NewAccessVerifierWithJWKS(issuer, jwks.JWKSURL(), aud, nil)
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)

	t.Run("valid header token, login recorded", func(t *testing.T) {
		users := &fakePrincipals{records: map[string]*PrincipalRecord{"user@example.test": {UserID: 3, Patterns: []string{"user@example.test"}}}}
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "User@Example.test"))
		var got *Session
		rr := runRequireAuth(newAuthenticator(cfg, store, users, access), func(r *http.Request) {
			r.Header.Set(CFAccessHeader, token)
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = SessionFromContext(r.Context()) }))
		if rr.Code != http.StatusOK || got == nil || got.Email != "user@example.test" || got.Provider != ProviderCFAccess || got.UserID != 3 {
			t.Fatalf("status = %d session = %+v", rr.Code, got)
		}
		if len(users.logins) != 1 || users.logins[0] != "user@example.test|cf_access" {
			t.Fatalf("logins = %v", users.logins)
		}
	})

	t.Run("recent login is not re-stamped", func(t *testing.T) {
		recent := time.Now().Add(-time.Minute)
		users := &fakePrincipals{records: map[string]*PrincipalRecord{"user@example.test": {UserID: 3, LastLoginAt: &recent}}}
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "user@example.test"))
		runRequireAuth(newAuthenticator(cfg, store, users, access), func(r *http.Request) { r.Header.Set(CFAccessHeader, token) },
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		if len(users.logins) != 0 {
			t.Fatalf("logins = %v, want none", users.logins)
		}
	})

	t.Run("cookie fallback", func(t *testing.T) {
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "user@example.test"))
		rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, access), func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: CFAccessCookie, Value: token})
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
	})

	t.Run("expired cookie session falls through to access token", func(t *testing.T) {
		old := &Session{Email: "old@example.test", ExpiresAt: time.Now().Add(-time.Hour)}
		_ = store.Create(old)
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "user@example.test"))
		var got *Session
		rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, access), func(r *http.Request) {
			r.AddCookie(NewSessionCookie(cfg, old.Token, old.ExpiresAt))
			r.Header.Set(CFAccessHeader, token)
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = SessionFromContext(r.Context()) }))
		if rr.Code != http.StatusOK || got == nil || got.Email != "user@example.test" {
			t.Fatalf("status = %d session = %+v", rr.Code, got)
		}
	})

	t.Run("valid cookie session wins over access token", func(t *testing.T) {
		live := &Session{Email: "cookie@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
		_ = store.Create(live)
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "user@example.test"))
		var got *Session
		runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, access), func(r *http.Request) {
			r.AddCookie(NewSessionCookie(cfg, live.Token, live.ExpiresAt))
			r.Header.Set(CFAccessHeader, token)
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = SessionFromContext(r.Context()) }))
		if got == nil || got.Email != "cookie@example.test" {
			t.Fatalf("session = %+v", got)
		}
	})

	t.Run("header ignored when access is not configured", func(t *testing.T) {
		token := jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "user@example.test"))
		rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, nil), func(r *http.Request) {
			r.Header.Set(CFAccessHeader, token)
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next should not be called") }))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rr.Code)
		}
	})

	t.Run("bad token is 401", func(t *testing.T) {
		claims := jwttest.AccessClaims(issuer, "other-aud", "user@example.test")
		rr := runRequireAuth(newAuthenticator(cfg, store, &fakePrincipals{}, access), func(r *http.Request) {
			r.Header.Set(CFAccessHeader, jwks.Mint(t, claims))
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next should not be called") }))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rr.Code)
		}
	})
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

func runRequireAuth(a *Authenticator, prepare func(*http.Request), next http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if prepare != nil {
		prepare(req)
	}
	rr := httptest.NewRecorder()
	a.RequireAuth(next).ServeHTTP(rr, req)
	return rr
}

func mustFuture() time.Time {
	return time.Now().Add(time.Hour)
}
