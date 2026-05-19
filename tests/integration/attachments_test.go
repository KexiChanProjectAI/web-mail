package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDownloadAttachment(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Attachment Download", "recipient@example.com"))
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d/attachments/0", messageID), cookies)
	body := assertStatus(t, resp, http.StatusOK)
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("content-type = %q", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), `filename="test-document.txt"`) {
		t.Fatalf("content-disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	if !strings.Contains(string(body), "This is the content of the attached text document.") {
		t.Fatalf("attachment body = %q", body)
	}
}

func TestDownloadAttachment_NotFound(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Attachment Missing", "recipient@example.com"))
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d/attachments/99", messageID), cookies)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestDownloadAttachment_Unauthorized(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Private Attachment", "owner@example.com"))
	cookies := login(t, suite, NormalUserPSK, "other@example.com")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d/attachments/0", messageID), cookies)
	assertStatus(t, resp, http.StatusNotFound)
}
