package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, "001_create_mailbox_tables.up.sql"), []byte("CREATE TABLE test1 (id INT);"), 0644); err != nil {
		t.Fatalf("write up: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "001_create_mailbox_tables.down.sql"), []byte("DROP TABLE test1;"), 0644); err != nil {
		t.Fatalf("write down: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "002_add_index.up.sql"), []byte("ALTER TABLE test1 ADD INDEX idx_id(id);"), 0644); err != nil {
		t.Fatalf("write up: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "002_add_index.down.sql"), []byte("ALTER TABLE test1 DROP INDEX idx_id;"), 0644); err != nil {
		t.Fatalf("write down: %v", err)
	}

	migrations, err := LoadMigrations(os.DirFS(tmp))
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}

	if len(migrations) != 2 {
		t.Errorf("expected 2 migrations, got %d", len(migrations))
	}

	if migrations[0].Version != 1 || migrations[0].Name != "create_mailbox_tables" {
		t.Errorf("migration 1: version=%d name=%s", migrations[0].Version, migrations[0].Name)
	}

	if migrations[1].Version != 2 || migrations[1].Name != "add_index" {
		t.Errorf("migration 2: version=%d name=%s", migrations[1].Version, migrations[1].Name)
	}

	if migrations[0].UpSQL != "CREATE TABLE test1 (id INT);" {
		t.Errorf("migration 1 up SQL mismatch: %q", migrations[0].UpSQL)
	}

	if migrations[0].DownSQL != "DROP TABLE test1;" {
		t.Errorf("migration 1 down SQL mismatch: %q", migrations[0].DownSQL)
	}
}

func TestMigrationString(t *testing.T) {
	m := Migration{Version: 1, Name: "create_tables"}
	if m.String() != "001_create_tables" {
		t.Errorf("Migration.String(): got %q", m.String())
	}
}

func TestLoadMigrationsEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	migrations, err := LoadMigrations(os.DirFS(tmp))
	if err != nil {
		t.Fatalf("LoadMigrations on empty dir: %v", err)
	}
	if len(migrations) != 0 {
		t.Errorf("expected 0 migrations, got %d", len(migrations))
	}
}

func TestLoadMigrationsIgnoresNonNumeric(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "invalid_name.up.sql"), []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "invalid_name.down.sql"), []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	migrations, err := LoadMigrations(os.DirFS(tmp))
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migrations) != 0 {
		t.Errorf("expected 0 migrations, got %d", len(migrations))
	}
}

func TestLoadMigrationsMissingDownSQL(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "001_test.up.sql"), []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadMigrations(os.DirFS(tmp))
	if err == nil {
		t.Error("expected error for missing down migration")
	}
}

func TestSQLSyntaxValid(t *testing.T) {
	upPath := "migrations/001_create_mailbox_tables.up.sql"
	downPath := "migrations/001_create_mailbox_tables.down.sql"

	upBytes, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}

	downBytes, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}

	upSQL := string(upBytes)
	downSQL := string(downBytes)

	if strings.TrimSpace(upSQL) == "" {
		t.Error("up migration SQL is empty")
	}

	if strings.TrimSpace(downSQL) == "" {
		t.Error("down migration SQL is empty")
	}

	if !strings.Contains(upSQL, "CREATE TABLE IF NOT EXISTS messages") {
		t.Error("up migration missing messages table creation")
	}

	if !strings.Contains(upSQL, "CREATE TABLE IF NOT EXISTS message_recipients") {
		t.Error("up migration missing message_recipients table creation")
	}

	if !strings.Contains(upSQL, "CREATE TABLE IF NOT EXISTS attachments") {
		t.Error("up migration missing attachments table creation")
	}

	if !strings.Contains(upSQL, "CREATE TABLE IF NOT EXISTS ingest_events") {
		t.Error("up migration missing ingest_events table creation")
	}

	if !strings.Contains(upSQL, "raw_mime_path") {
		t.Error("up migration missing raw_mime_path column")
	}

	if !strings.Contains(upSQL, "storage_key") {
		t.Error("up migration missing storage_key column in attachments")
	}

	if !strings.Contains(upSQL, "UNIQUE INDEX idx_ingest_dedupe") {
		t.Error("up migration missing dedupe index on ingest_events")
	}

	if !strings.Contains(upSQL, "FULLTEXT INDEX") {
		t.Error("up migration missing fulltext index for search")
	}

	if !strings.Contains(downSQL, "DROP TABLE") {
		t.Error("down migration missing DROP TABLE statements")
	}
}
