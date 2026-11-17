package middleware

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// RequireSameOrigin rejects state-changing requests that do not come from the
// application's own origin. Safe methods pass. Otherwise the browser's
// Sec-Fetch-Site must say same-origin (or none, for a typed URL); when that
// header is absent, Origin must equal the origin of publicBaseURL(). Requests
// with neither header are refused. Bodies must be application/json, which a
// cross-site form cannot send without a CORS preflight.
//
// publicBaseURL is read on every request because tests set the base URL after
// the router is built.
func RequireSameOrigin(publicBaseURL func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if !sameOrigin(r, publicBaseURL()) {
				writeForbidden(w, "cross-site request refused")
				return
			}
			if r.ContentLength != 0 {
				mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if mediaType != "application/json" {
					writeForbidden(w, "content type must be application/json")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameOrigin(r *http.Request, publicBaseURL string) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	return originOf(origin) != "" && originOf(origin) == originOf(publicBaseURL)
}

func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
