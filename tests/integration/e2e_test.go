package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestE2E_FullFlow(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	raw := uniqueMessage(t, "simple-text.eml", "E2E Full Flow", "recipient@example.com")
	messageID := ingestRaw(t, suite, raw)
	cookies := login(t, suite, AdminPSK, "")

	listResp := getWithCookies(t, suite, "/api/messages", cookies)
	list := decodeJSON[listResponse](t, listResp)
	if list.Total != 1 || len(list.Messages) != 1 || list.Messages[0].ID != messageID {
		t.Fatalf("e2e list = %+v", list)
	}

	detailResp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", messageID), cookies)
	detail := decodeJSON[messageResponse](t, detailResp)
	if detail.ID != messageID || !strings.Contains(detail.Subject, "E2E Full Flow") || detail.TextBody == "" {
		t.Fatalf("e2e detail = %+v", detail)
	}

	rawResp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d/raw", messageID), cookies)
	rawBody := assertStatus(t, rawResp, http.StatusOK)
	if !bytes.Equal(rawBody, raw) {
		t.Fatalf("raw body mismatch")
	}

	logoutResp := postWithCookies(t, suite, "/api/logout", cookies)
	assertStatus(t, logoutResp, http.StatusOK)
	unauthResp := getWithCookies(t, suite, "/api/messages", cookies)
	assertStatus(t, unauthResp, http.StatusUnauthorized)
}

func TestE2E_NormalUserFlow(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	userMessageID := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "User Visible", "user@example.com"))
	otherMessageID := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Other Hidden", "other@example.com"))
	cookies := login(t, suite, NormalUserPSK, "user@example.com")

	listResp := getWithCookies(t, suite, "/api/messages", cookies)
	list := decodeJSON[listResponse](t, listResp)
	if list.Total != 1 || len(list.Messages) != 1 || list.Messages[0].ID != userMessageID {
		t.Fatalf("normal user list = %+v, userMessageID=%d", list, userMessageID)
	}

	otherResp := getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", otherMessageID), cookies)
	assertStatus(t, otherResp, http.StatusNotFound)
}
