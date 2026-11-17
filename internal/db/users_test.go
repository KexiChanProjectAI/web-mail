package db

import "testing"

func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"plain":      "plain",
		"50%_off":    `50\%\_off`,
		`back\slash`: `back\\slash`,
	}
	for in, want := range cases {
		if got := EscapeLike(in); got != want {
			t.Errorf("EscapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}
