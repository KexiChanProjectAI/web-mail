package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"lite-mail/internal/ingest"
)

// forwardedMessage is mail a tenant forwarded to our catch-all: the To header
// names the original recipient, Cloudflare's Received header the real one.
// An empty headerTo leaves the To header out, as for Bcc-only delivery.
func forwardedMessage(headerTo, envelopeTo string) []byte {
	to := ""
	if headerTo != "" {
		to = "To: " + headerTo + "\r\n"
	}
	return []byte(fmt.Sprintf("Received: from relay.example (2a01:111::3)\r\n"+
		"        by cloudflare-email.org (cloudflare) id test\r\n"+
		"        for <%s>; Tue, 17 Nov 2026 14:07:09 +0000\r\n"+
		"From: Security <security-noreply@vendor.example>\r\n"+
		"%s"+
		"Subject: Weekly digest %d\r\n"+
		"Date: Tue, 17 Nov 2026 14:07:10 +0000\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"digest body\r\n", envelopeTo, to, time.Now().UnixNano()))
}

func postIngestEnvelope(t *testing.T, suite *IntegrationTestSuite, raw []byte, envelopeTo string) (string, int64) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, suite.server.URL+"/api/ingest", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create ingest request: %v", err)
	}
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set("X-Lite-Mail-Ingest-PSK", WorkerIngestPSK)
	if envelopeTo != "" {
		req.Header.Set(ingest.EnvelopeToHeader, envelopeTo)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatalf("post ingest: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest status = %d, body = %s", resp.StatusCode, body)
	}
	var decoded struct {
		Status    string `json:"status"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	return decoded.Status, decoded.MessageID
}

func listTotal(t *testing.T, suite *IntegrationTestSuite, cookies []*http.Cookie, path string) int {
	t.Helper()
	return decodeJSON[listResponse](t, getWithCookies(t, suite, path, cookies)).Total
}

func TestEnvelope_ForwardedMailBelongsToToRecipient(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := forwardedMessage("yzs@tenant.example", "catch-all@mine.example")
	status, id := postIngestEnvelope(t, suite, raw, "Catch-All@Mine.example")
	if status != "accepted" || id == 0 {
		t.Fatalf("ingest = %s %d", status, id)
	}

	owner := asUser(t, suite, "owner@corp.example", "yzs@tenant.example")
	domainOwner := asUser(t, suite, "domain@corp.example", "*@tenant.example")
	envelopeOnly := asUser(t, suite, "envelope@corp.example", "*@mine.example")

	if got := listTotal(t, suite, owner, "/api/messages"); got != 1 {
		t.Fatalf("exact To grant sees %d messages, want 1", got)
	}
	if got := listTotal(t, suite, domainOwner, "/api/messages?mailbox=yzs@tenant.example"); got != 1 {
		t.Fatalf("domain To grant with mailbox filter sees %d, want 1", got)
	}
	if got := listTotal(t, suite, envelopeOnly, "/api/messages"); got != 0 {
		t.Fatalf("grant on the envelope recipient sees %d messages, want 0", got)
	}
	if got := listTotal(t, suite, envelopeOnly, "/api/messages?mailbox=catch-all@mine.example"); got != 0 {
		t.Fatalf("envelope mailbox filter sees %d messages, want 0", got)
	}
	assertStatus(t, getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", id), envelopeOnly), http.StatusNotFound)

	msg := decodeJSON[messageResponse](t, getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", id), owner))
	want := []recipientResponse{{Email: "catch-all@mine.example", Type: "envelope"}, {Email: "yzs@tenant.example", Type: "to"}}
	if !slices.Equal(msg.Recipients, want) {
		t.Fatalf("recipients = %+v, want %+v", msg.Recipients, want)
	}

	// The admin grant editor suggests the To mailbox, not the envelope address.
	admin := accessCookies(t, suite, EnvAdminEmail)
	known := decodeJSON[struct {
		Addresses []string `json:"addresses"`
		Domains   []string `json:"domains"`
	}](t, getWithCookies(t, suite, "/api/admin/mailboxes", admin))
	if !slices.Contains(known.Addresses, "yzs@tenant.example") || slices.Contains(known.Addresses, "catch-all@mine.example") {
		t.Fatalf("known mailboxes = %+v", known)
	}
}

func TestEnvelope_NoHeaderRecipientUsesEnvelope(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	_, id := postIngestEnvelope(t, suite, forwardedMessage("", "hidden@mine.example"), "hidden@mine.example")
	reader := asUser(t, suite, "hidden@corp.example", "hidden@mine.example")
	if got := listTotal(t, suite, reader, "/api/messages"); got != 1 {
		t.Fatalf("envelope grant on a message without To sees %d, want 1", got)
	}
	assertStatus(t, getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", id), reader), http.StatusOK)
}

func TestEnvelope_FallsBackToCloudflareReceived(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	// An older Worker sends no envelope header.
	_, id := postIngestEnvelope(t, suite, forwardedMessage("yzs@tenant.example", "catch-all@mine.example"), "")
	got := dbScalar[string](t, suite, "SELECT recipient_email FROM message_recipients WHERE message_id = ? AND recipient_type = 'envelope'", id)
	if got != "catch-all@mine.example" {
		t.Fatalf("envelope recipient = %q", got)
	}
}

func TestEnvelope_WithoutEnvelopeUsesHeaders(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "No envelope", "inbox@legacy.example"))
	reader := asUser(t, suite, "legacy@corp.example", "inbox@legacy.example")
	if got := listTotal(t, suite, reader, "/api/messages"); got != 1 {
		t.Fatalf("header fallback sees %d messages, want 1", got)
	}
}

func TestEnvelope_DuplicateAddsSecondEnvelopeRecipient(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := forwardedMessage("", "a@mine.example")
	_, id := postIngestEnvelope(t, suite, raw, "a@mine.example")
	status, _ := postIngestEnvelope(t, suite, raw, "b@mine.example")
	if status != "duplicate" {
		t.Fatalf("second delivery status = %s", status)
	}
	// A retry of the same delivery adds nothing.
	postIngestEnvelope(t, suite, raw, "b@mine.example")
	if n := dbScalar[int](t, suite, "SELECT COUNT(*) FROM message_recipients WHERE message_id = ? AND recipient_type = 'envelope'", id); n != 2 {
		t.Fatalf("envelope rows = %d, want 2", n)
	}
	b := asUser(t, suite, "b@corp.example", "b@mine.example")
	if got := listTotal(t, suite, b, "/api/messages"); got != 1 {
		t.Fatalf("second envelope recipient sees %d messages, want 1", got)
	}
}

func TestEnvelope_Backfill(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	_, forwarded := postIngestEnvelope(t, suite, forwardedMessage("yzs@tenant.example", "catch-all@mine.example"), "")
	plain := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Plain", "inbox@legacy.example"))
	// Simulate mail stored before envelope tracking.
	if _, err := suite.db.Exec("DELETE FROM message_recipients WHERE recipient_type = 'envelope'"); err != nil {
		t.Fatalf("reset envelope rows: %v", err)
	}

	dry, err := ingest.BackfillEnvelopeRecipients(context.Background(), suite.db, suite.storage, true)
	if err != nil || dry.Scanned != 2 || dry.Added != 1 || !slices.Equal(dry.NoEnvelope, []int64{plain}) {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if n := dbScalar[int](t, suite, "SELECT COUNT(*) FROM message_recipients WHERE recipient_type = 'envelope'"); n != 0 {
		t.Fatalf("dry run wrote %d rows", n)
	}

	res, err := ingest.BackfillEnvelopeRecipients(context.Background(), suite.db, suite.storage, false)
	if err != nil || res.Added != 1 {
		t.Fatalf("backfill = %+v, %v", res, err)
	}
	got := dbScalar[string](t, suite, "SELECT recipient_email FROM message_recipients WHERE message_id = ? AND recipient_type = 'envelope'", forwarded)
	if got != "catch-all@mine.example" {
		t.Fatalf("backfilled envelope = %q", got)
	}
	again, err := ingest.BackfillEnvelopeRecipients(context.Background(), suite.db, suite.storage, false)
	if err != nil || again.Added != 0 || again.Scanned != 1 {
		t.Fatalf("second backfill = %+v, %v", again, err)
	}
}

func TestEnvelope_AuditRecordsToMailbox(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	_, id := postIngestEnvelope(t, suite, forwardedMessage("yzs@tenant.example", "catch-all@mine.example"), "catch-all@mine.example")
	// Granted both the envelope domain and the To domain: the access goes
	// through the To mailbox only.
	reader := asUser(t, suite, "both@corp.example", "*@mine.example", "*@tenant.example")
	assertStatus(t, getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", id), reader), http.StatusOK)

	rows := auditRows(t, suite, "event = 'message_view' AND message_id = ?", id)
	if len(rows) != 1 || rows[0].Mailbox != "yzs@tenant.example" {
		t.Fatalf("message_view audit rows = %+v", rows)
	}
}
