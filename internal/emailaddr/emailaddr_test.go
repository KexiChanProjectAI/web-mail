package emailaddr

import "testing"

func TestCanonical(t *testing.T) {
	if got := Canonical("  Alice@Example.COM \n"); got != "alice@example.com" {
		t.Fatalf("Canonical = %q", got)
	}
}

func TestDomain(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "example.com",
		"noat":              "",
		"trailing@":         "",
		"a@b@c.org":         "c.org",
	}
	for in, want := range cases {
		if got := Domain(in); got != want {
			t.Errorf("Domain(%q) = %q, want %q", in, got, want)
		}
	}
}
