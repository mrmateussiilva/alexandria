package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alexandria/internal/books"
	"alexandria/internal/database"
	"alexandria/internal/library"
	"alexandria/internal/scanner"
)

type scannerFixture struct {
	libraries *library.Service
	books     *books.Store
	scanner   *scanner.Manager
}

func newScannerFixture(t *testing.T) scannerFixture {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "alexandria.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	libraries := library.NewService(db)
	bookStore := books.NewStore(db)
	return scannerFixture{
		libraries: libraries,
		books:     bookStore,
		scanner:   scanner.NewManager(libraries, bookStore),
	}
}

func TestScannerIncrementalPDFDiscovery(t *testing.T) {
	fixture := newScannerFixture(t)
	root := t.TempDir()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(root, "Go", "livro1.pdf"), []byte("one"), baseTime)
	writeFileAt(t, filepath.Join(root, "Rust", "livro2.PDF"), []byte("two"), baseTime.Add(time.Second))
	writeFileAt(t, filepath.Join(root, "notas.txt"), []byte("ignore"), baseTime)

	lib, err := fixture.libraries.CreateLibrary(context.Background(), library.CreateInput{Name: "Tech", Path: root})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	summary, err := fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{New: 2})

	summary, err = fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{Unchanged: 2})

	writeFileAt(t, filepath.Join(root, "Go", "livro1.pdf"), []byte("changed"), baseTime.Add(2*time.Second))
	summary, err = fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("changed scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{Changed: 1, Unchanged: 1})

	writeFileAt(t, filepath.Join(root, "Go", "livro3.pdf"), []byte("three"), baseTime.Add(3*time.Second))
	summary, err = fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("new file scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{New: 1, Unchanged: 2})

	if err := os.Remove(filepath.Join(root, "Rust", "livro2.PDF")); err != nil {
		t.Fatalf("remove pdf: %v", err)
	}
	summary, err = fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("missing scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{Unchanged: 2, Missing: 1})

	items, err := fixture.books.ListBooks(context.Background(), books.ListFilter{LibraryID: lib.ID, Limit: 20})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 tracked PDFs, got %d", len(items))
	}
}

func TestScannerIgnoresNonPDFHiddenDirsAndSymlinks(t *testing.T) {
	fixture := newScannerFixture(t)
	root := t.TempDir()
	outside := t.TempDir()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(root, "visible.pdf"), []byte("pdf"), baseTime)
	writeFileAt(t, filepath.Join(root, "notes.txt"), []byte("txt"), baseTime)
	writeFileAt(t, filepath.Join(root, ".hidden", "secret.pdf"), []byte("hidden"), baseTime)
	writeFileAt(t, filepath.Join(outside, "outside.pdf"), []byte("outside"), baseTime)
	if err := os.Symlink(filepath.Join(outside, "outside.pdf"), filepath.Join(root, "outside-link.pdf")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	lib, err := fixture.libraries.CreateLibrary(context.Background(), library.CreateInput{Name: "Safe", Path: root})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	summary, err := fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{New: 1})

	items, err := fixture.books.ListBooks(context.Background(), books.ListFilter{LibraryID: lib.ID, Limit: 20})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(items) != 1 || items[0].RelativePath != "visible.pdf" {
		t.Fatalf("expected only visible.pdf, got %#v", items)
	}
}

func TestScannerIndexesSpacesAndUnicodePDFs(t *testing.T) {
	fixture := newScannerFixture(t)
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "Espacos", "acao λ.PDF"), []byte("pdf"), time.Now())

	lib, err := fixture.libraries.CreateLibrary(context.Background(), library.CreateInput{Name: "Unicode", Path: root})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	summary, err := fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{New: 1})

	items, err := fixture.books.ListBooks(context.Background(), books.ListFilter{LibraryID: lib.ID, Query: "λ", Limit: 20})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(items) != 1 || items[0].RelativePath != "Espacos/acao λ.PDF" {
		t.Fatalf("expected unicode relative path, got %#v", items)
	}
}

func TestScannerDiscoversEPUBAndMOBI(t *testing.T) {
	fixture := newScannerFixture(t)
	root := t.TempDir()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(root, "book.epub"), []byte("epub"), baseTime)
	writeFileAt(t, filepath.Join(root, "book.MOBI"), []byte("mobi"), baseTime)

	lib, err := fixture.libraries.CreateLibrary(context.Background(), library.CreateInput{Name: "Ebooks", Path: root})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	summary, err := fixture.scanner.ScanLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	assertSummary(t, summary, scanner.Summary{New: 2})

	items, err := fixture.books.ListBooks(context.Background(), books.ListFilter{LibraryID: lib.ID, Limit: 20})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	extensions := map[string]bool{}
	for _, item := range items {
		extensions[item.Extension] = true
	}
	if !extensions[".epub"] || !extensions[".mobi"] {
		t.Fatalf("expected epub and mobi extensions, got %#v", extensions)
	}
}

func writeFileAt(t *testing.T, path string, body []byte, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("set file time: %v", err)
	}
}

func assertSummary(t *testing.T, got, want scanner.Summary) {
	t.Helper()
	if got != want {
		t.Fatalf("expected summary %#v, got %#v", want, got)
	}
}
