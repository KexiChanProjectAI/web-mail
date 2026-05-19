package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
)

type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

func (m *Migration) String() string {
	return fmt.Sprintf("%03d_%s", m.Version, m.Name)
}

func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	if !hasUpMigration(entries) && hasDir(entries, "migrations") {
		if subFS, err := fs.Sub(fsys, "migrations"); err == nil {
			fsys = subFS
			entries, err = fs.ReadDir(fsys, ".")
			if err != nil {
				return nil, fmt.Errorf("read migrations dir: %w", err)
			}
		}
	}

	seen := make(map[string]bool)
	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		filename := entry.Name()
		name := strings.TrimSuffix(filename, ".up.sql")
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			continue
		}
		migrationName := parts[1]
		if seen[migrationName] {
			continue
		}
		seen[migrationName] = true
		migrations = append(migrations, Migration{
			Version: version,
			Name:    migrationName,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	for i := range migrations {
		upPath := fmt.Sprintf("%03d_%s.up.sql", migrations[i].Version, migrations[i].Name)
		downPath := fmt.Sprintf("%03d_%s.down.sql", migrations[i].Version, migrations[i].Name)

		upBytes, err := fs.ReadFile(fsys, upPath)
		if err != nil {
			return nil, fmt.Errorf("read up migration %s: %w", upPath, err)
		}
		migrations[i].UpSQL = string(upBytes)

		downBytes, err := fs.ReadFile(fsys, downPath)
		if err != nil {
			return nil, fmt.Errorf("read down migration %s: %w", downPath, err)
		}
		migrations[i].DownSQL = string(downBytes)
	}

	return migrations, nil
}

// splitSQL splits a multi-statement SQL string into individual statements.
// It strips SQL comments and whitespace-only fragments, returning only
// meaningful statements suitable for drivers that don't support multi-statement exec.
func splitSQL(sql string) []string {
	var stmts []string
	for _, s := range strings.Split(sql, ";") {
		var lines []string
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "--") || line == "" {
				continue
			}
			lines = append(lines, line)
		}
		stmt := strings.Join(lines, "\n")
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}

func hasUpMigration(entries []fs.DirEntry) bool {
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			return true
		}
	}
	return false
}

func hasDir(entries []fs.DirEntry, name string) bool {
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() == name {
			return true
		}
	}
	return false
}

func Connect(dsn string) (*sql.DB, error) {
	normalizedDSN, err := normalizeMySQLDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("normalize dsn: %w", err)
	}
	db, err := sql.Open("mysql", normalizedDSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

func normalizeMySQLDSN(dsn string) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", fmt.Errorf("dsn is required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", err
	}
	cfg.ParseTime = true
	return cfg.FormatDSN(), nil
}

func RunMigrations(db *sql.DB, migrations []Migration) error {
	if err := createMigrationsTable(db); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	for _, m := range migrations {
		applied, err := isMigrationApplied(db, m.Version)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if applied {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.Version, err)
		}

		for _, stmt := range splitSQL(m.UpSQL) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("apply migration %d: %w", m.Version, err)
			}
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", m.Version, m.Name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}

		fmt.Printf("Applied migration %s\n", m.String())
	}

	return nil
}

func RollbackMigration(db *sql.DB, migrations []Migration) error {
	if err := createMigrationsTable(db); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version > migrations[j].Version
	})

	for _, m := range migrations {
		applied, err := isMigrationApplied(db, m.Version)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if !applied {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for rollback %d: %w", m.Version, err)
		}

		for _, stmt := range splitSQL(m.DownSQL) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("rollback migration %d: %w", m.Version, err)
			}
		}

		if _, err := tx.Exec("DELETE FROM schema_migrations WHERE version = ?", m.Version); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete migration record %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit rollback %d: %w", m.Version, err)
		}

		fmt.Printf("Rolled back migration %s\n", m.String())
		break
	}

	return nil
}

func createMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	return err
}

func isMigrationApplied(db *sql.DB, version int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func Migrate(databaseURL string, fsys fs.FS) error {
	db, err := Connect(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	return RunMigrations(db, migrations)
}

func Rollback(databaseURL string, fsys fs.FS) error {
	db, err := Connect(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	return RollbackMigration(db, migrations)
}
