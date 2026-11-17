// Package jwttest serves a fake JWKS and mints RS256 tokens for auth tests.
package jwttest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// FakeJWKS serves a JSON Web Key Set over httptest and mints RS256 tokens,
// standing in for Cloudflare Access or Google in tests.
type FakeJWKS struct {
	Server *httptest.Server
	Key    *rsa.PrivateKey
	KID    string

	mu   sync.Mutex
	keys map[string]*rsa.PublicKey
	// Handler, when set, receives every request that is not /jwks.
	Handler http.Handler
}

// NewFakeJWKS starts a JWKS server at Server.URL+"/jwks" with one signing key.
func NewFakeJWKS(t *testing.T) *FakeJWKS {
	t.Helper()
	key := mustRSAKey(t)
	f := &FakeJWKS{Key: key, KID: "test-key-1", keys: map[string]*rsa.PublicKey{"test-key-1": &key.PublicKey}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwks" {
			f.serveJWKS(w)
			return
		}
		if f.Handler != nil {
			f.Handler.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.Server.Close)
	return f
}

// JWKSURL is the key-set endpoint.
func (f *FakeJWKS) JWKSURL() string { return f.Server.URL + "/jwks" }

// AddKey publishes another public key (rotation) and returns its private key.
func (f *FakeJWKS) AddKey(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	key := mustRSAKey(t)
	f.mu.Lock()
	f.keys[kid] = &key.PublicKey
	f.mu.Unlock()
	return key
}

func (f *FakeJWKS) serveJWKS(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type jwk struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	set := struct {
		Keys []jwk `json:"keys"`
	}{}
	for kid, pub := range f.keys {
		set.Keys = append(set.Keys, jwk{
			Kty: "RSA", Kid: kid, Alg: "RS256", Use: "sig",
			N: b64(pub.N.Bytes()),
			E: b64(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}

// Mint signs claims with the default key.
func (f *FakeJWKS) Mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	return MintRS256(t, f.Key, f.KID, claims)
}

// AccessClaims returns Cloudflare-Access-shaped claims valid for an hour.
func AccessClaims(issuer, aud, email string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   issuer,
		"aud":   []string{aud},
		"email": email,
		"sub":   "sub-" + email,
		"type":  "app",
		"iat":   now.Unix(),
		"nbf":   now.Add(-time.Minute).Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
}

// MintRS256 builds a compact JWS signed with key.
func MintRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := b64(header) + "." + b64(payload)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + b64(sig)
}

// MintUnsigned builds an alg=none token, which verifiers must reject.
func MintUnsigned(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return b64(header) + "." + b64(payload) + "."
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
