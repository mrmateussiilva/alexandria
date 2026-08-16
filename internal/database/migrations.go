package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"alexandria/migrations"
)

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := migrationApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version = ?", name).Scan(&version)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check migration %s: %w", name, err)
}

func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	body, err := migrations.FS.ReadFile(filepath.ToSlash(name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("execute migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}
