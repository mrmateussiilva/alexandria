package metadata

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"alexandria/internal/books"
	"alexandria/internal/database"
)

func TestWorkerFetchesMetadataAndCachesCover(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	bookID := insertMetadataTestBook(t, db)
	store := NewStore(db)
	bookStore := books.NewStore(db)

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/search.json":
			if got := r.URL.Query().Get("title"); !strings.Contains(got, "Clean Code") {
				t.Fatalf("expected title query to contain Clean Code, got %q", got)
			}
			return testResponse(http.StatusOK, `{"docs":[{"key":"/works/OL1W","title":"Clean Code","author_name":["Robert C. Martin"],"first_publish_year":2008,"cover_i":123}]}`), nil
		case "/b/id/123-M.jpg":
			return testResponse(http.StatusOK, "cover"), nil
		default:
			return testResponse(http.StatusNotFound, "not found"), nil
		}
	})}

	client := NewOpenLibraryClient(httpClient)
	client.baseURL = "https://openlibrary.test"
	client.coverBaseURL = "https://openlibrary.test"
	worker := NewWorker(store, bookStore, client, nil, t.TempDir(), nil)
	worker.http = httpClient

	if _, err := store.Enqueue(ctx, bookID); err != nil {
		t.Fatalf("enqueue metadata job: %v", err)
	}
	processed, err := worker.processOne(ctx)
	if err != nil {
		t.Fatalf("process metadata job: %v", err)
	}
	if !processed {
		t.Fatal("expected worker to process a job")
	}

	value, err := store.Get(ctx, bookID)
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	if value.Title != "Clean Code" {
		t.Fatalf("expected title Clean Code, got %q", value.Title)
	}
	if len(value.Authors) != 1 || value.Authors[0] != "Robert C. Martin" {
		t.Fatalf("unexpected authors %#v", value.Authors)
	}
	if value.CoverPath == nil || *value.CoverPath == "" {
		t.Fatal("expected cached cover path")
	}

	job, err := store.GetJobByBook(ctx, bookID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != JobSucceeded || job.Attempts != 1 {
		t.Fatalf("expected succeeded job with one attempt, got %#v", job)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCleanTitleRemovesExtensionAndSeparators(t *testing.T) {
	got := CleanTitle("Clean_Code - Robert Martin.pdf", ".pdf")
	if got != "Clean Code" {
		t.Fatalf("unexpected clean title %q", got)
	}
}

func insertMetadataTestBook(t *testing.T, db *sql.DB) string {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO libraries (id, name, path, created_at, updated_at)
VALUES ('lib1', 'Library', '/books', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert library: %v", err)
	}
	_, err = db.Exec(`
INSERT INTO books (id, library_id, relative_path, filename, extension, file_size, file_modified_at, page_count, status, created_at, updated_at)
VALUES ('book1', 'lib1', 'Clean_Code.pdf', 'Clean_Code.pdf', '.pdf', 10, '2026-01-01T00:00:00Z', NULL, 'discovered', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert book: %v", err)
	}
	return "book1"
}
