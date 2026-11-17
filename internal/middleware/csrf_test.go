package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireSameOrigin(t *testing.T) {
	base := "https://mail.example.test"
	h := RequireSameOrigin(func() string { return base })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		name    string
		method  string
		headers map[string]string
		body    string
		want    int
	}{
		{"get passes", http.MethodGet, nil, "", 204},
		{"same-origin fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/json"}, "{}", 204},
		{"cross-site fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "application/json"}, "{}", 403},
		{"same-site subdomain", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site", "Content-Type": "application/json"}, "{}", 403},
		{"matching origin", http.MethodDelete, map[string]string{"Origin": base}, "", 204},
		{"foreign origin", http.MethodDelete, map[string]string{"Origin": "https://evil.test"}, "", 403},
		{"no headers", http.MethodPost, nil, "", 403},
		{"form body refused", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/x-www-form-urlencoded"}, "a=b", 403},
		{"json with charset", http.MethodPatch, map[string]string{"Origin": base, "Content-Type": "application/json; charset=utf-8"}, "{}", 204},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/admin/users", strings.NewReader(tc.body))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}
