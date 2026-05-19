package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID(t *testing.T) {
	handler := RequestID("X-Request-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("generates request ID when not present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		got := rr.Header().Get("X-Request-ID")
		if got == "" {
			t.Fatal("X-Request-ID should be set")
		}
		if len(got) != 36 {
			t.Fatalf("X-Request-ID should be UUID format, got %q", got)
		}
	})

	t.Run("preserves incoming request ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "existing-id-123")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		got := rr.Header().Get("X-Request-ID")
		if got != "existing-id-123" {
			t.Fatalf("X-Request-ID = %q, want %q", got, "existing-id-123")
		}
	})
}

func TestGetRequestIDFromContext(t *testing.T) {
	t.Run("returns empty string when not set", func(t *testing.T) {
		ctx := context.Background()
		if got := GetRequestIDFromContext(ctx); got != "" {
			t.Fatalf("GetRequestIDFromContext = %q, want empty string", got)
		}
	})

	t.Run("returns request ID from context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), contextKeyRequestID{}, "test-request-id")
		if got := GetRequestIDFromContext(ctx); got != "test-request-id" {
			t.Fatalf("GetRequestIDFromContext = %q, want %q", got, "test-request-id")
		}
	})
}
