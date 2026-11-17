package auth

import "testing"

func TestParseGrant(t *testing.T) {
	valid := map[string]Grant{
		"Alice@Example.com":          {Kind: GrantExact, Value: "alice@example.com", Pattern: "alice@example.com"},
		" *@Example.COM ":            {Kind: GrantDomain, Value: "example.com", Pattern: "*@example.com"},
		"first_last@sub.example.org": {Kind: GrantExact, Value: "first_last@sub.example.org", Pattern: "first_last@sub.example.org"},
		"a+tag@x-y.io":               {Kind: GrantExact, Value: "a+tag@x-y.io", Pattern: "a+tag@x-y.io"},
	}
	for in, want := range valid {
		got, err := ParseGrant(in)
		if err != nil || got != want {
			t.Errorf("ParseGrant(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "alice", "@example.com", "*@", "a@b@c.com", "*@*", "*@example", "a b@example.com",
		"*x@example.com", "a@exa_mple.com", "a@exa%mple.com", "a@.example.com", "a@example..com", `a"@example.com`} {
		if g, err := ParseGrant(in); err == nil {
			t.Errorf("ParseGrant(%q) = %+v, want error", in, g)
		}
	}
}

func TestGrantMatchesAndSessionAccess(t *testing.T) {
	s := &Session{Grants: ParseGrants([]string{"box@example.com", "*@example.org", "not a grant"})}
	if len(s.Grants) != 2 {
		t.Fatalf("invalid stored pattern not skipped: %+v", s.Grants)
	}
	cases := map[string]bool{
		"box@example.com":        true,
		"other@example.com":      false,
		"anyone@example.org":     true,
		"anyone@sub.example.org": false,
		"x@example.org.evil":     false,
	}
	for addr, want := range cases {
		if got := s.HasMailboxAccess(addr); got != want {
			t.Errorf("HasMailboxAccess(%q) = %v, want %v", addr, got, want)
		}
	}
	if !s.HasAnyMailbox() || (&Session{}).HasAnyMailbox() || !(&Session{IsAdmin: true}).HasMailboxAccess("x@y.z") {
		t.Fatal("HasAnyMailbox/admin access mismatch")
	}
	var nilSession *Session
	if nilSession.HasMailboxAccess("a@b.c") || nilSession.HasAnyMailbox() {
		t.Fatal("nil session must have no access")
	}
}
