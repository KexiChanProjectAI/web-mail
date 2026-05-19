package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"lite-mail/internal/testutil"
)

func TestParseMIMESimpleText(t *testing.T) {
	raw := loadFixture(t, "simple-text.eml")
	msg, err := ParseMIME(raw)
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if msg.Sender != "sender@example.com" {
		t.Fatalf("Sender = %q", msg.Sender)
	}
	if msg.Subject != "Simple text email" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if len(msg.Recipients) != 1 || msg.Recipients[0].Email != "recipient@example.com" || msg.Recipients[0].Type != "to" {
		t.Fatalf("Recipients = %#v", msg.Recipients)
	}
	if !strings.Contains(msg.TextBody, "simple plain text email body") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if msg.HTMLBody != "" {
		t.Fatalf("HTMLBody = %q, want empty", msg.HTMLBody)
	}
}

func TestParseMIMEHTMLMultipart(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "html-multipart.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !strings.Contains(msg.TextBody, "plain text version") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>HTML</strong>") {
		t.Fatalf("HTMLBody = %q", msg.HTMLBody)
	}
}

func TestParseMIMEAttachmentMultipart(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "attachment-multipart.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !strings.Contains(msg.TextBody, "contains an attachment") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("Attachments len = %d, want 1", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.OriginalFilename != "test-document.txt" {
		t.Fatalf("OriginalFilename = %q", att.OriginalFilename)
	}
	if att.MimeType != "text/plain" {
		t.Fatalf("MimeType = %q", att.MimeType)
	}
	if !strings.Contains(string(att.Content), "attached text document") {
		t.Fatalf("Content = %q", att.Content)
	}
	if att.Size != int64(len(att.Content)) || att.ContentHash != HashBytes(att.Content) {
		t.Fatalf("attachment metadata mismatch: %#v", att)
	}
}

func TestParseMIMEUTF8Subject(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "utf8-subject.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if msg.Subject != "Résumê Discussion" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
}

func TestParseMIMEContentHash(t *testing.T) {
	raw := loadFixture(t, "simple-text.eml")
	msg, err := ParseMIME(raw)
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:])
	if msg.ContentHash != want {
		t.Fatalf("ContentHash = %q, want %q", msg.ContentHash, want)
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := testutil.LoadFixture(name)
	if err != nil {
		t.Fatalf("LoadFixture(%s): %v", name, err)
	}
	return raw
}
