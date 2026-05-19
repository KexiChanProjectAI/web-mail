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

	rr := performLogin(handler, `{"psk":"admin-secret","email":"ignored@example.test"}`)
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

func TestLoginValidNormalPSKWithEmail(t *testing.T) {
	cfg := testConfig()
	store := NewCookieSessionStore(cfg.SessionCookieName)
	handler := NewAuthHandler(store, cfg)

	rr := performLogin(handler, `{"psk":"normal-secret","email":"user@example.test"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var body loginResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.IsAdmin || body.Email != "user@example.test" {
		t.Fatalf("response = %+v", body)
	}

	cookie := rr.Result().Cookies()[0]
	session, err := store.Get(cookie.Value)
	if err != nil {
		t.Fatalf("stored session missing: %v", err)
	}
	if session.IsAdmin || session.Email != "user@example.test" {
		t.Fatalf("session = %+v", session)
	}
}

func TestLoginInvalidPSK(t *testing.T) {
	handler := NewAuthHandler(NewCookieSessionStore("test_session"), testConfig())

	rr := performLogin(handler, `{"psk":"wrong","email":"user@example.test"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rr.Body.String(), "invalid credentials") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestLoginNormalPSKNoEmail(t *testing.T) {
	handler := NewAuthHandler(NewCookieSessionStore("test_session"), testConfig())

	rr := performLogin(handler, `{"psk":"normal-secret"}`)
	// Returns generic "invalid credentials" to not reveal whether PSK was correct
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rr.Body.String(), "invalid credentials") {
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
		NormalUserPSK:     "normal-secret",
		AdminPSK:          "admin-secret",
		AppEnv:            "development",
	}
}
