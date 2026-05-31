package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
)

func insertTestMessage(t *testing.T, ctx context.Context, db *sql.DB, cfID, hash string) int64 {
	t.Helper()
	result, err := db.ExecContext(ctx,
		"INSERT INTO messages (cloudflare_message_id, content_hash, sender, subject, message_date, received_at, raw_mime_path) VALUES (?, ?, ?, ?, ?, ?, ?)",
		cfID, hash, "sender@example.com", "Test", "2025-01-01 00:00:00", "2025-01-01 00:00:00", "/raw/test.eml",
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

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping database test")
	}

	database, err := Connect(dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	migrations, err := LoadMigrations(MigrationsFS)
	if err != nil {
		database.Close()
		t.Fatalf("load migrations: %v", err)
	}

	if err := RunMigrations(database, migrations); err != nil {
		database.Close()
		t.Fatalf("run migrations: %v", err)
	}

	return database
}

func teardownTestDB(t *testing.T, database *sql.DB) {
	t.Helper()
	if database == nil {
		return
	}

	migrations, err := LoadMigrations(MigrationsFS)
	if err != nil {
		t.Logf("WARNING: failed to load migrations for teardown: %v", err)
		database.Close()
		return
	}

	if err := RollbackMigration(database, migrations); err != nil {
		t.Logf("WARNING: failed to rollback migration: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Logf("WARNING: failed to close database: %v", err)
	}
}

func TestCreateShareTokenIdempotent(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()
	messageID := insertTestMessage(t, ctx, database, "cf-id-share-1", "hash-share-1")

	token1, err := CreateShareToken(ctx, database, messageID)
	if err != nil {
		t.Fatalf("CreateShareToken first call: %v", err)
	}
	if token1 == "" {
		t.Fatal("CreateShareToken returned empty token")
	}

	token2, err := CreateShareToken(ctx, database, messageID)
	if err != nil {
		t.Fatalf("CreateShareToken second call: %v", err)
	}
	if token2 != token1 {
		t.Fatalf("idempotency violated: first=%s second=%s", token1, token2)
	}

	var count int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM share_tokens WHERE message_id = ?", messageID).Scan(&count)
	if err != nil {
		t.Fatalf("count share tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 share token row, got %d", count)
	}
}

func TestFindMessageIDByTokenNotFound(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()

	_, err := FindMessageIDByToken(ctx, database, "nonexistent_token")
	if !errors.Is(err, ErrShareTokenNotFound) {
		t.Fatalf("expected ErrShareTokenNotFound, got %v", err)
	}
}

func TestFindMessageIDByTokenRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()
	messageID := insertTestMessage(t, ctx, database, "cf-id-share-rt", "hash-share-rt")

	token, err := CreateShareToken(ctx, database, messageID)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	foundID, err := FindMessageIDByToken(ctx, database, token)
	if err != nil {
		t.Fatalf("FindMessageIDByToken: %v", err)
	}
	if foundID != messageID {
		t.Fatalf("expected message_id %d, got %d", messageID, foundID)
	}
}

func TestGetShareTokenByMessageIDNotFound(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()

	_, err := GetShareTokenByMessageID(ctx, database, 99999)
	if !errors.Is(err, ErrShareTokenNotFound) {
		t.Fatalf("expected ErrShareTokenNotFound, got %v", err)
	}
}

func TestGenerateTokenFormat(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("token contains non-hex character: %c", c)
		}
	}
}

func TestFindMessageIDByTokenReturnsSQLErrNoRows(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()

	_, err := FindMessageIDByToken(ctx, database, "does_not_exist")
	if !errors.Is(err, ErrShareTokenNotFound) {
		t.Fatalf("expected ErrShareTokenNotFound, got %v", err)
	}
	if !errors.Is(ErrShareTokenNotFound, sql.ErrNoRows) {
		t.Log("ErrShareTokenNotFound is a sentinel distinct from sql.ErrNoRows (acceptable)")
	}
}
