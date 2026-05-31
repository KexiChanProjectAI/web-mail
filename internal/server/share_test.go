package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
)

func setupShareTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping database test")
	}

	database, err := db.Connect(dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	migrations, err := db.LoadMigrations(db.MigrationsFS)
	if err != nil {
		database.Close()
		t.Fatalf("load migrations: %v", err)
	}

	if err := db.RunMigrations(database, migrations); err != nil {
		database.Close()
		t.Fatalf("run migrations: %v", err)
	}

	return database
}

func teardownShareTestDB(t *testing.T, database *sql.DB) {
	t.Helper()
	if database == nil {
		return
	}

	migrations, err := db.LoadMigrations(db.MigrationsFS)
	if err != nil {
		t.Logf("WARNING: failed to load migrations for teardown: %v", err)
		database.Close()
		return
	}

	if err := db.RollbackMigration(database, migrations); err != nil {
		t.Logf("WARNING: failed to rollback migration: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Logf("WARNING: failed to close database: %v", err)
	}
}

func insertShareTestMessage(t *testing.T, ctx context.Context, database *sql.DB, sender, subject, textBody, htmlBody string) int64 {
	t.Helper()
	result, err := database.ExecContext(ctx,
		"INSERT INTO messages (cloudflare_message_id, content_hash, sender, subject, message_date, received_at, raw_mime_path, text_body, html_body) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"cf-share-test", "hash-share-test", sender, subject, "2025-01-15 10:30:00", "2025-01-15 10:30:00", "/raw/share-test.eml", textBody, htmlBody,
	)
	if err != nil {
		t.Fatalf("insert test message: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get last insert id: %v", err)
	}
	return id
}

func TestShareHTMLWithValidToken(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "alice@example.com", "Hello World", "Plain text body", "<p>HTML body</p>")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	// Test /share/{token} (default HTML)
	req := httptest.NewRequest(http.MethodGet, "/share/"+token, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<p>HTML body</p>") {
		t.Fatalf("body does not contain HTML content: %s", body)
	}

	// Test /share/{token}/html (explicit HTML)
	req2 := httptest.NewRequest(http.MethodGet, "/share/"+token+"/html", nil)
	rr2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr2.Code, http.StatusOK)
	}
	if !strings.HasPrefix(rr2.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", rr2.Header().Get("Content-Type"))
	}
}

func TestShareHTMLFallbackWithoutHTMLBody(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "bob@example.com", "Text Only", "Just plain text", "")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	req := httptest.NewRequest(http.MethodGet, "/share/"+token, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Just plain text") {
		t.Fatalf("fallback HTML does not contain text body: %s", body)
	}
	if !strings.Contains(body, "bob@example.com") {
		t.Fatalf("fallback HTML does not contain sender: %s", body)
	}
}

func TestShareTXTWithValidToken(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "carol@example.com", "TXT Test", "Plain text content", "<p>HTML</p>")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	req := httptest.NewRequest(http.MethodGet, "/share/"+token+"/txt", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Plain text content") {
		t.Fatalf("TXT body does not contain text content: %s", body)
	}
}

func TestShareTXTFallbackWithoutTextBody(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "dave@example.com", "No Text", "", "<p>Only HTML</p>")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	req := httptest.NewRequest(http.MethodGet, "/share/"+token+"/txt", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "dave@example.com") {
		t.Fatalf("TXT fallback does not contain sender: %s", body)
	}
	if !strings.Contains(body, "No Text") {
		t.Fatalf("TXT fallback does not contain subject: %s", body)
	}
	if !strings.Contains(body, "No text content available") {
		t.Fatalf("TXT fallback does not contain no-content notice: %s", body)
	}
}

func TestShareInvalidToken(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	// Test HTML route with invalid token
	req := httptest.NewRequest(http.MethodGet, "/share/nonexistenttoken123", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	body := rr.Body.String()
	// Must not leak any email metadata
	if strings.Contains(body, "@") || strings.Contains(body, "From:") {
		t.Fatalf("404 body leaks email metadata: %s", body)
	}

	// Test TXT route with invalid token
	req2 := httptest.NewRequest(http.MethodGet, "/share/nonexistenttoken123/txt", nil)
	rr2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr2.Code, http.StatusNotFound)
	}
	body2 := rr2.Body.String()
	if strings.Contains(body2, "@") || strings.Contains(body2, "From:") {
		t.Fatalf("404 body leaks email metadata: %s", body2)
	}
}

func TestShareInvalidTokenExplicitHTMLRoute(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	// Test explicit /html route with invalid token
	req := httptest.NewRequest(http.MethodGet, "/share/nonexistenttoken456/html", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	body := rr.Body.String()
	// Must not leak any email metadata
	if strings.Contains(body, "@") || strings.Contains(body, "From:") {
		t.Fatalf("404 body leaks email metadata: %s", body)
	}
}

func TestSharePublicNoAuthRequired(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "eve@example.com", "Public Test", "Public content", "")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	// Request WITHOUT any session cookie
	req := httptest.NewRequest(http.MethodGet, "/share/"+token, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (public route should work without auth)", rr.Code, http.StatusOK)
	}
}

func TestShareSecurityHeaders(t *testing.T) {
	database := setupShareTestDB(t)
	defer teardownShareTestDB(t, database)

	ctx := context.Background()
	msgID := insertShareTestMessage(t, ctx, database, "sec@example.com", "Security Test", "content", "<p>html</p>")

	token, err := db.CreateShareToken(ctx, database, msgID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), PublicBaseURL: "https://mail.example.test"}
	s := New(cfg, database)

	req := httptest.NewRequest(http.MethodGet, "/share/"+token, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", rr.Header().Get("X-Content-Type-Options"))
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q, want restrictive CSP", csp)
	}
}

func TestRenderShareHTMLEscapes(t *testing.T) {
	html := renderShareHTML("<script>alert(1)</script>", "<b>bold</b>", "2025-01-01", "<em>text</em>")
	if strings.Contains(html, "<script>") {
		t.Fatal("renderShareHTML did not escape sender")
	}
	if strings.Contains(html, "<b>bold</b>") {
		t.Fatal("renderShareHTML did not escape subject")
	}
	if strings.Contains(html, "<em>text</em>") {
		t.Fatal("renderShareHTML did not escape text body")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("renderShareHTML missing escaped sender")
	}
}
