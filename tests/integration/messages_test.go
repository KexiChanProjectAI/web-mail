package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListMessages_Pagination(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	for i := 0; i < 5; i++ {
		ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", fmt.Sprintf("Pagination %d", i), "recipient@example.com"))
	}
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, "/api/messages?page=1&per_page=2", cookies)
	list := decodeJSON[listResponse](t, resp)
	if list.Page != 1 || list.PerPage != 2 || list.Total != 5 || len(list.Messages) != 2 {
		t.Fatalf("pagination response = %+v", list)
	}
}

func TestListMessages_Search(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, withBody(uniqueMessage(t, "simple-text.eml", "Needle Subject", "recipient@example.com"), "uniqueneedle searchable body"))
	ingestRaw(t, suite, withBody(uniqueMessage(t, "simple-text.eml", "Ordinary Subject", "recipient@example.com"), "ordinary body"))
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, "/api/messages?q=uniqueneedle", cookies)
	list := decodeJSON[listResponse](t, resp)
	if list.Total != 1 || len(list.Messages) != 1 || !strings.Contains(list.Messages[0].Subject, "Needle Subject") {
		t.Fatalf("search response = %+v", list)
	}
}

func TestListMessages_NewestFirst(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	oldID := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Old message", "recipient@example.com"))
	time.Sleep(10 * time.Millisecond)
	newID := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "New message", "recipient@example.com"))
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, "/api/messages", cookies)
	list := decodeJSON[listResponse](t, resp)
	if len(list.Messages) < 2 || list.Messages[0].ID != newID || list.Messages[1].ID != oldID {
		t.Fatalf("newest first IDs = %+v, want first %d then %d", list.Messages, newID, oldID)
	}
}

func TestGetMessage_Exists(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "html-multipart.eml", "Detail Message", "recipient@example.com"))
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", messageID), cookies)
	msg := decodeJSON[messageResponse](t, resp)
	if msg.ID != messageID || !strings.Contains(msg.Subject, "Detail Message") || msg.TextBody == "" || msg.HTMLBody == "" || len(msg.Recipients) != 1 {
		t.Fatalf("message detail = %+v", msg)
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, "/api/messages/99999", cookies)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestGetMessage_Unauthorized(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	messageID := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Private", "owner@example.com"))
	cookies := login(t, suite, NormalUserPSK, "other@example.com")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", messageID), cookies)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestGetRawMIME(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := uniqueMessage(t, "simple-text.eml", "Raw MIME", "recipient@example.com")
	messageID := ingestRaw(t, suite, raw)
	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d/raw", messageID), cookies)
	body := assertStatus(t, resp, http.StatusOK)
	if resp.Header.Get("Content-Type") != "message/rfc822" || !bytes.Equal(body, raw) {
		t.Fatalf("raw response content-type=%q body=%q", resp.Header.Get("Content-Type"), body)
	}
}

func withBody(raw []byte, body string) []byte {
	text := string(raw)
	sep := "\r\n\r\n"
	if idx := strings.Index(text, sep); idx >= 0 {
		return []byte(text[:idx+len(sep)] + body + "\r\n")
	}
	sep = "\n\n"
	if idx := strings.Index(text, sep); idx >= 0 {
		return []byte(text[:idx+len(sep)] + body + "\n")
	}
	return raw
}
