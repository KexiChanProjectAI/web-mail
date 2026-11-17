package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestLogin_AdminPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := doRequest(t, suite, http.MethodPost, "/api/login", nil, []byte(`{"psk":"test-admin-psk"}`))
	body := decodeJSON[struct {
		Status  string `json:"status"`
		IsAdmin bool   `json:"is_admin"`
		Email   string `json:"email"`
	}](t, resp)
	if body.Status != "ok" || !body.IsAdmin || body.Email != "admin" {
		t.Fatalf("login body = %+v", body)
	}
	if len(resp.Cookies()) == 0 || resp.Cookies()[0].Name != suite.config.SessionCookieName {
		t.Fatalf("session cookie missing: %+v", resp.Cookies())
	}
}

func TestLogin_NormalUserPSKRemoved(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := doRequest(t, suite, http.MethodPost, "/api/login", nil, []byte(`{"psk":"test-normal-psk","email":"test@example.com"}`))
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogin_InvalidPSK(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := doRequest(t, suite, http.MethodPost, "/api/login", nil, []byte(`{"psk":"wrong"}`))
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogin_CrossSiteRefused(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader([]byte(`{"psk":"test-admin-psk"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	assertStatus(t, resp, http.StatusForbidden)
}

func TestLogout(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	cookies := login(t, suite, AdminPSK)
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

	cookies := login(t, suite, AdminPSK)
	resp := getWithCookies(t, suite, "/api/messages", cookies)
	assertStatus(t, resp, http.StatusOK)
}

func TestProtectedEndpoint_WithoutAuth(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	resp := getWithCookies(t, suite, "/api/messages", nil)
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestAccessUser_ExactGrantScoped(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Visible to normal", TestUserEmail))
	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Hidden from normal", "other@example.com"))
	cookies := asUser(t, suite, "reader@corp.example", TestUserEmail)
	resp := getWithCookies(t, suite, "/api/messages", cookies)
	list := decodeJSON[listResponse](t, resp)
	if list.Total != 1 || len(list.Messages) != 1 {
		t.Fatalf("scoped list = %+v", list)
	}
	if list.Messages[0].Recipients != nil {
		t.Fatalf("list response unexpectedly included recipients: %+v", list.Messages[0])
	}
}

func TestAccessUser_DomainGrantScoped(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Domain one", "a@team.example"))
	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Domain two", "b@team.example"))
	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Elsewhere", "c@other.example"))
	cookies := asUser(t, suite, "reader@corp.example", "*@team.example")
	list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", cookies))
	if list.Total != 2 {
		t.Fatalf("domain grant list = %+v", list)
	}
	filtered := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages?mailbox=b@team.example", cookies))
	if filtered.Total != 1 {
		t.Fatalf("mailbox filter list = %+v", filtered)
	}
	assertStatus(t, getWithCookies(t, suite, "/api/messages?mailbox=c@other.example", cookies), http.StatusForbidden)
}

func TestAccessUser_NoGrantsSeesNothing(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	id := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Own address", "stranger@example.com"))
	// Signed in with the very address the mail was sent to, but no grant: no implicit access.
	cookies := accessCookies(t, suite, "stranger@example.com")
	list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", cookies))
	if list.Total != 0 || len(list.Messages) != 0 {
		t.Fatalf("ungranted list = %+v", list)
	}
	for _, path := range []string{
		fmt.Sprintf("/api/messages/%d", id),
		fmt.Sprintf("/api/messages/%d/raw", id),
		fmt.Sprintf("/api/messages/%d/attachments/0", id),
	} {
		assertStatus(t, getWithCookies(t, suite, path, cookies), http.StatusNotFound)
	}
	// The visit is recorded so an admin can find and grant the user.
	var count int
	if err := suite.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ? AND last_login_provider = 'cf_access'", "stranger@example.com").Scan(&count); err != nil || count != 1 {
		t.Fatalf("users row count = %d err = %v", count, err)
	}
}

func TestAccessUser_InvalidToken(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	forged := []*http.Cookie{{Name: "CF_Authorization", Value: suite.jwks.Mint(t, map[string]any{
		"iss": AccessIssuer, "aud": []string{"another-app"}, "email": "x@example.com", "exp": time.Now().Add(time.Hour).Unix(),
	})}}
	assertStatus(t, getWithCookies(t, suite, "/api/messages", forged), http.StatusUnauthorized)
	headerOnly := doRequest(t, suite, http.MethodGet, "/api/me", nil, nil)
	assertStatus(t, headerOnly, http.StatusUnauthorized)
}

func TestEnvAdmin_SeesAllViaAccess(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "One", "a@x.example"))
	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Two", "b@y.example"))
	cookies := accessCookies(t, suite, EnvAdminEmail)
	list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", cookies))
	if list.Total != 2 {
		t.Fatalf("env admin list = %+v", list)
	}
	me := decodeJSON[struct {
		Email      string `json:"email"`
		IsAdmin    bool   `json:"is_admin"`
		IsEnvAdmin bool   `json:"is_env_admin"`
		Provider   string `json:"provider"`
	}](t, getWithCookies(t, suite, "/api/me", cookies))
	if me.Email != EnvAdminEmail || !me.IsAdmin || !me.IsEnvAdmin || me.Provider != "cf_access" {
		t.Fatalf("me = %+v", me)
	}
}

func TestMe_GrantsAndProviders(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	cookies := asUser(t, suite, "reader@corp.example", "box@team.example", "*@team.example")
	me := decodeJSON[struct {
		IsAdmin bool `json:"is_admin"`
		Grants  []struct {
			Pattern string `json:"pattern"`
			Kind    string `json:"kind"`
		} `json:"grants"`
	}](t, getWithCookies(t, suite, "/api/me", cookies))
	if me.IsAdmin || len(me.Grants) != 2 || me.Grants[1].Kind != "domain" {
		t.Fatalf("me = %+v", me)
	}
	providers := decodeJSON[map[string]bool](t, getWithCookies(t, suite, "/api/auth/providers", nil))
	if providers["google"] || !providers["cf_access"] || !providers["admin_psk"] {
		t.Fatalf("providers = %v", providers)
	}
}
