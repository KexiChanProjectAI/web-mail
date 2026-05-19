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
