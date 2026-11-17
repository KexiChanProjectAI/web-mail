package ingest

import (
	"bytes"
	"net/http"
	"net/mail"
	"regexp"
	"strings"

	"lite-mail/internal/emailaddr"
)

// EnvelopeToHeader carries the SMTP RCPT TO address (the Worker's message.to).
// It is trusted only because the request already passed the ingest PSK check.
const EnvelopeToHeader = "X-Lite-Mail-To"

// RecipientTypeEnvelope marks the address Cloudflare Email Routing actually
// delivered the message to. It decides which mailbox the message belongs to
// only when the message has no To/Cc/Bcc recipient.
const RecipientTypeEnvelope = "envelope"

// cloudflareReceivedFor matches the "for <addr>" clause of the Received header
// that Cloudflare Email Routing prepends ("by cloudflare-email.net" or ".org").
var cloudflareReceivedFor = regexp.MustCompile(`(?i)\bby\s+cloudflare-email\.(?:net|org)\b.*?\bfor\s+<([^<>\s]+)>`)

// envelopeRecipient returns the canonical envelope recipient for an ingest
// request: the Worker's header when present, otherwise the one recorded in
// Cloudflare's own Received header. It returns "" when neither is usable.
func envelopeRecipient(r *http.Request, raw []byte) string {
	if addr := validEnvelopeAddress(r.Header.Get(EnvelopeToHeader)); addr != "" {
		return addr
	}
	return EnvelopeRecipientFromRaw(raw)
}

// EnvelopeRecipientFromRaw reads the envelope recipient from the topmost
// Received header, and only if Cloudflare Email Routing wrote that header.
// Lower Received headers are ignored because the sender controls them.
func EnvelopeRecipientFromRaw(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	received := msg.Header["Received"]
	if len(received) == 0 {
		return ""
	}
	// net/mail keeps folded continuation lines; flatten them before matching.
	top := strings.Join(strings.Fields(received[0]), " ")
	m := cloudflareReceivedFor.FindStringSubmatch(top)
	if m == nil {
		return ""
	}
	return validEnvelopeAddress(m[1])
}

func validEnvelopeAddress(value string) string {
	addr := emailaddr.Canonical(value)
	if addr == "" || len(addr) > 320 || strings.Count(addr, "@") != 1 {
		return ""
	}
	local, domain, _ := strings.Cut(addr, "@")
	if local == "" || domain == "" || strings.ContainsAny(addr, " \t\r\n<>,\"") {
		return ""
	}
	return addr
}
