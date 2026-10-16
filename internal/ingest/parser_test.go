package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestParseMIMEQuotedPrintableHTML(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "quoted-printable-html.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>565503</strong>") {
		t.Fatalf("HTMLBody should contain decoded HTML with strong tag, got = %q", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, "=3D") {
		t.Fatalf("HTMLBody should not contain quoted-printable encoding artifacts (=3D), got = %q", msg.HTMLBody)
	}
	if msg.Subject != "Your temporary ChatGPT login code" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
}

func TestParseMIMEQuotedPrintableMultipart(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "quoted-printable-multipart.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !strings.Contains(msg.TextBody, "Your verification code is: 565503") {
		t.Fatalf("TextBody should contain decoded text, got = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>565503</strong>") {
		t.Fatalf("HTMLBody should contain decoded HTML, got = %q", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, "=3D") {
		t.Fatalf("HTMLBody should not contain QP artifacts, got = %q", msg.HTMLBody)
	}
}

func TestParseMIMEBase64Multipart(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "base64-multipart.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !strings.Contains(msg.TextBody, "Hello from base64!") {
		t.Fatalf("TextBody should contain decoded text, got = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>base64</strong>") {
		t.Fatalf("HTMLBody should contain decoded HTML, got = %q", msg.HTMLBody)
	}
}
func TestParseMIMEHTMLOnly(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "html-only.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	// Should have HTML body
	if !strings.Contains(msg.HTMLBody, "281043") {
		t.Fatalf("HTMLBody should contain the code, got = %q", msg.HTMLBody)
	}
	// Should have generated text body from HTML
	if msg.TextBody == "" {
		t.Fatal("TextBody should be generated from HTML when no text part exists")
	}
	if !strings.Contains(msg.TextBody, "281043") {
		t.Fatalf("TextBody should contain the code from HTML conversion, got = %q", msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "输入此临时验证码") {
		t.Fatalf("TextBody should contain decoded Chinese text, got = %q", msg.TextBody)
	}
	// Should not contain HTML tags
	if strings.Contains(msg.TextBody, "<strong>") || strings.Contains(msg.TextBody, "<p>") {
		t.Fatal("TextBody should not contain HTML tags after conversion")
	}
}

func TestParseMIMEHTMLWithStyleTag(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "html-with-style.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}

	// Should have HTML body
	if !strings.Contains(msg.HTMLBody, "<strong>123456</strong>") {
		t.Fatalf("HTMLBody should contain decoded HTML with strong tag, got = %q", msg.HTMLBody)
	}

	// TextBody should contain the actual content
	if !strings.Contains(msg.TextBody, "123456") {
		t.Fatalf("TextBody should contain the code from HTML conversion, got = %q", msg.TextBody)
	}

	// TextBody must NOT contain CSS from <style> blocks
	if strings.Contains(msg.TextBody, "font-family") || strings.Contains(msg.TextBody, "sans-serif") {
		t.Fatalf("TextBody should not contain CSS from <style> tags, got = %q", msg.TextBody)
	}

	// TextBody must NOT contain HTML tags
	if strings.Contains(msg.TextBody, "<strong>") || strings.Contains(msg.TextBody, "<p>") {
		t.Fatal("TextBody should not contain HTML tags after conversion")
	}
}

func TestParseMIMEISO8859HTML(t *testing.T) {
	msg, err := ParseMIME(loadFixture(t, "iso8859-html.eml"))
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if !utf8.ValidString(msg.HTMLBody) {
		t.Fatalf("HTMLBody is not valid UTF-8: %q", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, "\xf6") {
		t.Fatalf("HTMLBody still contains raw 0xF6 byte: %q", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "Schöne Grüße") {
		t.Fatalf("HTMLBody should contain decoded Latin-1 text, got = %q", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>565503</strong>") {
		t.Fatalf("HTMLBody should contain the code, got = %q", msg.HTMLBody)
	}
	if !utf8.ValidString(msg.TextBody) || !strings.Contains(msg.TextBody, "565503") || !strings.Contains(msg.TextBody, "ö") {
		t.Fatalf("TextBody should be decoded UTF-8 with the code, got = %q", msg.TextBody)
	}
}

func TestDecodeText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
		in    []byte
		want  string
	}{
		{name: "utf-8 passthrough", label: "utf-8", in: []byte("输入此临时验证码 ö"), want: "输入此临时验证码 ö"},
		{name: "missing label passthrough", label: "", in: []byte("plain ascii"), want: "plain ascii"},
		{name: "us-ascii passthrough", label: "us-ascii", in: []byte("code 565503"), want: "code 565503"},
		{name: "iso-8859-1", label: "iso-8859-1", in: []byte("Sch\xf6n"), want: "Schön"},
		{name: "latin1 alias", label: "latin1", in: []byte("Sch\xf6n"), want: "Schön"},
		{name: "quoted upper-case label", label: " ISO-8859-1 ", in: []byte("\xf6"), want: "ö"},
		{name: "windows-1252", label: "windows-1252", in: []byte("\x93hi\x94"), want: "\u201chi\u201d"},
		{name: "missing label with 8-bit byte", label: "", in: []byte("Sch\xf6n"), want: "Schön"},
		{name: "us-ascii mislabelled 8-bit", label: "us-ascii", in: []byte("Sch\xf6n"), want: "Schön"},
		{name: "utf-8 mislabelled latin1", label: "utf-8", in: []byte("Sch\xf6n 565503"), want: "Schön 565503"},
		{name: "unknown label", label: "x-bogus", in: []byte("Sch\xf6n"), want: "Schön"},
		{name: "iso-8859-2", label: "iso-8859-2", in: []byte("\xb1"), want: "ą"},
		{name: "gb2312", label: "gb2312", in: []byte("\xc4\xe3\xba\xc3"), want: "你好"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeText(tc.in, tc.label)
			if !utf8.ValidString(got) {
				t.Fatalf("decodeText returned invalid UTF-8: %q", got)
			}
			if got != tc.want {
				t.Fatalf("decodeText(%q, %q) = %q, want %q", tc.in, tc.label, got, tc.want)
			}
		})
	}
}

func TestParseMIMEEncodedWordCharsets(t *testing.T) {
	raw := []byte("From: =?gb2312?B?xOO6ww==?= <Sender@Example.com>\n" +
		"To: =?windows-1252?Q?J=F6rg?= <jorg@example.com>, plain@example.com\n" +
		"Subject: =?windows-1252?Q?Sch=F6n?= =?iso-8859-2?Q?=B1?=\n" +
		"MIME-Version: 1.0\n" +
		"Content-Type: text/plain; charset=utf-8\n" +
		"\n" +
		"body\n")
	msg, err := ParseMIME(raw)
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if msg.Subject != "Schöną" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if msg.Sender != "sender@example.com" {
		t.Fatalf("Sender = %q", msg.Sender)
	}
	if len(msg.Recipients) != 2 || msg.Recipients[0].Email != "jorg@example.com" || msg.Recipients[1].Email != "plain@example.com" {
		t.Fatalf("Recipients = %#v", msg.Recipients)
	}
}

func TestParseMIMERaw8BitHeaders(t *testing.T) {
	raw := []byte("From: \"J\xf6rg\" <<bad\n" +
		"To: user@example.com\n" +
		"Subject: Gr\xfc\xdfe\n" +
		"MIME-Version: 1.0\n" +
		"Content-Type: multipart/mixed; boundary=\"b\"\n" +
		"\n" +
		"--b\n" +
		"Content-Type: text/plain; charset=iso-8859-1\n" +
		"Content-Transfer-Encoding: quoted-printable\n" +
		"\n" +
		"Sch=F6n\n" +
		"--b\n" +
		"Content-Type: application/pdf\n" +
		"Content-Disposition: attachment; filename=\"R\xe9sum\xe9.pdf\"\n" +
		"\n" +
		"%PDF-\xff\xfe\n" +
		"--b\n" +
		"Content-Type: application/pdf\n" +
		"Content-Disposition: attachment; filename=\"=?iso-8859-1?Q?J=F6rg.pdf?=\"\n" +
		"\n" +
		"%PDF-\n" +
		"--b--\n")
	msg, err := ParseMIME(raw)
	if err != nil {
		t.Fatalf("ParseMIME: %v", err)
	}
	if msg.Subject != "Grüße" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !utf8.ValidString(msg.Sender) || !strings.Contains(msg.Sender, "ö") {
		t.Fatalf("Sender fallback should be valid UTF-8, got = %q", msg.Sender)
	}
	if !strings.Contains(msg.TextBody, "Schön") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if len(msg.Attachments) != 2 {
		t.Fatalf("Attachments len = %d, want 2", len(msg.Attachments))
	}
	if got := msg.Attachments[0].OriginalFilename; got != "Résumé.pdf" {
		t.Fatalf("raw 8-bit filename = %q", got)
	}
	if got := msg.Attachments[1].OriginalFilename; got != "Jörg.pdf" {
		t.Fatalf("encoded-word filename = %q", got)
	}
	if !strings.Contains(string(msg.Attachments[0].Content), "\xff\xfe") {
		t.Fatalf("attachment bytes must be left untouched, got = %q", msg.Attachments[0].Content)
	}
}
