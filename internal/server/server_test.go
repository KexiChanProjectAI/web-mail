package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"lite-mail/internal/config"
)

func TestHealthEndpoint(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got, want := rr.Body.String(), `{"status":"ok"}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestCORSRestrictsOrigins(t *testing.T) {
	allowedOrigin := "https://mail.example.test"
	cfg := &config.Config{PublicBaseURL: allowedOrigin}
	s := New(cfg, nil)

	t.Run("allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", allowedOrigin)
		rr := httptest.NewRecorder()

		s.Handler().ServeHTTP(rr, req)

		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://evil.example.test")
		rr := httptest.NewRecorder()

		s.Handler().ServeHTTP(rr, req)

		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})
}

func TestAuthRoutesWiring(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://mail.example.test", AdminPSK: "x"}
	s := New(cfg, nil)

	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	if rr := get("/api/auth/providers"); rr.Code != http.StatusOK || rr.Body.String() != "{\"admin_psk\":true,\"cf_access\":false,\"google\":false}\n" {
		t.Fatalf("providers = %d %q", rr.Code, rr.Body.String())
	}
	if rr := get("/api/me"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("/api/me unauthenticated = %d", rr.Code)
	}
	if rr := get("/api/admin/users"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("/api/admin/users unauthenticated = %d", rr.Code)
	}
	if rr := get("/auth/google/start"); rr.Code != http.StatusNotFound {
		t.Fatalf("google start without config = %d, want 404", rr.Code)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-site login = %d, want 403", rr.Code)
	}
}

func TestGoogleRoutesMountedWhenConfigured(t *testing.T) {
	cfg := &config.Config{PublicBaseURL: "https://mail.example.test", GoogleClientID: "id", GoogleClientSecret: "secret"}
	s := New(cfg, nil, WithGoogleIssuer("http://127.0.0.1:1", nil))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	if rr.Code != http.StatusFound || rr.Header().Get("Location") != "/login?error=oauth_failed" {
		t.Fatalf("google start with unreachable issuer = %d %q", rr.Code, rr.Header().Get("Location"))
	}
}
