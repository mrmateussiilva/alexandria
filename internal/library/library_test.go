package library_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"alexandria/internal/database"
	"alexandria/internal/library"
)

func newService(t *testing.T) *library.Service {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	return library.NewService(db)
}

func TestCreateListGetDeleteLibrary(t *testing.T) {
	service := newService(t)
	root := t.TempDir()

	created, err := service.CreateLibrary(context.Background(), library.CreateInput{
		Name: "Programacao",
		Path: root,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected stable id")
	}
	if created.Path == "" || !filepath.IsAbs(created.Path) {
		t.Fatalf("expected absolute internal path, got %q", created.Path)
	}

	listed, err := service.ListLibraries(context.Background())
	if err != nil {
		t.Fatalf("list libraries: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 library, got %d", len(listed))
	}

	got, err := service.GetLibrary(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get library: %v", err)
	}
	if got.Name != "Programacao" {
		t.Fatalf("expected library name Programacao, got %q", got.Name)
	}

	if err := service.DeleteLibrary(context.Background(), created.ID); err != nil {
		t.Fatalf("delete library: %v", err)
	}
	if _, err := service.GetLibrary(context.Background(), created.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCreateLibraryRejectsDuplicateNormalizedPath(t *testing.T) {
	service := newService(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "library")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}

	if _, err := service.CreateLibrary(context.Background(), library.CreateInput{Name: "One", Path: root}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	duplicatePath := filepath.Join(root, "..", filepath.Base(root))
	_, err := service.CreateLibrary(context.Background(), library.CreateInput{Name: "Two", Path: duplicatePath})
	if !errors.Is(err, library.ErrDuplicate) {
		t.Fatalf("expected duplicate path error, got %v", err)
	}
}

func TestCreateLibraryRejectsInvalidPaths(t *testing.T) {
	service := newService(t)
	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(root, "missing")},
		{name: "file", path: filePath},
		{name: "empty", path: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.CreateLibrary(context.Background(), library.CreateInput{Name: "Invalid", Path: tt.path})
			if !errors.Is(err, library.ErrInvalidPath) {
				t.Fatalf("expected invalid path error, got %v", err)
			}
		})
	}
}

func TestCreateLibraryRejectsEmptyName(t *testing.T) {
	service := newService(t)
	_, err := service.CreateLibrary(context.Background(), library.CreateInput{Name: "  ", Path: t.TempDir()})
	if !errors.Is(err, library.ErrInvalidName) {
		t.Fatalf("expected invalid name error, got %v", err)
	}
}

func TestCreateLibraryAcceptsUnusualNames(t *testing.T) {
	service := newService(t)
	lib, err := service.CreateLibrary(context.Background(), library.CreateInput{
		Name: "Programacao / Filosofia Δ",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create library with unusual name: %v", err)
	}
	if lib.Name != "Programacao / Filosofia Δ" {
		t.Fatalf("unexpected library name %q", lib.Name)
	}
}
