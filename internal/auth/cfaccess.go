package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"lite-mail/internal/emailaddr"
)

const (
	// CFAccessHeader is the header Cloudflare Access adds to proxied requests.
	CFAccessHeader = "Cf-Access-Jwt-Assertion"
	// CFAccessCookie is the cookie Cloudflare Access sets on the protected hostname.
	CFAccessCookie = "CF_Authorization"
)

// ErrNoAccessEmail is returned for Access tokens without an email claim, such
// as service-token JWTs; they are treated as unauthenticated.
var ErrNoAccessEmail = errors.New("cloudflare access token has no email")

// AccessClaims are the Cloudflare Access application-token claims we use.
type AccessClaims struct {
	Email   string `json:"email"`
	Subject string `json:"sub"`
	Type    string `json:"type"`
	// IssuedAt is when Cloudflare issued this Access session's token; it
	// identifies one sign-in for the audit log.
	IssuedAt time.Time `json:"-"`
}

// AccessVerifier validates Cloudflare Access JWTs: RS256 signature against the
// team's JWKS (cached, re-fetched on unknown kid), issuer, audience (the Access
// application AUD tag) and expiry.
type AccessVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewAccessVerifier builds a verifier for issuer (https://<team>.cloudflareaccess.com).
// Keys are fetched lazily from <issuer>/cdn-cgi/access/certs.
func NewAccessVerifier(issuer, aud string) *AccessVerifier {
	issuer = strings.TrimRight(issuer, "/")
	return NewAccessVerifierWithJWKS(issuer, issuer+"/cdn-cgi/access/certs", aud, nil)
}

// NewAccessVerifierWithJWKS lets tests point at an arbitrary issuer and JWKS URL.
func NewAccessVerifierWithJWKS(issuer, jwksURL, aud string, client *http.Client) *AccessVerifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	ctx := oidc.ClientContext(context.Background(), client)
	keySet := oidc.NewRemoteKeySet(ctx, jwksURL)
	return &AccessVerifier{verifier: oidc.NewVerifier(issuer, keySet, &oidc.Config{
		ClientID:             aud,
		SupportedSigningAlgs: []string{oidc.RS256},
	})}
}

// TokenFromRequest returns the Access JWT from the header, falling back to the cookie.
func (v *AccessVerifier) TokenFromRequest(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get(CFAccessHeader)); token != "" {
		return token
	}
	if cookie, err := r.Cookie(CFAccessCookie); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

// Verify checks raw and returns its claims with a canonical email.
func (v *AccessVerifier) Verify(ctx context.Context, raw string) (*AccessClaims, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var claims AccessClaims
	if err := token.Claims(&claims); err != nil {
		return nil, err
	}
	claims.Email = emailaddr.Canonical(claims.Email)
	claims.IssuedAt = token.IssuedAt
	if claims.Email == "" {
		return nil, ErrNoAccessEmail
	}
	return &claims, nil
}
