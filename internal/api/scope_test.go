package api

import (
	"reflect"
	"testing"

	"lite-mail/internal/auth"
)

func TestRecipientPredicate(t *testing.T) {
	grants := func(p ...string) *auth.Session { return &auth.Session{Grants: auth.ParseGrants(p)} }
	cases := []struct {
		name     string
		session  *auth.Session
		mailbox  string
		wantSQL  string
		wantArgs []any
	}{
		{"admin unrestricted", &auth.Session{IsAdmin: true}, "", "", nil},
		{"admin with mailbox", &auth.Session{IsAdmin: true}, "x@y.z", "mr.recipient_email = ?", []any{"x@y.z"}},
		{"nil session", nil, "", "0 = 1", nil},
		{"no grants", grants(), "", "0 = 1", nil},
		{"exact only", grants("a@x.io", "b@x.io"), "", "(mr.recipient_email IN (?,?))", []any{"a@x.io", "b@x.io"}},
		{"domain only", grants("*@x.io"), "", "(mr.recipient_email LIKE ?)", []any{"%@x.io"}},
		{"mixed", grants("*@y.io", "a@x.io"), "", "(mr.recipient_email IN (?) OR mr.recipient_email LIKE ?)", []any{"a@x.io", "%@y.io"}},
		{"granted mailbox", grants("*@y.io"), "q@y.io", "mr.recipient_email = ?", []any{"q@y.io"}},
		{"ungranted mailbox", grants("*@y.io"), "q@z.io", "0 = 1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := recipientPredicate(tc.session, tc.mailbox)
			if sql != tc.wantSQL || !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("got (%q, %v), want (%q, %v)", sql, args, tc.wantSQL, tc.wantArgs)
			}
		})
	}
}

func TestScopeClauseAndWhere(t *testing.T) {
	if sql, args := scopeClause(&auth.Session{IsAdmin: true}, ""); sql != "" || args != nil {
		t.Fatalf("admin scope = %q %v", sql, args)
	}
	sql, _ := scopeClause(&auth.Session{}, "")
	if sql != "EXISTS (SELECT 1 FROM message_recipients mr WHERE mr.message_id = m.id AND "+
		"(mr.recipient_type <> 'envelope' OR NOT EXISTS (SELECT 1 FROM message_recipients hdr WHERE hdr.message_id = m.id AND hdr.recipient_type <> 'envelope'))"+
		" AND 0 = 1)" {
		t.Fatalf("empty scope = %q", sql)
	}
	if got := whereClause("", "a = 1", "", "b = 2"); got != " WHERE a = 1 AND b = 2" {
		t.Fatalf("whereClause = %q", got)
	}
	if got := whereClause("", ""); got != "" {
		t.Fatalf("empty whereClause = %q", got)
	}
}
