package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"lite-mail/internal/ingest"
)

func TestIngest_SimpleText(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := uniqueMessage(t, "simple-text.eml", "Simple text email", "recipient@example.com")
	messageID := ingestRaw(t, suite, raw)

	var subject, rawPath string
	if err := suite.db.QueryRow("SELECT subject, raw_mime_path FROM messages WHERE id = ?", messageID).Scan(&subject, &rawPath); err != nil {
		t.Fatalf("select message: %v", err)
	}
	if !strings.Contains(subject, "Simple text email") || rawPath != rawMIMEPath(raw) {
		t.Fatalf("subject=%q rawPath=%q", subject, rawPath)
	}
	if _, err := os.Stat(rawMIMEFile(suite, raw)); err != nil {
		t.Fatalf("raw MIME file missing: %v", err)
	}
}

func TestIngest_HTMLMultipart(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "html-multipart.eml", "HTML multipart", "recipient@example.com"))
	var textBody, htmlBody string
	if err := suite.db.QueryRow("SELECT COALESCE(text_body,''), COALESCE(html_body,'') FROM messages WHERE id = ?", messageID).Scan(&textBody, &htmlBody); err != nil {
		t.Fatalf("select bodies: %v", err)
	}
	if !strings.Contains(textBody, "plain text version") || !strings.Contains(htmlBody, "<strong>HTML</strong>") {
		t.Fatalf("unexpected bodies text=%q html=%q", textBody, htmlBody)
	}
}

func TestIngest_WithAttachment(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Email with Attachment", "recipient@example.com"))
	var filename, mimeType string
	var sizeBytes int64
	if err := suite.db.QueryRow("SELECT COALESCE(original_filename,''), mime_type, size_bytes FROM attachments WHERE message_id = ?", messageID).Scan(&filename, &mimeType, &sizeBytes); err != nil {
		t.Fatalf("select attachment: %v", err)
	}
	if filename != "test-document.txt" || mimeType != "text/plain" || sizeBytes == 0 {
		t.Fatalf("attachment filename=%q mime=%q size=%d", filename, mimeType, sizeBytes)
	}
	if _, err := os.Stat(attachmentFile(t, suite, messageID)); err != nil {
		t.Fatalf("attachment file missing: %v", err)
	}
}

func TestIngest_DuplicateDetection(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := uniqueMessage(t, "simple-text.eml", "Duplicate", "recipient@example.com")
	ingestRaw(t, suite, raw)
	resp := postIngest(t, suite, raw, WorkerIngestPSK)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate status = %d", resp.StatusCode)
	}
	var decoded struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode duplicate: %v", err)
	}
	if decoded.Status != "duplicate" {
		t.Fatalf("duplicate response = %+v", decoded)
	}
}

func TestIngest_InvalidPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := postIngest(t, suite, LoadFixture(t, "simple-text.eml"), "wrong")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestIngest_MissingPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := postIngest(t, suite, LoadFixture(t, "simple-text.eml"), "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestIngest_TooLarge(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	suite.config.MaxMessageBytes = 8
	resp := postIngest(t, suite, []byte("From: sender@example.com\r\n\r\nthis body is too large"), WorkerIngestPSK)
	assertStatus(t, resp, http.StatusRequestEntityTooLarge)
}

func TestIngest_VerifiesContentHashRecord(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := uniqueMessage(t, "simple-text.eml", "Hash", "recipient@example.com")
	messageID := ingestRaw(t, suite, raw)
	hash := dbScalar[string](t, suite, "SELECT content_hash FROM messages WHERE id = ?", messageID)
	if hash != ingest.HashBytes(raw) {
		t.Fatalf("content hash = %s, want %s", hash, ingest.HashBytes(raw))
	}
}
