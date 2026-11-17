package ingest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const forwardedRaw = "Received: from OS8PR02CU002.outbound.protection.outlook.com (2a01:111:f403:c406::3)\r\n" +
	"        by cloudflare-email.org (cloudflare) id 5BA26eSA4pFy\r\n" +
	"        for <Catch-All@046542.example>; Tue, 17 Nov 2026 14:07:09 +0000\r\n" +
	"Received: from attacker.example by relay.example for <victim@other.example>; Tue, 17 Nov 2026 14:07:00 +0000\r\n" +
	"From: Microsoft Security <MSSecurity-noreply@microsoft.com>\r\n" +
	"To: yzs@tenant.onmicrosoft.example\r\n" +
	"Subject: digest\r\n" +
	"\r\n" +
	"body\r\n"

func TestEnvelopeRecipientFromRaw(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"cloudflare top received, folded", forwardedRaw, "catch-all@046542.example"},
		{"cloudflare .net", "Received: from x by cloudflare-email.net (cloudflare) id 1 for <a@b.example>; date\r\nTo: z@z.example\r\n\r\nbody", "a@b.example"},
		{"top received not from cloudflare", "Received: from x by relay.example for <a@b.example>; date\r\nReceived: from y by cloudflare-email.net id 2 for <c@d.example>; date\r\n\r\nbody", ""},
		{"no received", "To: a@b.example\r\n\r\nbody", ""},
		{"cloudflare without for clause", "Received: from x by cloudflare-email.net id 1; date\r\n\r\nbody", ""},
		{"bad address", "Received: from x by cloudflare-email.net id 1 for <a@b@c>; date\r\n\r\nbody", ""},
		{"not a message", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EnvelopeRecipientFromRaw([]byte(tc.raw)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvelopeRecipientPrefersWorkerHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(""))
	req.Header.Set(EnvelopeToHeader, " Box@Mine.Example ")
	if got := envelopeRecipient(req, []byte(forwardedRaw)); got != "box@mine.example" {
		t.Fatalf("header recipient = %q", got)
	}

	req.Header.Set(EnvelopeToHeader, "not an address")
	if got := envelopeRecipient(req, []byte(forwardedRaw)); got != "catch-all@046542.example" {
		t.Fatalf("invalid header should fall back to Received, got %q", got)
	}

	req.Header.Del(EnvelopeToHeader)
	if got := envelopeRecipient(req, []byte("To: a@b.example\r\n\r\nbody")); got != "" {
		t.Fatalf("no envelope source should give empty, got %q", got)
	}
}
