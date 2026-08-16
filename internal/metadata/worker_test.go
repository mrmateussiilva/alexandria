package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alexandria/internal/books"
	"alexandria/internal/database"
	"alexandria/internal/library"
)

func TestWorkerStoresLocalMetadataWhenOpenLibraryHasNoMatch(t *testing.T) {
	testWorkerStoresLocalMetadataWhenOpenLibraryFails(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"docs":[]}`))
	})
}

func TestWorkerStoresLocalMetadataWhenOpenLibraryTimesOut(t *testing.T) {
	testWorkerStoresLocalMetadataWhenOpenLibraryFails(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"docs":[]}`))
	}, func(client *OpenLibraryClient) {
		client.httpClient.Timeout = time.Millisecond
	})
}

func testWorkerStoresLocalMetadataWhenOpenLibraryFails(
	t *testing.T,
	handler http.HandlerFunc,
	options ...func(*OpenLibraryClient),
) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	root := t.TempDir()
	bookPath := filepath.Join(root, "Sem Match.mobi")
	if err := os.WriteFile(bookPath, []byte("mobi"), 0o644); err != nil {
		t.Fatalf("write book: %v", err)
	}

	libraries := library.NewService(db)
	lib, err := libraries.CreateLibrary(context.Background(), library.CreateInput{Name: "Livros", Path: root})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	bookStore := books.NewStore(db)
	if err := bookStore.InsertDiscovered(context.Background(), lib.ID, books.FileRecord{
		RelativePath:   "Sem Match.mobi",
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
		t.Fatalf("expected one book, got %#v", items)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewOpenLibraryClient(server.Client())
	client.baseURL = server.URL
	for _, option := range options {
		option(client)
	}

	store := NewStore(db)
	worker := NewWorker(store, bookStore, client, nil, t.TempDir(), nil)
	if err := worker.runJob(context.Background(), Job{BookID: items[0].ID}); err != nil {
		t.Fatalf("run metadata job: %v", err)
	}

	value, err := store.Get(context.Background(), items[0].ID)
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	if value.Provider != ProviderLocal || value.Title != "Sem Match" {
		t.Fatalf("expected local metadata fallback, got %#v", value)
	}
}
