// Package emailaddr holds the single canonical form for email addresses, shared
// by ingest (recipient rows) and auth (login identities and mailbox grants) so
// that the two always compare equal.
package emailaddr

import "strings"

// Canonical returns the lowercase, whitespace-trimmed form of an address.
func Canonical(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Domain returns the part after the last '@' of a canonical address, or "".
func Domain(email string) string {
	idx := strings.LastIndex(email, "@")
	if idx < 0 || idx == len(email)-1 {
		return ""
	}
	return email[idx+1:]
}
