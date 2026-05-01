package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := db.migrationApplied(ctx, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) migrationApplied(ctx context.Context, version string) (bool, error) {
	var existing string
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_migrations WHERE version = ?`, version).Scan(&existing)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}
