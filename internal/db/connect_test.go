package db

import (
	"strings"
	"testing"
)

func TestNormalizeMySQLDSNAddsParseTime(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/lite_mail"

	normalized, err := normalizeMySQLDSN(dsn)
	if err != nil {
		t.Fatalf("normalizeMySQLDSN: %v", err)
	}
	if !strings.Contains(normalized, "parseTime=true") {
		t.Fatalf("normalized dsn %q does not enable parseTime", normalized)
	}
}

func TestNormalizeMySQLDSNPreservesExistingQueryParams(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/lite_mail?charset=utf8mb4&loc=UTC"

	normalized, err := normalizeMySQLDSN(dsn)
	if err != nil {
		t.Fatalf("normalizeMySQLDSN: %v", err)
	}
	for _, want := range []string{"charset=utf8mb4", "parseTime=true"} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("normalized dsn %q missing %q", normalized, want)
		}
	}
}

func TestNormalizeMySQLDSNOverridesParseTimeFalse(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/lite_mail?parseTime=false"

	normalized, err := normalizeMySQLDSN(dsn)
	if err != nil {
		t.Fatalf("normalizeMySQLDSN: %v", err)
	}
	if !strings.Contains(normalized, "parseTime=true") {
		t.Fatalf("normalized dsn %q does not force parseTime=true", normalized)
	}
	if strings.Contains(normalized, "parseTime=false") {
		t.Fatalf("normalized dsn %q still contains parseTime=false", normalized)
	}
}
