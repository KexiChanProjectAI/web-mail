package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"lite-mail/internal/testutil/jwttest"
)

type auditRow struct {
	Actor, Provider, SessionKey, Event, Mailbox, Detail, ClientIP string
	MessageID                                                     int64
}

func auditRows(t *testing.T, suite *IntegrationTestSuite, where string, args ...any) []auditRow {
	t.Helper()
	query := `SELECT actor_email, provider, session_key, event, COALESCE(mailbox,''), COALESCE(detail,''), COALESCE(client_ip,''), COALESCE(message_id,0)
FROM audit_events`
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := suite.db.Query(query+" ORDER BY id", args...)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.Actor, &r.Provider, &r.SessionKey, &r.Event, &r.Mailbox, &r.Detail, &r.ClientIP, &r.MessageID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func getWithHeaders(t *testing.T, suite *IntegrationTestSuite, path string, cookies []*http.Cookie, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, suite.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAudit_AccessSessionIsRecorded(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	own := ingestRaw(t, suite, uniqueMessage(t, "attachment-multipart.eml", "Team mail", "box@team.example"))
	other := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Private", "ceo@private.example"))
	cookies := asUser(t, suite, "reader@corp.example", "*@team.example")
	ip := map[string]string{"Cf-Connecting-Ip": "198.51.100.7"}

	// Several requests of one Access session: one sign_in, one throttled listing.
	for i := 0; i < 3; i++ {
		assertStatus(t, getWithHeaders(t, suite, "/api/messages", cookies, ip), http.StatusOK)
	}
	assertStatus(t, getWithHeaders(t, suite, "/api/messages?mailbox=box@team.example", cookies, ip), http.StatusOK)
	assertStatus(t, getWithHeaders(t, suite, fmt.Sprintf("/api/messages/%d", own), cookies, ip), http.StatusOK)
	assertStatus(t, getWithHeaders(t, suite, fmt.Sprintf("/api/messages/%d/raw", own), cookies, ip), http.StatusOK)
	assertStatus(t, getWithHeaders(t, suite, fmt.Sprintf("/api/messages/%d/attachments/0", own), cookies, ip), http.StatusOK)
	assertStatus(t, getWithHeaders(t, suite, fmt.Sprintf("/api/messages/%d/attachments/9", own), cookies, ip), http.StatusNotFound)
	assertStatus(t, getWithHeaders(t, suite, fmt.Sprintf("/api/messages/%d", other), cookies, ip), http.StatusNotFound)
	assertStatus(t, getWithHeaders(t, suite, "/api/messages?mailbox=ceo@private.example", cookies, ip), http.StatusForbidden)

	rows := auditRows(t, suite, "actor_email = ?", "reader@corp.example")
	count := map[string]int{}
	for _, r := range rows {
		count[r.Event]++
		if r.Provider != "cf_access" || !strings.HasPrefix(r.SessionKey, "cf-") || r.ClientIP != "198.51.100.7" {
			t.Fatalf("row identity fields = %+v", r)
		}
		if r.SessionKey != rows[0].SessionKey {
			t.Fatalf("one Access session must keep one session key: %+v", rows)
		}
	}
	want := map[string]int{"sign_in": 1, "mailbox_list": 2, "message_view": 1, "raw_download": 1, "attachment_download": 1, "access_denied": 2}
	for ev, n := range want {
		if count[ev] != n {
			t.Fatalf("event %s count = %d, want %d; all = %v", ev, count[ev], n, count)
		}
	}
	for _, r := range rows {
		switch r.Event {
		case "sign_in":
			if r.Detail != "grants: *@team.example" {
				t.Fatalf("sign_in detail = %q", r.Detail)
			}
		case "message_view", "raw_download", "attachment_download":
			if r.Mailbox != "box@team.example" || r.MessageID != own {
				t.Fatalf("%s row = %+v", r.Event, r)
			}
		case "access_denied":
			if !(r.MessageID == other || r.Mailbox == "ceo@private.example") {
				t.Fatalf("access_denied row = %+v", r)
			}
		}
	}

	// A new Access sign-in (new iat) is a new session.
	claims := jwttest.AccessClaims(AccessIssuer, AccessAUD, "reader@corp.example")
	claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
	second := []*http.Cookie{{Name: "CF_Authorization", Value: suite.jwks.Mint(t, claims)}}
	assertStatus(t, getWithCookies(t, suite, "/api/me", second), http.StatusOK)
	if n := len(auditRows(t, suite, "actor_email = ? AND event = 'sign_in'", "reader@corp.example")); n != 2 {
		t.Fatalf("sign_in rows after second session = %d, want 2", n)
	}
}

func TestAudit_PSKAdminAndAdminChanges(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	assertStatus(t, doRequest(t, suite, http.MethodPost, "/api/login", nil, []byte(`{"psk":"wrong"}`)), http.StatusUnauthorized)
	admin := login(t, suite, AdminPSK)
	userID := seedUser(t, suite, "helper@corp.example", false)
	resp := doRequest(t, suite, http.MethodPost, fmt.Sprintf("/api/admin/users/%d/grants", userID), admin, []byte(`{"pattern":"*@team.example"}`))
	grant := decodeJSON[struct {
		ID int64 `json:"id"`
	}](t, resp)
	assertStatus(t, doRequest(t, suite, http.MethodPatch, fmt.Sprintf("/api/admin/users/%d", userID), admin, []byte(`{"is_admin":true}`)), http.StatusOK)
	assertStatus(t, doRequest(t, suite, http.MethodDelete, fmt.Sprintf("/api/admin/users/%d/grants/%d", userID, grant.ID), admin, nil), http.StatusNoContent)
	assertStatus(t, postWithCookies(t, suite, "/api/logout", admin), http.StatusOK)

	rows := auditRows(t, suite, "")
	var events []string
	for _, r := range rows {
		events = append(events, r.Event)
	}
	got := strings.Join(events, ",")
	if got != "sign_in_failed,sign_in,admin_grant_add,admin_role_change,admin_grant_remove,sign_out" {
		t.Fatalf("events = %s", got)
	}
	if rows[0].Provider != "admin_psk" || rows[1].Actor != "admin" || !strings.HasPrefix(rows[1].SessionKey, "s-") {
		t.Fatalf("psk rows = %+v %+v", rows[0], rows[1])
	}
	if rows[2].Mailbox != "*@team.example" || rows[2].Detail != "granted to helper@corp.example" ||
		rows[4].Mailbox != "*@team.example" || rows[3].Detail != "made admin: helper@corp.example" {
		t.Fatalf("admin rows = %+v", rows[2:5])
	}
	if rows[5].SessionKey != rows[1].SessionKey {
		t.Fatalf("sign_out session key %q != sign_in %q", rows[5].SessionKey, rows[1].SessionKey)
	}
}

func TestAudit_AdminListAPI(t *testing.T) {
	suite := SetupSuite(t)
	defer TeardownSuite(suite)

	id := ingestRaw(t, suite, uniqueMessage(t, "simple-text.eml", "Audit me", "box@team.example"))
	reader := asUser(t, suite, "reader@corp.example", "box@team.example")
	assertStatus(t, getWithCookies(t, suite, fmt.Sprintf("/api/messages/%d", id), reader), http.StatusOK)
	assertStatus(t, getWithCookies(t, suite, "/api/admin/audit", reader), http.StatusForbidden)

	admin := accessCookies(t, suite, EnvAdminEmail)
	type listResp struct {
		Events []struct {
			ID        int64  `json:"id"`
			Actor     string `json:"actor"`
			Event     string `json:"event"`
			Mailbox   string `json:"mailbox"`
			MessageID int64  `json:"message_id"`
		} `json:"events"`
		NextBefore int64    `json:"next_before"`
		EventTypes []string `json:"event_types"`
	}
	all := decodeJSON[listResp](t, getWithCookies(t, suite, "/api/admin/audit?actor=reader", admin))
	if len(all.Events) != 2 || all.Events[0].Event != "message_view" || all.Events[1].Event != "sign_in" || len(all.EventTypes) == 0 {
		t.Fatalf("actor filter = %+v", all)
	}
	views := decodeJSON[listResp](t, getWithCookies(t, suite, "/api/admin/audit?mailbox=team.example&event=message_view", admin))
	if len(views.Events) != 1 || views.Events[0].MessageID != id || views.Events[0].Actor != "reader@corp.example" {
		t.Fatalf("mailbox+event filter = %+v", views)
	}
	page := decodeJSON[listResp](t, getWithCookies(t, suite, "/api/admin/audit?limit=1", admin))
	if len(page.Events) != 1 || page.NextBefore == 0 {
		t.Fatalf("page = %+v", page)
	}
	next := decodeJSON[listResp](t, getWithCookies(t, suite, fmt.Sprintf("/api/admin/audit?limit=1&before=%d", page.NextBefore), admin))
	if len(next.Events) != 1 || next.Events[0].ID >= page.NextBefore {
		t.Fatalf("next page = %+v", next)
	}
	future := decodeJSON[listResp](t, getWithCookies(t, suite, "/api/admin/audit?since=2999-01-01", admin))
	if len(future.Events) != 0 {
		t.Fatalf("since filter = %+v", future)
	}
	assertStatus(t, getWithCookies(t, suite, "/api/admin/audit?event=bogus", admin), http.StatusBadRequest)
	assertStatus(t, getWithCookies(t, suite, "/api/admin/audit?since=yesterday", admin), http.StatusBadRequest)
}
