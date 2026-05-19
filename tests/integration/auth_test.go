package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLogin_AdminPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader([]byte(`{"psk":"test-admin-psk"}`)))
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	var body struct {
		Status  string `json:"status"`
		IsAdmin bool   `json:"is_admin"`
		Email   string `json:"email"`
	}
	data := decodeJSON[struct {
		Status  string `json:"status"`
		IsAdmin bool   `json:"is_admin"`
		Email   string `json:"email"`
	}](t, resp)
	body = data
	if body.Status != "ok" || !body.IsAdmin || body.Email != "admin" {
		t.Fatalf("login body = %+v", body)
	}
	if len(resp.Cookies()) == 0 || resp.Cookies()[0].Name != suite.config.SessionCookieName {
		t.Fatalf("session cookie missing: %+v", resp.Cookies())
	}
}

func TestLogin_NormalPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader([]byte(`{"psk":"test-normal-psk","email":"test@example.com"}`)))
	if err != nil {
		t.Fatalf("login normal: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		IsAdmin bool   `json:"is_admin"`
		Email   string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.IsAdmin || body.Email != TestUserEmail || len(resp.Cookies()) == 0 {
		t.Fatalf("status=%d body=%+v cookies=%+v", resp.StatusCode, body, resp.Cookies())
	}
}

func TestLogin_NormalPSK_NoEmail(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader([]byte(`{"psk":"test-normal-psk"}`)))
	if err != nil {
		t.Fatalf("login normal no email: %v", err)
	}
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogin_InvalidPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader([]byte(`{"psk":"wrong","email":"test@example.com"}`)))
	if err != nil {
		t.Fatalf("login invalid: %v", err)
	}
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogout(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	cookies := login(t, suite, AdminPSK, "")
	resp := postWithCookies(t, suite, "/api/logout", cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	cleared := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == suite.config.SessionCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout did not clear cookie: %+v", resp.Cookies())
	}
}

func TestProtectedEndpoint_WithAuth(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	cookies := login(t, suite, AdminPSK, "")
	resp := getWithCookies(t, suite, "/api/messages", cookies)
	assertStatus(t, resp, http.StatusOK)
}

func TestProtectedEndpoint_WithoutAuth(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := getWithCookies(t, suite, "/api/messages", nil)
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestNormalUser_ScopedAccess(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Visible to normal", TestUserEmail))
	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Hidden from normal", "other@example.com"))
	cookies := login(t, suite, NormalUserPSK, TestUserEmail)
	resp := getWithCookies(t, suite, "/api/messages", cookies)
	list := decodeJSON[listResponse](t, resp)
	if list.Total != 1 || len(list.Messages) != 1 {
		t.Fatalf("normal scoped list = %+v", list)
	}
	if list.Messages[0].Recipients != nil {
		t.Fatalf("list response unexpectedly included recipients: %+v", list.Messages[0])
	}
}
