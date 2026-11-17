package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lite-mail/internal/testutil/jwttest"
)

func TestAccessVerifier(t *testing.T) {
	jwks := jwttest.NewFakeJWKS(t)
	issuer, aud := "https://team.cloudflareaccess.com", "app-aud"
	v := NewAccessVerifierWithJWKS(issuer, jwks.JWKSURL(), aud, nil)
	ctx := context.Background()

	claims, err := v.Verify(ctx, jwks.Mint(t, jwttest.AccessClaims(issuer, aud, " Alice@Example.COM")))
	if err != nil || claims.Email != "alice@example.com" || claims.Type != "app" {
		t.Fatalf("valid token: claims=%+v err=%v", claims, err)
	}

	bad := map[string]func(map[string]any){
		"wrong audience": func(c map[string]any) { c["aud"] = []string{"other"} },
		"wrong issuer":   func(c map[string]any) { c["iss"] = "https://evil.cloudflareaccess.com" },
		"expired":        func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
	}
	for name, mutate := range bad {
		c := jwttest.AccessClaims(issuer, aud, "alice@example.com")
		mutate(c)
		if _, err := v.Verify(ctx, jwks.Mint(t, c)); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}

	if _, err := v.Verify(ctx, jwttest.MintUnsigned(jwttest.AccessClaims(issuer, aud, "a@b.co"))); err == nil {
		t.Error("alg=none token accepted")
	}

	stranger := jwttest.NewFakeJWKS(t) // different key, same kid
	if _, err := v.Verify(ctx, stranger.Mint(t, jwttest.AccessClaims(issuer, aud, "a@b.co"))); err == nil {
		t.Error("token signed by an unknown key accepted")
	}

	noEmail := jwttest.AccessClaims(issuer, aud, "")
	delete(noEmail, "email")
	noEmail["common_name"] = "service-token-id"
	if _, err := v.Verify(ctx, jwks.Mint(t, noEmail)); !errors.Is(err, ErrNoAccessEmail) {
		t.Errorf("service token: err = %v, want ErrNoAccessEmail", err)
	}
}

func TestAccessVerifierKeyRotation(t *testing.T) {
	jwks := jwttest.NewFakeJWKS(t)
	issuer, aud := "https://team.cloudflareaccess.com", "app-aud"
	v := NewAccessVerifierWithJWKS(issuer, jwks.JWKSURL(), aud, nil)
	if _, err := v.Verify(context.Background(), jwks.Mint(t, jwttest.AccessClaims(issuer, aud, "a@b.co"))); err != nil {
		t.Fatalf("initial key: %v", err)
	}
	rotated := jwks.AddKey(t, "test-key-2")
	token := jwttest.MintRS256(t, rotated, "test-key-2", jwttest.AccessClaims(issuer, aud, "a@b.co"))
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("rotated key not picked up: %v", err)
	}
}

func TestAccessTokenFromRequest(t *testing.T) {
	v := NewAccessVerifier("https://team.cloudflareaccess.com", "aud")
	req := httptest.NewRequest("GET", "/", nil)
	if got := v.TokenFromRequest(req); got != "" {
		t.Fatalf("empty request token = %q", got)
	}
	req.AddCookie(&http.Cookie{Name: CFAccessCookie, Value: "from-cookie"})
	if got := v.TokenFromRequest(req); got != "from-cookie" {
		t.Fatalf("cookie token = %q", got)
	}
	req.Header.Set(CFAccessHeader, "from-header")
	if got := v.TokenFromRequest(req); got != "from-header" {
		t.Fatalf("header should win, got %q", got)
	}
}
