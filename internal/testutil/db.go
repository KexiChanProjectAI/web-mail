package testutil

import (
	"database/sql"
	"os"
	"testing"

	"lite-mail/internal/db"
)

func SetupTestDB(t *testing.T) *sql.DB {
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

func TeardownTestDB(t *testing.T, database *sql.DB) {
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

func DBURL() string {
	return os.Getenv("TEST_DATABASE_URL")
}

func HasDB() bool {
	return os.Getenv("TEST_DATABASE_URL") != ""
}
