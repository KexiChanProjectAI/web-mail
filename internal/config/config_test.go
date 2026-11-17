package config

import (
	"os"
	"strings"
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
	t.Setenv("ADMIN_PSK", "emergency")

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
	t.Setenv("ADMIN_PSK", "emergency")
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

func TestLoadAuthEnv(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://mail.example.test")
	t.Setenv("AUTH_PROVIDERS", "cf_access, Google")
	t.Setenv("GOOGLE_CLIENT_ID", "cid")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAINS", " Example.com, ,example.org,example.com")
	t.Setenv("CF_ACCESS_TEAM_DOMAIN", "myteam")
	t.Setenv("CF_ACCESS_AUD", "aud-tag")
	t.Setenv("ADMIN_EMAILS", "Boss@Example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.GoogleEnabled() || !cfg.CFAccessEnabled() {
		t.Fatalf("providers not enabled: %+v", cfg)
	}
	if got := strings.Join(cfg.GoogleAllowedDomains, ","); got != "example.com,example.org" {
		t.Fatalf("GoogleAllowedDomains = %q", got)
	}
	if !cfg.IsEnvAdmin(" boss@example.COM") || cfg.IsEnvAdmin("other@example.com") || cfg.IsEnvAdmin("") {
		t.Fatalf("IsEnvAdmin mismatch for %v", cfg.AdminEmails)
	}
	if got := cfg.CFAccessIssuer(); got != "https://myteam.cloudflareaccess.com" {
		t.Fatalf("CFAccessIssuer = %q", got)
	}
}

func TestCFAccessIssuerFullDomain(t *testing.T) {
	cfg := &Config{CFAccessTeamDomain: "https://myteam.cloudflareaccess.com/"}
	if got := cfg.CFAccessIssuer(); got != "https://myteam.cloudflareaccess.com" {
		t.Fatalf("CFAccessIssuer = %q", got)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"nothing configured", Config{}, "no authentication configured"},
		{"admin psk only", Config{AdminPSK: "x"}, ""},
		{"half google", Config{AdminPSK: "x", GoogleClientID: "id"}, "must be set together"},
		{"half cf", Config{AdminPSK: "x", CFAccessAUD: "aud"}, "must be set together"},
		{"cf only", Config{CFAccessTeamDomain: "t", CFAccessAUD: "a"}, ""},
		{"google over http in production", Config{GoogleClientID: "id", GoogleClientSecret: "s", PublicBaseURL: "http://mail.example.test"}, "https"},
		{"google over http in development", Config{GoogleClientID: "id", GoogleClientSecret: "s", PublicBaseURL: "http://localhost:8080", AppEnv: "development"}, ""},
		{"google over https", Config{GoogleClientID: "id", GoogleClientSecret: "s", PublicBaseURL: "https://mail.example.test"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestDefaultProvidersAreCloudflareAccessOnly(t *testing.T) {
	t.Setenv("AUTH_PROVIDERS", "")
	t.Setenv("PUBLIC_BASE_URL", "https://mail.example.test")
	t.Setenv("GOOGLE_CLIENT_ID", "cid")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("CF_ACCESS_TEAM_DOMAIN", "myteam")
	t.Setenv("CF_ACCESS_AUD", "aud-tag")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if strings.Join(cfg.AuthProviders, ",") != "cf_access" {
		t.Fatalf("AuthProviders = %v, want [cf_access]", cfg.AuthProviders)
	}
	if !cfg.CFAccessEnabled() || cfg.GoogleEnabled() {
		t.Fatalf("default must enable Cloudflare Access only: cf=%v google=%v", cfg.CFAccessEnabled(), cfg.GoogleEnabled())
	}
}

func TestAuthProvidersValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"unknown provider", Config{AuthProviders: []string{"github"}, AdminPSK: "x"}, "unknown provider"},
		{"google listed without credentials", Config{AuthProviders: []string{"google"}, AdminPSK: "x"}, "lists google"},
		{"cf listed but unset, psk fallback", Config{AuthProviders: []string{"cf_access"}, AdminPSK: "x"}, ""},
		{"cf listed but unset, nothing else", Config{AuthProviders: []string{"cf_access"}}, "no authentication configured"},
		{"google creds without listing do not count", Config{AuthProviders: []string{"cf_access"}, GoogleClientID: "id", GoogleClientSecret: "s"}, "no authentication configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
