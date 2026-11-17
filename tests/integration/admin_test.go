package integration

import (
	"fmt"
	"net/http"
	"testing"
)

type adminUser struct {
	ID         int64  `json:"id"`
	Email      string `json:"email"`
	IsAdmin    bool   `json:"is_admin"`
	IsEnvAdmin bool   `json:"is_env_admin"`
	Grants     []struct {
		ID        int64   `json:"id"`
		Pattern   string  `json:"pattern"`
		Kind      string  `json:"kind"`
		CreatedBy *string `json:"created_by"`
	} `json:"grants"`
}

type adminUserList struct {
	Users     []adminUser `json:"users"`
	EnvAdmins []string    `json:"env_admins"`
}

func TestAdmin_GrantLifecycleChangesAccess(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "For the team", "inbox@team.example"))
	admin := accessCookies(t, suite, EnvAdminEmail)
	reader := accessCookies(t, suite, "reader@corp.example")

	// Before any grant the reader sees nothing.
	if list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", reader)); list.Total != 0 {
		t.Fatalf("pre-grant list = %+v", list)
	}

	// The visit created a users row; creating it again conflicts.
	assertStatus(t, doRequest(t, suite, http.MethodPost, "/api/admin/users", admin, []byte(`{"email":"reader@corp.example"}`)), http.StatusConflict)
	var created *adminUser
	for _, u := range decodeJSON[adminUserList](t, getWithCookies(t, suite, "/api/admin/users", admin)).Users {
		if u.Email == "reader@corp.example" {
			created = &u
		}
	}
	if created == nil || created.IsAdmin || len(created.Grants) != 0 {
		t.Fatalf("visitor row = %+v", created)
	}

	// Users can also be added ahead of their first sign-in.
	newbie := decodeJSON[adminUser](t, assertResponse(t, doRequest(t, suite, http.MethodPost, "/api/admin/users", admin, []byte(`{"email":" Newbie@Corp.example "}`)), http.StatusCreated))
	if newbie.Email != "newbie@corp.example" || newbie.IsAdmin {
		t.Fatalf("created = %+v", newbie)
	}
	assertStatus(t, doRequest(t, suite, http.MethodPost, "/api/admin/users", admin, []byte(`{"email":"not an email"}`)), http.StatusBadRequest)

	grantPath := fmt.Sprintf("/api/admin/users/%d/grants", created.ID)
	assertStatus(t, doRequest(t, suite, http.MethodPost, grantPath, admin, []byte(`{"pattern":"not-an-address"}`)), http.StatusBadRequest)
	grant := decodeJSON[struct {
		ID        int64   `json:"id"`
		Pattern   string  `json:"pattern"`
		Kind      string  `json:"kind"`
		CreatedBy *string `json:"created_by"`
	}](t, assertResponse(t, doRequest(t, suite, http.MethodPost, grantPath, admin, []byte(`{"pattern":"*@Team.example"}`)), http.StatusCreated))
	if grant.Pattern != "*@team.example" || grant.Kind != "domain" || grant.CreatedBy == nil || *grant.CreatedBy != EnvAdminEmail {
		t.Fatalf("grant = %+v", grant)
	}
	assertStatus(t, doRequest(t, suite, http.MethodPost, grantPath, admin, []byte(`{"pattern":"*@team.example"}`)), http.StatusConflict)

	// The grant applies immediately, without a new login.
	if list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", reader)); list.Total != 1 {
		t.Fatalf("post-grant list = %+v", list)
	}

	users := decodeJSON[adminUserList](t, getWithCookies(t, suite, "/api/admin/users", admin))
	var found *adminUser
	for i := range users.Users {
		if users.Users[i].Email == "reader@corp.example" {
			found = &users.Users[i]
		}
	}
	if found == nil || len(found.Grants) != 1 || found.Grants[0].ID != grant.ID {
		t.Fatalf("users list = %+v", users)
	}

	assertStatus(t, doRequest(t, suite, http.MethodDelete, fmt.Sprintf("%s/%d", grantPath, grant.ID), admin, nil), http.StatusNoContent)
	assertStatus(t, doRequest(t, suite, http.MethodDelete, fmt.Sprintf("%s/%d", grantPath, grant.ID), admin, nil), http.StatusNotFound)
	if list := decodeJSON[listResponse](t, getWithCookies(t, suite, "/api/messages", reader)); list.Total != 0 {
		t.Fatalf("post-revoke list = %+v", list)
	}

	mailboxes := decodeJSON[struct {
		Addresses []string `json:"addresses"`
		Domains   []string `json:"domains"`
	}](t, getWithCookies(t, suite, "/api/admin/mailboxes?q=team", admin))
	if len(mailboxes.Addresses) != 1 || mailboxes.Addresses[0] != "inbox@team.example" || len(mailboxes.Domains) != 1 || mailboxes.Domains[0] != "team.example" {
		t.Fatalf("mailboxes = %+v", mailboxes)
	}

	assertStatus(t, doRequest(t, suite, http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", created.ID), admin, nil), http.StatusNoContent)
	assertStatus(t, doRequest(t, suite, http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", created.ID), admin, nil), http.StatusNotFound)
}

func TestAdmin_PromoteAndEnvAdminProtection(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	admin := login(t, suite, AdminPSK)
	userID := seedUser(t, suite, "helper@corp.example", false)
	helper := accessCookies(t, suite, "helper@corp.example")

	assertStatus(t, getWithCookies(t, suite, "/api/admin/users", helper), http.StatusForbidden)
	promoted := decodeJSON[adminUser](t, assertResponse(t, doRequest(t, suite, http.MethodPatch, fmt.Sprintf("/api/admin/users/%d", userID), admin, []byte(`{"is_admin":true}`)), http.StatusOK))
	if !promoted.IsAdmin {
		t.Fatalf("promoted = %+v", promoted)
	}
	assertStatus(t, getWithCookies(t, suite, "/api/admin/users", helper), http.StatusOK)

	list := decodeJSON[adminUserList](t, getWithCookies(t, suite, "/api/admin/users", admin))
	if len(list.EnvAdmins) != 1 || list.EnvAdmins[0] != EnvAdminEmail {
		t.Fatalf("env_admins = %v", list.EnvAdmins)
	}

	envID := seedUser(t, suite, EnvAdminEmail, false)
	assertStatus(t, doRequest(t, suite, http.MethodPatch, fmt.Sprintf("/api/admin/users/%d", envID), admin, []byte(`{"is_admin":false}`)), http.StatusConflict)
	assertStatus(t, doRequest(t, suite, http.MethodPatch, "/api/admin/users/999999", admin, []byte(`{"is_admin":true}`)), http.StatusNotFound)
}

func TestAdmin_RequiresAuthAndSameOrigin(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	assertStatus(t, getWithCookies(t, suite, "/api/admin/users", nil), http.StatusUnauthorized)
	reader := asUser(t, suite, "reader@corp.example", "*@team.example")
	assertStatus(t, getWithCookies(t, suite, "/api/admin/users", reader), http.StatusForbidden)

	admin := accessCookies(t, suite, EnvAdminEmail)
	req, err := http.NewRequest(http.MethodPost, suite.server.URL+"/api/admin/users", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	for _, c := range admin {
		req.AddCookie(c)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, http.StatusForbidden)
}

// assertResponse checks the status and hands the response back for decoding.
func assertResponse(t *testing.T, resp *http.Response, want int) *http.Response {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(t, resp)
		resp.Body.Close()
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, want, body)
	}
	return resp
}
