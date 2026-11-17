package audit

import (
	"net/http/httptest"
	"strings"
	"testing"

	"lite-mail/internal/auth"
)

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if got := ClientIP(r); got != "127.0.0.1" {
		t.Fatalf("remote addr = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("xff = %q", got)
	}
	r.Header.Set("Cf-Connecting-Ip", "2001:db8::1")
	if got := ClientIP(r); got != "2001:db8::1" {
		t.Fatalf("cf-connecting-ip should win, got %q", got)
	}
}

func TestTruncateKeepsRunes(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	got := truncate(s, 5)
	if len(got) != 4 || !strings.HasPrefix(s, got) {
		t.Fatalf("truncate cut a rune: %q", got)
	}
	if truncate("short", 10) != "short" {
		t.Fatal("short string changed")
	}
}

func TestSignInDetail(t *testing.T) {
	cases := []struct {
		s    *auth.Session
		want string
	}{
		{&auth.Session{IsAdmin: true, Provider: auth.ProviderCFAccess}, "admin"},
		{&auth.Session{Provider: auth.ProviderGoogle}, ""},
		{&auth.Session{Provider: auth.ProviderCFAccess}, "no mailbox granted"},
		{&auth.Session{Provider: auth.ProviderCFAccess, Grants: auth.ParseGrants([]string{"a@x.io", "*@y.io"})}, "grants: a@x.io, *@y.io"},
	}
	for _, tc := range cases {
		if got := signInDetail(tc.s); got != tc.want {
			t.Errorf("signInDetail(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	r := httptest.NewRequest("GET", "/", nil)
	s := &auth.Session{Email: "a@b.co"}
	l.Record(r, s, MessageView)
	l.RecordList(r, s, "", "x")
	l.SignIn(r, s)
	l.SignInFailed(r, "cf_access", "", "x")
	l.SignOut(r, s)
	if events, err := l.List(r.Context(), Filter{}); err != nil || len(events) != 0 {
		t.Fatalf("nil List = %v, %v", events, err)
	}
}
