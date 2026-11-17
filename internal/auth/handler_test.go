package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lite-mail/internal/config"
)

func TestLoginValidAdminPSK(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	handler := NewAuthHandler(store, cfg)

	rr := performLogin(handler, `{"psk":"admin-secret"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var body loginResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.IsAdmin || body.Email != "admin" || body.Status != "ok" {
		t.Fatalf("response = %+v", body)
	}

	cookie := rr.Result().Cookies()[0]
	session, err := store.Get(cookie.Value)
	if err != nil {
		t.Fatalf("stored session missing: %v", err)
	}
	if !session.IsAdmin || session.Email != "admin" {
		t.Fatalf("session = %+v", session)
	}
}

func TestLoginInvalidPSK(t *testing.T) {
	handler := NewAuthHandler(NewCookieSessionStore("test_session"), testConfig())

	rr := performLogin(handler, `{"psk":"wrong"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rr.Body.String(), "invalid credentials") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestLoginDisabledWithoutAdminPSK(t *testing.T) {
	cfg := testConfig()
	cfg.AdminPSK = ""
	handler := NewAuthHandler(NewCookieSessionStore("test_session"), cfg)

	rr := performLogin(handler, `{"psk":""}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestLoginReplacesIncomingSession(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	planted := &Session{Email: "victim@example.test", Provider: ProviderGoogle, ExpiresAt: mustFuture()}
	if err := store.Create(planted); err != nil {
		t.Fatal(err)
	}
	handler := NewAuthHandler(store, cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"psk":"admin-secret"}`))
	req.AddCookie(NewSessionCookie(cfg, planted.Token, planted.ExpiresAt))
	rr := httptest.NewRecorder()
	handler.LoginHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if _, err := store.Get(planted.Token); err != ErrSessionNotFound {
		t.Fatalf("incoming session survived login: %v", err)
	}
	if got := rr.Result().Cookies()[0].Value; got == planted.Token || got == "" {
		t.Fatalf("new session token = %q", got)
	}
}

func TestProvidersHandler(t *testing.T) {
	cfg := testConfig()
	cfg.GoogleClientID, cfg.GoogleClientSecret = "id", "secret"
	handler := NewAuthHandler(NewCookieSessionStore("s"), cfg)
	rr := httptest.NewRecorder()
	handler.ProvidersHandler(rr, httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil))
	var got map[string]bool
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got["google"] || got["cf_access"] || !got["admin_psk"] {
		t.Fatalf("providers = %v", got)
	}
}

func TestMeHandler(t *testing.T) {
	handler := NewAuthHandler(NewCookieSessionStore("s"), testConfig())
	session := &Session{Email: "user@example.test", Provider: ProviderCFAccess, Grants: ParseGrants([]string{"a@example.test", "*@example.org"})}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req = req.WithContext(WithSession(req.Context(), session))
	rr := httptest.NewRecorder()
	handler.MeHandler(rr, req)
	var got meResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Email != "user@example.test" || got.IsAdmin || got.Provider != ProviderCFAccess || len(got.Grants) != 2 ||
		got.Grants[0].Kind != "exact" || got.Grants[1].Pattern != "*@example.org" || got.Grants[1].Kind != "domain" {
		t.Fatalf("me = %+v", got)
	}
}

func TestLogoutAdvertisesAccessLogout(t *testing.T) {
	cfg := testConfig()
	cfg.CFAccessTeamDomain, cfg.CFAccessAUD = "team", "aud"
	handler := NewAuthHandler(NewCookieSessionStore("s"), cfg)
	rr := httptest.NewRecorder()
	handler.LogoutHandler(rr, httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	if !strings.Contains(rr.Body.String(), AccessLogoutPath) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestLogoutClearsSessionAndCookie(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	handler := NewAuthHandler(store, cfg)
	session := &Session{Email: "user@example.test", ExpiresAt: mustFuture()}
	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	rr := httptest.NewRecorder()

	handler.LogoutHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if _, err := store.Get(session.Token); err != ErrSessionNotFound {
		t.Fatalf("Get() err = %v, want ErrSessionNotFound", err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Fatalf("clear cookie = %+v", cookies)
	}
}

func performLogin(handler *AuthHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.LoginHandler(rr, req)
	return rr
}

func testConfig() *config.Config {
	return &config.Config{
		SessionCookieName: "test_session",
		SessionTTLHours:   2,
		AdminPSK:          "admin-secret",
		AppEnv:            "development",
	}
}
