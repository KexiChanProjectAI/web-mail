package auth

import (
	"errors"
	"log/slog"
	"strings"

	"lite-mail/internal/emailaddr"
)

// GrantKind says whether a grant names one recipient address or a whole domain.
type GrantKind string

const (
	GrantExact  GrantKind = "exact"
	GrantDomain GrantKind = "domain"
)

// Grant lets a user read mail delivered to one address or to any address of a domain.
type Grant struct {
	Kind GrantKind
	// Value is the lowercase address for GrantExact, or the lowercase domain for GrantDomain.
	Value string
	// Pattern is the canonical stored form: "alice@example.com" or "*@example.com".
	Pattern string
}

// ErrInvalidGrant is returned for a pattern that is neither local@domain nor *@domain.
var ErrInvalidGrant = errors.New("invalid grant pattern: use alice@example.com or *@example.com")

// ParseGrant validates and canonicalises a grant pattern.
func ParseGrant(pattern string) (Grant, error) {
	p := emailaddr.Canonical(pattern)
	if strings.Count(p, "@") != 1 {
		return Grant{}, ErrInvalidGrant
	}
	local, domain, _ := strings.Cut(p, "@")
	if !validDomain(domain) || local == "" {
		return Grant{}, ErrInvalidGrant
	}
	if local == "*" {
		return Grant{Kind: GrantDomain, Value: domain, Pattern: "*@" + domain}, nil
	}
	if !validLocal(local) {
		return Grant{}, ErrInvalidGrant
	}
	return Grant{Kind: GrantExact, Value: p, Pattern: p}, nil
}

// ParseGrants parses stored patterns, skipping (and logging) invalid ones.
func ParseGrants(patterns []string) []Grant {
	grants := make([]Grant, 0, len(patterns))
	for _, pattern := range patterns {
		g, err := ParseGrant(pattern)
		if err != nil {
			slog.Warn("ignoring invalid stored mailbox grant", "pattern", pattern)
			continue
		}
		grants = append(grants, g)
	}
	return grants
}

// Matches reports whether a canonical recipient address falls under the grant.
func (g Grant) Matches(address string) bool {
	switch g.Kind {
	case GrantExact:
		return address == g.Value
	case GrantDomain:
		return emailaddr.Domain(address) == g.Value && strings.Count(address, "@") == 1
	}
	return false
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return false
	}
	for _, r := range domain {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// validLocal rejects whitespace, control characters, '*' (reserved for domain
// grants) and quoting characters. Exact grants are compared with '=' / IN, never
// LIKE, so '_' and '%' are harmless and allowed.
func validLocal(local string) bool {
	if len(local) > 64 {
		return false
	}
	for _, r := range local {
		if r <= ' ' || r == 0x7f || r == '*' || r == '\\' || r == '"' || r == ',' {
			return false
		}
	}
	return true
}
