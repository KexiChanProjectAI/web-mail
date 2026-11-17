package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/testutil/jwttest"
)

// fakeGoogle is a minimal OIDC provider: discovery, JWKS and a token endpoint
// that returns an id_token built from the next claims.
type fakeGoogle struct {
	jwks *jwttest.FakeJWKS

	mu       sync.Mutex
	claims   func(nonce string) map[string]any
	nonce    string
	verifier string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{jwks: jwttest.NewFakeJWKS(t)}
	issuer := f.jwks.Server.URL
	f.jwks.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                issuer,
				"authorization_endpoint":                issuer + "/auth",
				"token_endpoint":                        issuer + "/token",
				"jwks_uri":                              issuer + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/token":
			_ = r.ParseForm()
			f.mu.Lock()
			f.verifier = r.PostForm.Get("code_verifier")
			claims := f.claims(f.nonce)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
				"id_token": f.jwks.Mint(t, claims),
			})
		default:
			http.NotFound(w, r)
		}
	})
	return f
}

func (f *fakeGoogle) idClaims(email string, verified bool) func(string) map[string]any {
	return func(nonce string) map[string]any {
		now := time.Now()
		return map[string]any{
			"iss": f.jwks.Server.URL, "aud": "client-id", "sub": "123", "email": email,
			"email_verified": verified, "nonce": nonce, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		}
	}
}

type googleRig struct {
	fake    *fakeGoogle
	handler *GoogleHandler
	store   *CookieSessionStore
	users   *fakePrincipals
	cfg     *config.Config
}

func newGoogleRig(t *testing.T) *googleRig {
	fake := newFakeGoogle(t)
	cfg := testConfig()
	cfg.GoogleClientID, cfg.GoogleClientSecret = "client-id", "secret"
	cfg.PublicBaseURL = "http://localhost:8080"
	store := NewCookieSessionStore(cfg.SessionCookieName)
	users := &fakePrincipals{}
	return &googleRig{fake: fake, cfg: cfg, store: store, users: users,
		handler: NewGoogleHandler(cfg, store, users, fake.jwks.Server.URL, nil)}
}

// start runs Start and returns the oauth cookie and the authorize URL query.
func (rig *googleRig) start(t *testing.T) (*http.Cookie, url.Values) {
	t.Helper()
	rr := httptest.NewRecorder()
	rig.handler.Start(rr, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	if rr.Code != http.StatusFound {
		t.Fatalf("start status = %d body=%s", rr.Code, rr.Body.String())
	}
	loc, _ := url.Parse(rr.Header().Get("Location"))
	q := loc.Query()
	rig.fake.mu.Lock()
	rig.fake.nonce = q.Get("nonce")
	rig.fake.mu.Unlock()
	var cookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == oauthCookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != q.Get("state") || !cookie.HttpOnly {
		t.Fatalf("oauth cookie = %+v, state = %q", cookie, q.Get("state"))
	}
	return cookie, q
}

func (rig *googleRig) callback(cookie *http.Cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?"+query, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	rig.handler.Callback(rr, req)
	return rr
}

func sessionCookieFrom(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestGoogleLoginHappyPath(t *testing.T) {
	rig := newGoogleRig(t)
	rig.fake.claims = rig.fake.idClaims("Person@Example.com", true)
	cookie, q := rig.start(t)
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("prompt") != "select_account" ||
		q.Get("redirect_uri") != "http://localhost:8080/auth/google/callback" || !strings.Contains(q.Get("scope"), "email") {
		t.Fatalf("authorize query = %v", q)
	}

	rr := rig.callback(cookie, "code=abc&state="+url.QueryEscape(q.Get("state")))
	if rr.Code != http.StatusFound || rr.Header().Get("Location") != "/" {
		t.Fatalf("callback status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	if rig.fake.verifier == "" {
		t.Fatal("PKCE verifier not sent to token endpoint")
	}
	sc := sessionCookieFrom(rr, rig.cfg.SessionCookieName)
	if sc == nil {
		t.Fatal("no session cookie")
	}
	session, err := rig.store.Get(sc.Value)
	if err != nil || session.Email != "person@example.com" || session.Provider != ProviderGoogle || session.IsAdmin {
		t.Fatalf("session = %+v err = %v", session, err)
	}
	if len(rig.users.logins) != 1 || rig.users.logins[0] != "person@example.com|google" {
		t.Fatalf("logins = %v", rig.users.logins)
	}

	// The state is single-use.
	if rr := rig.callback(cookie, "code=abc&state="+url.QueryEscape(q.Get("state"))); rr.Header().Get("Location") != "/login?error=oauth_failed" {
		t.Fatalf("replayed state: location = %q", rr.Header().Get("Location"))
	}
}

func TestGoogleLoginFailures(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(rig *googleRig)
		query    func(state string) string
		noCookie bool
		want     string
	}{
		{name: "provider error", query: func(string) string { return "error=access_denied" }, want: oauthErrDenied},
		{name: "state mismatch", query: func(string) string { return "code=abc&state=forged" }, want: oauthErrFailed},
		{name: "missing state cookie", noCookie: true, query: func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, want: oauthErrFailed},
		{name: "unverified email", setup: func(rig *googleRig) { rig.fake.claims = rig.fake.idClaims("p@example.com", false) },
			query: func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, want: oauthErrEmailUnverified},
		{name: "domain not allowed", setup: func(rig *googleRig) {
			rig.cfg.GoogleAllowedDomains = []string{"corp.example"}
			rig.fake.claims = rig.fake.idClaims("p@gmail.com", true)
		}, query: func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, want: oauthErrDomainNotAllowed},
		{name: "nonce mismatch", setup: func(rig *googleRig) {
			good := rig.fake.idClaims("p@example.com", true)
			rig.fake.claims = func(string) map[string]any { return good("other-nonce") }
		}, query: func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, want: oauthErrFailed},
		{name: "wrong audience", setup: func(rig *googleRig) {
			good := rig.fake.idClaims("p@example.com", true)
			rig.fake.claims = func(n string) map[string]any { c := good(n); c["aud"] = "someone-else"; return c }
		}, query: func(s string) string { return "code=abc&state=" + url.QueryEscape(s) }, want: oauthErrFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newGoogleRig(t)
			rig.fake.claims = rig.fake.idClaims("p@example.com", true)
			if tc.setup != nil {
				tc.setup(rig)
			}
			cookie, q := rig.start(t)
			if tc.noCookie {
				cookie = nil
			}
			rr := rig.callback(cookie, tc.query(q.Get("state")))
			if got := rr.Header().Get("Location"); got != "/login?error="+tc.want {
				t.Fatalf("location = %q, want error %s", got, tc.want)
			}
			if sessionCookieFrom(rr, rig.cfg.SessionCookieName) != nil {
				t.Fatal("session cookie set on failure")
			}
		})
	}
}

func TestGoogleStartDiscoveryFailure(t *testing.T) {
	cfg := testConfig()
	cfg.GoogleClientID, cfg.GoogleClientSecret = "id", "secret"
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	h := NewGoogleHandler(cfg, NewCookieSessionStore("s"), nil, dead.URL, nil)
	rr := httptest.NewRecorder()
	h.Start(rr, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil).WithContext(context.Background()))
	if rr.Header().Get("Location") != "/login?error="+oauthErrFailed {
		t.Fatalf("location = %q", rr.Header().Get("Location"))
	}
}

func TestGoogleStartHostedDomainHint(t *testing.T) {
	rig := newGoogleRig(t)
	rig.cfg.GoogleAllowedDomains = []string{"corp.example"}
	_, q := rig.start(t)
	if q.Get("hd") != "corp.example" {
		t.Fatalf("hd = %q", q.Get("hd"))
	}
}
