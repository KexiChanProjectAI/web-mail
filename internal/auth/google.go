package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"lite-mail/internal/config"
	"lite-mail/internal/emailaddr"
	"lite-mail/internal/middleware"
)

const (
	// GoogleIssuer is Google's OIDC issuer.
	GoogleIssuer = "https://accounts.google.com"

	googleCallbackPath = "/auth/google/callback"
	oauthCookieName    = "lite_mail_oauth"
	oauthCookiePath    = "/auth/google"
	oauthFlowTTL       = 10 * time.Minute
)

// Login-page error codes set by the callback.
const (
	oauthErrDenied           = "oauth_denied"
	oauthErrFailed           = "oauth_failed"
	oauthErrEmailUnverified  = "email_unverified"
	oauthErrDomainNotAllowed = "domain_not_allowed"
)

type oauthFlow struct {
	nonce     string
	verifier  string
	expiresAt time.Time
}

// GoogleHandler runs the OIDC authorization-code flow with state, nonce and PKCE.
type GoogleHandler struct {
	cfg      *config.Config
	sessions SessionStore
	logins   LoginRecorder
	issuer   string
	client   *http.Client

	mu       sync.Mutex
	flows    map[string]oauthFlow
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	auditor  Auditor
}

// SetAuditor wires the audit log; nil disables it.
func (h *GoogleHandler) SetAuditor(auditor Auditor) { h.auditor = auditor }

// NewGoogleHandler builds the handler. issuer is GoogleIssuer in production
// and a fake provider in tests. Discovery happens lazily on first use.
func NewGoogleHandler(cfg *config.Config, sessions SessionStore, logins LoginRecorder, issuer string, client *http.Client) *GoogleHandler {
	if issuer == "" {
		issuer = GoogleIssuer
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &GoogleHandler{
		cfg:      cfg,
		sessions: sessions,
		logins:   logins,
		issuer:   issuer,
		client:   client,
		flows:    make(map[string]oauthFlow),
	}
}

func (h *GoogleHandler) clientContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, h.client)
}

// init performs discovery once; failures are retried on the next request.
func (h *GoogleHandler) init(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.provider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(h.clientContext(context.WithoutCancel(ctx)), h.issuer)
	if err != nil {
		return err
	}
	h.provider = provider
	h.oauth = &oauth2.Config{
		ClientID:     h.cfg.GoogleClientID,
		ClientSecret: h.cfg.GoogleClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  strings.TrimRight(h.cfg.PublicBaseURL, "/") + googleCallbackPath,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	h.verifier = provider.VerifierContext(h.clientContext(context.Background()), &oidc.Config{ClientID: h.cfg.GoogleClientID})
	return nil
}

// Start redirects the browser to Google.
func (h *GoogleHandler) Start(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestIDFromContext(r.Context())
	if err := h.init(r.Context()); err != nil {
		slog.Error("google discovery failed", "request_id", requestID, "error", err.Error())
		redirectLoginError(w, r, oauthErrFailed)
		return
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	if err := errors.Join(err1, err2); err != nil {
		redirectLoginError(w, r, oauthErrFailed)
		return
	}
	verifier := oauth2.GenerateVerifier()

	h.mu.Lock()
	now := time.Now()
	for key, flow := range h.flows {
		if now.After(flow.expiresAt) {
			delete(h.flows, key)
		}
	}
	h.flows[state] = oauthFlow{nonce: nonce, verifier: verifier, expiresAt: now.Add(oauthFlowTTL)}
	oauthCfg := h.oauth
	h.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookieName,
		Value:    state,
		Path:     oauthCookiePath,
		MaxAge:   int(oauthFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(h.cfg),
		SameSite: http.SameSiteLaxMode,
	})

	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}
	if len(h.cfg.GoogleAllowedDomains) == 1 {
		// UX hint only; the email domain is enforced in the callback.
		opts = append(opts, oauth2.SetAuthURLParam("hd", h.cfg.GoogleAllowedDomains[0]))
	}
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, opts...), http.StatusFound)
}

type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// Callback completes the flow, records the login and starts a session.
func (h *GoogleHandler) Callback(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestIDFromContext(r.Context())
	email := ""
	fail := func(code, reason string, err error) {
		attrs := []any{"request_id", requestID, "reason", reason}
		if err != nil {
			attrs = append(attrs, "error", err.Error())
		}
		slog.Warn("google login failed", attrs...)
		if h.auditor != nil {
			h.auditor.SignInFailed(r, ProviderGoogle, email, code+": "+reason)
		}
		redirectLoginError(w, r, code)
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookieName, Value: "", Path: oauthCookiePath, MaxAge: -1, HttpOnly: true, Secure: secureCookies(h.cfg), SameSite: http.SameSiteLaxMode})

	query := r.URL.Query()
	if query.Get("error") != "" {
		fail(oauthErrDenied, "provider returned error "+query.Get("error"), nil)
		return
	}
	state := query.Get("state")
	cookie, err := r.Cookie(oauthCookieName)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		fail(oauthErrFailed, "state mismatch", nil)
		return
	}
	h.mu.Lock()
	flow, ok := h.flows[state]
	delete(h.flows, state)
	oauthCfg, verifier := h.oauth, h.verifier
	h.mu.Unlock()
	if !ok || time.Now().After(flow.expiresAt) || oauthCfg == nil {
		fail(oauthErrFailed, "unknown or expired state", nil)
		return
	}

	ctx := h.clientContext(r.Context())
	token, err := oauthCfg.Exchange(ctx, query.Get("code"), oauth2.VerifierOption(flow.verifier))
	if err != nil {
		fail(oauthErrFailed, "code exchange", err)
		return
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		fail(oauthErrFailed, "no id_token in token response", nil)
		return
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		fail(oauthErrFailed, "id_token verification", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(flow.nonce)) != 1 {
		fail(oauthErrFailed, "nonce mismatch", nil)
		return
	}
	var claims googleClaims
	if err := idToken.Claims(&claims); err != nil {
		fail(oauthErrFailed, "claims decode", err)
		return
	}
	email = emailaddr.Canonical(claims.Email)
	if email == "" || !claims.EmailVerified {
		fail(oauthErrEmailUnverified, "email missing or unverified", nil)
		return
	}
	if len(h.cfg.GoogleAllowedDomains) > 0 && !slices.Contains(h.cfg.GoogleAllowedDomains, emailaddr.Domain(email)) {
		fail(oauthErrDomainNotAllowed, "domain not allowed: "+emailaddr.Domain(email), nil)
		return
	}

	if h.logins != nil {
		if err := h.logins.RecordLogin(r.Context(), email, ProviderGoogle); err != nil {
			fail(oauthErrFailed, "record login", err)
			return
		}
	}
	session := Session{Email: email, Provider: ProviderGoogle}
	if err := StartSession(w, r, h.sessions, h.cfg, &session); err != nil {
		fail(oauthErrFailed, "create session", err)
		return
	}
	slog.Info("login successful", "request_id", requestID, "ip", clientIPFromRequest(r), "provider", ProviderGoogle, "email", email)
	if h.auditor != nil {
		h.auditor.SignIn(r, &session)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
