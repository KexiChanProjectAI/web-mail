package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRateLimit(t *testing.T) {
	t.Run("allows requests under limit", func(t *testing.T) {
		handler := RateLimit(10, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "192.168.1.1:12345"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("request %d: got status %d, want %d", i, rr.Code, http.StatusOK)
			}
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		handler := RateLimit(2, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "192.168.1.2:12345"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("request %d: got status %d, want %d", i, rr.Code, http.StatusOK)
			}
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.2:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("request over limit: got status %d, want %d", rr.Code, http.StatusTooManyRequests)
		}
	})

	t.Run("returns 429 response format", func(t *testing.T) {
		handler := RateLimit(1, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.3:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.3:12345"
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
		}
		body := strings.TrimSpace(rr.Body.String())
		if body != `{"error":"rate limit exceeded"}` {
			t.Fatalf("body = %q, want %q", body, `{"error":"rate limit exceeded"}`)
		}
	})

	t.Run("sets X-RateLimit headers", func(t *testing.T) {
		handler := RateLimit(5, 5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.4:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if got := rr.Header().Get("X-RateLimit-Limit"); got != "5" {
			t.Fatalf("X-RateLimit-Limit = %q, want %q", got, "5")
		}
		if got := rr.Header().Get("X-RateLimit-Remaining"); got == "" {
			t.Fatal("X-RateLimit-Remaining should be set")
		}
		if got := rr.Header().Get("X-RateLimit-Reset"); got == "" {
			t.Fatal("X-RateLimit-Reset should be set")
		}
	})

	t.Run("uses X-Forwarded-For header", func(t *testing.T) {
		handler := RateLimit(1, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("same X-Forwarded-For should be rate limited: got %d, want %d", rr.Code, http.StatusTooManyRequests)
		}
	})
}
