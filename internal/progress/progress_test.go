package progress_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"alexandria/internal/books"
	"alexandria/internal/database"
	"alexandria/internal/library"
	"alexandria/internal/progress"
)

func TestProgressDefaultsToFirstPage(t *testing.T) {
	store := newProgressStore(t)
	value, err := store.GetProgress(context.Background(), "missing")
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}
	if value.CurrentPage != 1 {
		t.Fatalf("expected default current page 1, got %d", value.CurrentPage)
	}
}

func TestPutProgressRejectsInvalidValues(t *testing.T) {
	store := newProgressStore(t)
	_, err := store.PutProgress(context.Background(), "book", progress.UpdateInput{CurrentPage: 0})
	if !errors.Is(err, progress.ErrInvalidProgress) {
		t.Fatalf("expected invalid progress error, got %v", err)
	}

	totalPages := 5
	_, err = store.PutProgress(context.Background(), "book", progress.UpdateInput{CurrentPage: 6, TotalPages: &totalPages})
	if !errors.Is(err, progress.ErrInvalidProgress) {
		t.Fatalf("expected invalid progress error, got %v", err)
	}
}

func TestPutProgressStoresLocatorAndFraction(t *testing.T) {
	store, bookID := newProgressStoreWithBook(t)
	locator := "epubcfi(/6/2!/4)"
	fraction := 0.42

	value, err := store.PutProgress(context.Background(), bookID, progress.UpdateInput{
		CurrentPage: 1,
		Locator:     &locator,
		Fraction:    &fraction,
	})
	if err != nil {
		t.Fatalf("put progress: %v", err)
	}
	if value.Locator == nil || *value.Locator != locator {
		t.Fatalf("expected locator %q, got %#v", locator, value.Locator)
	}
	if value.Fraction == nil || *value.Fraction != fraction {
		t.Fatalf("expected fraction %f, got %#v", fraction, value.Fraction)
	}
}

func newProgressStore(t *testing.T) *progress.Store {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return progress.NewStore(db)
}

func newProgressStoreWithBook(t *testing.T) (*progress.Store, string) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	libraryService := library.NewService(db)
	lib, err := libraryService.CreateLibrary(context.Background(), library.CreateInput{
		Name: "Progress",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	bookStore := books.NewStore(db)
	if err := bookStore.InsertDiscovered(context.Background(), lib.ID, books.FileRecord{
		RelativePath:   "book.epub",
		FileSize:       4,
		FileModifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("insert book: %v", err)
	}
	items, err := bookStore.ListBooks(context.Background(), books.ListFilter{LibraryID: lib.ID, Limit: 1})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected inserted book, got %d", len(items))
	}

	return progress.NewStore(db), items[0].ID
}
