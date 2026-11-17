package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the application.
type Config struct {
	DatabaseURL       string
	DataDir           string
	PublicBaseURL     string
	MaxMessageBytes   int64
	SessionCookieName string
	SessionTTLHours   int
	WorkerIngestPSK   string
	ServerAddr        string
	AppEnv            string

	// AuthProviders lists the sign-in methods that may be used: "cf_access"
	// and/or "google". It defaults to cf_access only, so Google stays off
	// even when its credentials are present until it is listed here.
	AuthProviders []string

	// AdminPSK is an optional emergency back door: when set, POST /api/login
	// with this key yields an admin session. Leave empty to disable.
	AdminPSK string

	// Google OAuth (OIDC authorization-code flow). Enabled when both are set.
	GoogleClientID     string
	GoogleClientSecret string
	// GoogleAllowedDomains optionally restricts Google logins to these email
	// domains. Empty means any verified Google account may sign in (it still
	// needs mailbox grants to see anything).
	GoogleAllowedDomains []string

	// Cloudflare Access (Zero Trust). Enabled when both are set.
	CFAccessTeamDomain string
	CFAccessAUD        string

	// AdminEmails are always admins, regardless of the users table.
	AdminEmails []string
}

// Load reads configuration from environment variables with sensible defaults
// and validates the authentication settings.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		DataDir:              getEnv("DATA_DIR", "./data"),
		PublicBaseURL:        getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		MaxMessageBytes:      getEnvInt("MAX_MESSAGE_BYTES", 26214400),
		SessionCookieName:    getEnv("SESSION_COOKIE_NAME", "lite_mail_session"),
		SessionTTLHours:      int(getEnvInt("SESSION_TTL_HOURS", 24)),
		WorkerIngestPSK:      getEnv("WORKER_INGEST_PSK", ""),
		ServerAddr:           getEnv("SERVER_ADDR", ":8080"),
		AppEnv:               getEnv("APP_ENV", "production"),
		AdminPSK:             getEnv("ADMIN_PSK", ""),
		GoogleClientID:       getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:   getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleAllowedDomains: splitCSV(getEnv("GOOGLE_ALLOWED_DOMAINS", "")),
		CFAccessTeamDomain:   strings.TrimSpace(getEnv("CF_ACCESS_TEAM_DOMAIN", "")),
		CFAccessAUD:          strings.TrimSpace(getEnv("CF_ACCESS_AUD", "")),
		AdminEmails:          splitCSV(getEnv("ADMIN_EMAILS", "")),
		AuthProviders:        splitCSV(getEnv("AUTH_PROVIDERS", DefaultAuthProviders)),
	}
	if os.Getenv("NORMAL_USER_PSK") != "" {
		slog.Warn("NORMAL_USER_PSK is set but no longer used; normal users sign in with Google or Cloudflare Access")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.providerListed(ProviderCFAccess) && !cfg.CFAccessEnabled() {
		slog.Warn("AUTH_PROVIDERS lists cf_access but CF_ACCESS_TEAM_DOMAIN/CF_ACCESS_AUD are not set; Cloudflare Access sign-in is off")
	}
	if !cfg.providerListed(ProviderGoogle) && (cfg.GoogleClientID != "" || cfg.GoogleClientSecret != "") {
		slog.Warn("Google credentials are set but AUTH_PROVIDERS does not list google; Google sign-in is off")
	}
	if len(cfg.AdminEmails) == 0 {
		slog.Warn("ADMIN_EMAILS is empty; only ADMIN_PSK or admins stored in the database can administer users")
	}
	return cfg, nil
}

// Sign-in providers that AUTH_PROVIDERS may list.
const (
	ProviderCFAccess = "cf_access"
	ProviderGoogle   = "google"
	// DefaultAuthProviders enables Cloudflare Access (Zero Trust) only.
	DefaultAuthProviders = ProviderCFAccess
)

// Validate checks that at least one way to sign in is configured and that
// provider settings are complete.
func (c *Config) Validate() error {
	var errs []error
	for _, p := range c.AuthProviders {
		if p != ProviderCFAccess && p != ProviderGoogle {
			errs = append(errs, fmt.Errorf("AUTH_PROVIDERS: unknown provider %q (use cf_access and/or google)", p))
		}
	}
	if slices.Contains(c.AuthProviders, ProviderGoogle) && !c.GoogleEnabled() {
		errs = append(errs, errors.New("AUTH_PROVIDERS lists google but GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET are not set"))
	}

	if (c.GoogleClientID == "") != (c.GoogleClientSecret == "") {
		errs = append(errs, errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set together"))
	}
	if (c.CFAccessTeamDomain == "") != (c.CFAccessAUD == "") {
		errs = append(errs, errors.New("CF_ACCESS_TEAM_DOMAIN and CF_ACCESS_AUD must be set together"))
	}
	if !c.GoogleEnabled() && !c.CFAccessEnabled() && c.AdminPSK == "" {
		errs = append(errs, errors.New("no authentication configured: set CF_ACCESS_TEAM_DOMAIN/CF_ACCESS_AUD (or list google in AUTH_PROVIDERS with its credentials), or ADMIN_PSK"))
	}
	if c.GoogleEnabled() && c.AppEnv != "development" {
		u, err := url.Parse(c.PublicBaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL must be an https URL when Google sign-in is enabled (got %q)", c.PublicBaseURL))
		}
	}
	return errors.Join(errs...)
}

// GoogleEnabled reports whether Google sign-in is listed in AUTH_PROVIDERS
// and has credentials.
func (c *Config) GoogleEnabled() bool {
	return c.providerListed(ProviderGoogle) && c.GoogleClientID != "" && c.GoogleClientSecret != ""
}

// CFAccessEnabled reports whether Cloudflare Access JWTs are accepted.
func (c *Config) CFAccessEnabled() bool {
	return c.providerListed(ProviderCFAccess) && c.CFAccessTeamDomain != "" && c.CFAccessAUD != ""
}

// providerListed reports whether p is enabled. A Config built in code with no
// AuthProviders (tests, embedding) behaves as if every provider were listed,
// so the credentials alone decide.
func (c *Config) providerListed(p string) bool {
	return c.AuthProviders == nil || slices.Contains(c.AuthProviders, p)
}

// CFAccessIssuer returns the issuer URL for the configured team domain, which
// may be given as "myteam" or "myteam.cloudflareaccess.com".
func (c *Config) CFAccessIssuer() string {
	team := strings.TrimSuffix(strings.TrimPrefix(c.CFAccessTeamDomain, "https://"), "/")
	if team == "" {
		return ""
	}
	if !strings.Contains(team, ".") {
		team += ".cloudflareaccess.com"
	}
	return "https://" + team
}

// IsEnvAdmin reports whether email is listed in ADMIN_EMAILS.
func (c *Config) IsEnvAdmin(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	return email != "" && slices.Contains(c.AdminEmails, email)
}

// IsDevelopment reports whether APP_ENV=development (non-Secure cookies).
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

// splitCSV splits a comma-separated list, lowercasing and dropping empties.
func splitCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" && !slices.Contains(out, part) {
			out = append(out, part)
		}
	}
	return out
}

// SessionTTL returns the session TTL as a time.Duration.
func (c *Config) SessionTTL() time.Duration {
	return time.Duration(c.SessionTTLHours) * time.Hour
}
