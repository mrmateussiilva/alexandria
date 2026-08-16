package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAppliesInitialMigration(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	var version string
	err = db.QueryRow("SELECT version FROM schema_migrations WHERE version = ?", "000001_init.sql").Scan(&version)
	if err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if version != "000001_init.sql" {
		t.Fatalf("expected migration version 000001_init.sql, got %q", version)
	}
}

func TestMigrateCreatesLibraryAndBookTables(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	for _, table := range []string{"libraries", "books", "reading_progress", "book_metadata", "metadata_jobs", "book_annotations"} {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
			if err != nil {
				t.Fatalf("query table %s: %v", table, err)
			}
			if name != table {
				t.Fatalf("expected table %s, got %q", table, name)
			}
		})
	}
}
