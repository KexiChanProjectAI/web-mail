package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("DATA_DIR")
	os.Unsetenv("PUBLIC_BASE_URL")
	os.Unsetenv("MAX_MESSAGE_BYTES")
	os.Unsetenv("SESSION_COOKIE_NAME")
	os.Unsetenv("SESSION_TTL_HOURS")
	os.Unsetenv("NORMAL_USER_PSK")
	os.Unsetenv("ADMIN_PSK")
	os.Unsetenv("WORKER_INGEST_PSK")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MaxMessageBytes != 26214400 {
		t.Errorf("MaxMessageBytes = %d, want 26214400", cfg.MaxMessageBytes)
	}
	if cfg.SessionCookieName != "lite_mail_session" {
		t.Errorf("SessionCookieName = %s, want lite_mail_session", cfg.SessionCookieName)
	}
	if cfg.SessionTTLHours != 24 {
		t.Errorf("SessionTTLHours = %d, want 24", cfg.SessionTTLHours)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %s, want ./data", cfg.DataDir)
	}
	if cfg.PublicBaseURL != "http://localhost:8080" {
		t.Errorf("PublicBaseURL = %s, want http://localhost:8080", cfg.PublicBaseURL)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	os.Setenv("MAX_MESSAGE_BYTES", "1048576")
	os.Setenv("SESSION_COOKIE_NAME", "custom_session")
	os.Setenv("SESSION_TTL_HOURS", "48")
	defer func() {
		os.Unsetenv("MAX_MESSAGE_BYTES")
		os.Unsetenv("SESSION_COOKIE_NAME")
		os.Unsetenv("SESSION_TTL_HOURS")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MaxMessageBytes != 1048576 {
		t.Errorf("MaxMessageBytes = %d, want 1048576", cfg.MaxMessageBytes)
	}
	if cfg.SessionCookieName != "custom_session" {
		t.Errorf("SessionCookieName = %s, want custom_session", cfg.SessionCookieName)
	}
	if cfg.SessionTTLHours != 48 {
		t.Errorf("SessionTTLHours = %d, want 48", cfg.SessionTTLHours)
	}
}

func TestSessionTTL(t *testing.T) {
	cfg := &Config{SessionTTLHours: 12}
	got := cfg.SessionTTL()
	want := int64(12 * 3600 * 1000000000)
	if int64(got) != want {
		t.Errorf("SessionTTL() = %d ns, want %d ns", int64(got), want)
	}
}
