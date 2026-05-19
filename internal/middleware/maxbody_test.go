package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodySize(t *testing.T) {
	t.Run("allows requests under limit", func(t *testing.T) {
		handler := MaxBodySize(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
		req.ContentLength = 5
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got status %d, want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this is a long body"))
		req.ContentLength = 21
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("got status %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
		}
	})

	t.Run("returns 413 error response format", func(t *testing.T) {
		handler := MaxBodySize(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this is long"))
		req.ContentLength = 14
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
		}
		body := strings.TrimSpace(rr.Body.String())
		if body != `{"error":"request body too large"}` {
			t.Fatalf("body = %q, want %q", body, `{"error":"request body too large"}`)
		}
	})

	t.Run("uses MaxBytesReader for streaming body", func(t *testing.T) {
		handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Logf("read error: %v", err)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write(body)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789AB"))
		req.ContentLength = 12
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("got status %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
		}
	})
}
