package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lite-mail/internal/config"
	maildb "lite-mail/internal/db"
	"lite-mail/internal/ingest"
	"lite-mail/internal/server"
	"lite-mail/internal/storage"
)

const (
	AdminPSK        = "test-admin-psk"
	NormalUserPSK   = "test-normal-psk"
	WorkerIngestPSK = "test-ingest-psk"
	TestUserEmail   = "test@example.com"
)

type IntegrationTestSuite struct {
	db      *sql.DB
	storage *storage.Storage
	server  *httptest.Server
	config  *config.Config
	tmpDir  string
}

func SetupSuite(t *testing.T) *IntegrationTestSuite {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	tmpDir := t.TempDir()
	database, err := maildb.Connect(dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}

	migrations, err := maildb.LoadMigrations(projectPath("migrations"))
	if err != nil {
		database.Close()
		t.Fatalf("load migrations: %v", err)
	}
	if err := maildb.RunMigrations(database, migrations); err != nil {
		database.Close()
		t.Fatalf("run migrations: %v", err)
	}
	cleanDatabase(t, database)

	cfg := &config.Config{
		DatabaseURL:       dsn,
		DataDir:           tmpDir,
		PublicBaseURL:     "http://127.0.0.1",
		MaxMessageBytes:   1024 * 1024,
		SessionCookieName: "lite_mail_test_session",
		SessionTTLHours:   24,
		NormalUserPSK:     NormalUserPSK,
		AdminPSK:          AdminPSK,
		WorkerIngestPSK:   WorkerIngestPSK,
		AppEnv:            "test",
	}
	store, err := storage.NewStorage(cfg.DataDir)
	if err != nil {
		database.Close()
		t.Fatalf("create storage: %v", err)
	}
	srv := server.New(cfg, database)
	httpServer := httptest.NewServer(srv.Handler())
	cfg.PublicBaseURL = httpServer.URL

	return &IntegrationTestSuite{db: database, storage: store, server: httpServer, config: cfg, tmpDir: tmpDir}
}

func TeardownSuite(suite *IntegrationTestSuite) {
	if suite == nil {
		return
	}
	if suite.server != nil {
		suite.server.Close()
	}
	if suite.db != nil {
		migrations, err := maildb.LoadMigrations(projectPath("migrations"))
		if err == nil {
			_ = maildb.RollbackMigration(suite.db, migrations)
		}
		_ = suite.db.Close()
	}
	if suite.tmpDir != "" {
		_ = os.RemoveAll(suite.tmpDir)
	}
}

func LoadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(projectPath("testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

func postIngest(t *testing.T, suite *IntegrationTestSuite, raw []byte, psk string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, suite.server.URL+"/api/ingest", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create ingest request: %v", err)
	}
	req.Header.Set("Content-Type", "message/rfc822")
	if psk != "" {
		req.Header.Set("X-Lite-Mail-Ingest-PSK", psk)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatalf("post ingest: %v", err)
	}
	return resp
}

func ingestFixture(t *testing.T, suite *IntegrationTestSuite, fixture string) int64 {
	t.Helper()
	return ingestRaw(t, suite, LoadFixture(t, fixture))
}

func ingestRaw(t *testing.T, suite *IntegrationTestSuite, raw []byte) int64 {
	t.Helper()
	resp := postIngest(t, suite, raw, WorkerIngestPSK)
	defer resp.Body.Close()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest status = %d, body = %s", resp.StatusCode, body)
	}
	var decoded struct {
		Status    string `json:"status"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	if decoded.Status != "accepted" || decoded.MessageID == 0 {
		t.Fatalf("ingest response = %+v", decoded)
	}
	return decoded.MessageID
}

func login(t *testing.T, suite *IntegrationTestSuite, psk, email string) []*http.Cookie {
	t.Helper()
	body := map[string]string{"psk": psk}
	if email != "" {
		body["email"] = email
	}
	payload, _ := json.Marshal(body)
	resp, err := suite.server.Client().Post(suite.server.URL+"/api/login", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	respBody := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", resp.StatusCode, respBody)
	}
	return resp.Cookies()
}

func getWithCookies(t *testing.T, suite *IntegrationTestSuite, path string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, suite.server.URL+path, nil)
	if err != nil {
		t.Fatalf("create GET %s: %v", path, err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func postWithCookies(t *testing.T, suite *IntegrationTestSuite, path string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, suite.server.URL+path, nil)
	if err != nil {
		t.Fatalf("create POST %s: %v", path, err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := suite.server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

type listResponse struct {
	Messages []messageResponse `json:"messages"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PerPage  int               `json:"per_page"`
}

type messageResponse struct {
	ID          int64                `json:"id"`
	Sender      string               `json:"sender"`
	Subject     string               `json:"subject"`
	TextBody    string               `json:"text_body"`
	HTMLBody    string               `json:"html_body"`
	Recipients  []recipientResponse  `json:"recipients"`
	Attachments []attachmentResponse `json:"attachments"`
}

type recipientResponse struct {
	Email string `json:"email"`
	Type  string `json:"type"`
}

type attachmentResponse struct {
	ID               int64  `json:"id"`
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	data := readBody(t, resp)
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode JSON: %v; body=%s", err, data)
	}
	return out
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return data
}

func dbScalar[T comparable](t *testing.T, suite *IntegrationTestSuite, query string, args ...any) T {
	t.Helper()
	var value T
	if err := suite.db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	return value
}

func rawMIMEPath(raw []byte) string {
	return filepath.Join("raw", ingest.HashBytes(raw))
}

func rawMIMEFile(suite *IntegrationTestSuite, raw []byte) string {
	return filepath.Join(suite.tmpDir, rawMIMEPath(raw))
}

func attachmentFile(t *testing.T, suite *IntegrationTestSuite, messageID int64) string {
	t.Helper()
	storageKey := dbScalar[string](t, suite, "SELECT storage_key FROM attachments WHERE message_id = ? ORDER BY id LIMIT 1", messageID)
	return filepath.Join(suite.tmpDir, "attachments", fmt.Sprintf("%d", messageID), storageKey)
}

func assertStatus(t *testing.T, resp *http.Response, want int) []byte {
	t.Helper()
	defer resp.Body.Close()
	body := readBody(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, want, body)
	}
	return body
}

func replaceHeader(raw []byte, header, value string) []byte {
	lines := strings.Split(string(raw), "\n")
	prefix := strings.ToLower(header) + ":"
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimRight(line, "\r")), prefix) {
			ending := ""
			if strings.HasSuffix(line, "\r") {
				ending = "\r"
			}
			lines[i] = header + ": " + value + ending
			return []byte(strings.Join(lines, "\n"))
		}
	}
	return raw
}

func uniqueMessage(t *testing.T, fixture, subject, recipient string) []byte {
	t.Helper()
	raw := LoadFixture(t, fixture)
	if subject != "" {
		raw = replaceHeader(raw, "Subject", fmt.Sprintf("%s %d", subject, time.Now().UnixNano()))
	}
	if recipient != "" {
		raw = replaceHeader(raw, "To", recipient)
	}
	return raw
}

func cleanDatabase(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, query := range []string{
		"DELETE FROM ingest_events",
		"DELETE FROM attachments",
		"DELETE FROM message_recipients",
		"DELETE FROM messages",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatalf("clean database with %q: %v", query, err)
		}
	}
}

func projectPath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot resolve project path")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	return filepath.Join(append([]string{root}, parts...)...)
}
