package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alexandria/internal/annotations"
	"alexandria/internal/books"
	"alexandria/internal/database"
	"alexandria/internal/library"
	metadatastore "alexandria/internal/metadata"
	progressstore "alexandria/internal/progress"
	"alexandria/internal/scanner"
)

func TestLibrariesScanAndBooksAPI(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "Go", "livro 1.pdf"), []byte("%PDF-one"), time.Now())

	createBody := `{"name":"Programacao","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), root) || strings.Contains(createRec.Body.String(), `"path"`) {
		t.Fatalf("library response leaked absolute path: %s", createRec.Body.String())
	}

	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}

	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}
	var first scanner.Summary
	if err := json.NewDecoder(scanRec.Body).Decode(&first); err != nil {
		t.Fatalf("decode scan summary: %v", err)
	}
	if first != (scanner.Summary{New: 1}) {
		t.Fatalf("expected first scan new=1, got %#v", first)
	}

	booksRec := httptest.NewRecorder()
	router.ServeHTTP(booksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID+"&q=livro", nil))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books status 200, got %d: %s", booksRec.Code, booksRec.Body.String())
	}
	var listed booksResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode books response: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].RelativePath != "Go/livro 1.pdf" {
		t.Fatalf("expected scanned book, got %#v", listed.Items)
	}

	secondScanRec := httptest.NewRecorder()
	router.ServeHTTP(secondScanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if secondScanRec.Code != http.StatusOK {
		t.Fatalf("expected second scan status 200, got %d: %s", secondScanRec.Code, secondScanRec.Body.String())
	}
	var second scanner.Summary
	if err := json.NewDecoder(secondScanRec.Body).Decode(&second); err != nil {
		t.Fatalf("decode second scan summary: %v", err)
	}
	if second != (scanner.Summary{Unchanged: 1}) {
		t.Fatalf("expected second scan unchanged=1, got %#v", second)
	}

	fileRec := httptest.NewRecorder()
	fileReq := httptest.NewRequest(http.MethodGet, "/api/books/"+listed.Items[0].ID+"/file", nil)
	fileReq.Header.Set("Range", "bytes=0-3")
	router.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusPartialContent {
		t.Fatalf("expected file status 206, got %d: %s", fileRec.Code, fileRec.Body.String())
	}
	if fileRec.Body.String() != "%PDF" {
		t.Fatalf("expected ranged PDF bytes, got %q", fileRec.Body.String())
	}

	progressRec := httptest.NewRecorder()
	router.ServeHTTP(progressRec, httptest.NewRequest(http.MethodPut, "/api/books/"+listed.Items[0].ID+"/progress", strings.NewReader(`{"current_page":2,"total_pages":4}`)))
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress status 200, got %d: %s", progressRec.Code, progressRec.Body.String())
	}

	getProgressRec := httptest.NewRecorder()
	router.ServeHTTP(getProgressRec, httptest.NewRequest(http.MethodGet, "/api/books/"+listed.Items[0].ID+"/progress", nil))
	if getProgressRec.Code != http.StatusOK {
		t.Fatalf("expected get progress status 200, got %d: %s", getProgressRec.Code, getProgressRec.Body.String())
	}
	var progress progressResponse
	if err := json.NewDecoder(getProgressRec.Body).Decode(&progress); err != nil {
		t.Fatalf("decode progress response: %v", err)
	}
	if progress.CurrentPage != 2 || progress.TotalPages == nil || *progress.TotalPages != 4 {
		t.Fatalf("unexpected progress response %#v", progress)
	}
}

func TestBookFileEndpointServesEPUBAndCSP(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "Livro.epub"), []byte("epub"), time.Now())

	createBody := `{"name":"Ebooks","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	if createRec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected CSP header")
	}

	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}
	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}

	booksRec := httptest.NewRecorder()
	router.ServeHTTP(booksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID, nil))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books status 200, got %d: %s", booksRec.Code, booksRec.Body.String())
	}
	var listed booksResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode books response: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("expected 1 ebook, got %#v", listed.Items)
	}

	fileRec := httptest.NewRecorder()
	router.ServeHTTP(fileRec, httptest.NewRequest(http.MethodGet, "/api/books/"+listed.Items[0].ID+"/file", nil))
	if fileRec.Code != http.StatusOK {
		t.Fatalf("expected file status 200, got %d: %s", fileRec.Code, fileRec.Body.String())
	}
	if contentType := fileRec.Header().Get("Content-Type"); contentType != "application/epub+zip" {
		t.Fatalf("expected epub content type, got %q", contentType)
	}
}

func TestLibraryFoldersAndBookFolderFilter(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	now := time.Now()
	writeAPITestFile(t, filepath.Join(root, "Go", "livro 1.pdf"), []byte("go"), now)
	writeAPITestFile(t, filepath.Join(root, "Go", "Avancado", "livro 2.epub"), []byte("epub"), now)
	writeAPITestFile(t, filepath.Join(root, "Rust", "livro 3.MOBI"), []byte("mobi"), now)
	writeAPITestFile(t, filepath.Join(root, "raiz.pdf"), []byte("root"), now)

	createBody := `{"name":"Livros","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}

	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}

	foldersRec := httptest.NewRecorder()
	router.ServeHTTP(foldersRec, httptest.NewRequest(http.MethodGet, "/api/libraries/"+created.ID+"/folders", nil))
	if foldersRec.Code != http.StatusOK {
		t.Fatalf("expected folders status 200, got %d: %s", foldersRec.Code, foldersRec.Body.String())
	}
	var folders foldersResponse
	if err := json.NewDecoder(foldersRec.Body).Decode(&folders); err != nil {
		t.Fatalf("decode folders response: %v", err)
	}
	if len(folders.Items) != 2 {
		t.Fatalf("expected two root folders, got %#v", folders.Items)
	}
	if folders.Items[0].Path != "Go" || folders.Items[0].BookCount != 2 {
		t.Fatalf("expected Go category with 2 books, got %#v", folders.Items[0])
	}
	if folders.Items[1].Path != "Rust" || folders.Items[1].BookCount != 1 {
		t.Fatalf("expected Rust category with 1 book, got %#v", folders.Items[1])
	}

	goFoldersRec := httptest.NewRecorder()
	router.ServeHTTP(goFoldersRec, httptest.NewRequest(http.MethodGet, "/api/libraries/"+created.ID+"/folders?parent=Go", nil))
	if goFoldersRec.Code != http.StatusOK {
		t.Fatalf("expected Go folders status 200, got %d: %s", goFoldersRec.Code, goFoldersRec.Body.String())
	}
	var goFolders foldersResponse
	if err := json.NewDecoder(goFoldersRec.Body).Decode(&goFolders); err != nil {
		t.Fatalf("decode Go folders response: %v", err)
	}
	if goFolders.ParentPath != "Go" || len(goFolders.Items) != 1 || goFolders.Items[0].Path != "Go/Avancado" {
		t.Fatalf("expected Go/Avancado child folder, got %#v", goFolders)
	}

	booksRec := httptest.NewRecorder()
	router.ServeHTTP(booksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID+"&folder=Go", nil))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books status 200, got %d: %s", booksRec.Code, booksRec.Body.String())
	}
	var listed booksResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode books response: %v", err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("expected 2 Go books, got %#v", listed.Items)
	}
	for _, item := range listed.Items {
		if item.Category != "Go" || !strings.HasPrefix(item.RelativePath, "Go/") {
			t.Fatalf("expected Go categorized book, got %#v", item)
		}
	}

	invalidFoldersRec := httptest.NewRecorder()
	router.ServeHTTP(invalidFoldersRec, httptest.NewRequest(http.MethodGet, "/api/libraries/"+created.ID+"/folders?parent=../", nil))
	if invalidFoldersRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid folders status 400, got %d: %s", invalidFoldersRec.Code, invalidFoldersRec.Body.String())
	}

	invalidBooksRec := httptest.NewRecorder()
	router.ServeHTTP(invalidBooksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID+"&folder=../", nil))
	if invalidBooksRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid books status 400, got %d: %s", invalidBooksRec.Code, invalidBooksRec.Body.String())
	}
}

func TestMetadataEndpointsEnqueueAndReadMetadata(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "Clean Code.pdf"), []byte("pdf"), time.Now())

	createBody := `{"name":"Livros","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}

	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}

	booksRec := httptest.NewRecorder()
	router.ServeHTTP(booksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID, nil))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books status 200, got %d: %s", booksRec.Code, booksRec.Body.String())
	}
	var listed booksResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode books response: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("expected one book, got %#v", listed.Items)
	}
	bookID := listed.Items[0].ID

	refreshRec := httptest.NewRecorder()
	router.ServeHTTP(refreshRec, httptest.NewRequest(http.MethodPost, "/api/books/"+bookID+"/metadata/refresh", nil))
	if refreshRec.Code != http.StatusAccepted {
		t.Fatalf("expected metadata refresh status 202, got %d: %s", refreshRec.Code, refreshRec.Body.String())
	}
	var job metadataJobResponse
	if err := json.NewDecoder(refreshRec.Body).Decode(&job); err != nil {
		t.Fatalf("decode metadata job: %v", err)
	}
	if job.BookID != bookID || job.Status != metadatastore.JobQueued {
		t.Fatalf("unexpected metadata job %#v", job)
	}

	metadataRec := httptest.NewRecorder()
	router.ServeHTTP(metadataRec, httptest.NewRequest(http.MethodGet, "/api/books/"+bookID+"/metadata", nil))
	if metadataRec.Code != http.StatusNotFound {
		t.Fatalf("expected metadata status 404 before worker runs, got %d: %s", metadataRec.Code, metadataRec.Body.String())
	}
}

func TestMetadataJobsAPIListsAndEnqueuesMissing(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "Go", "Livro A.pdf"), []byte("pdf-a"), time.Now())
	writeAPITestFile(t, filepath.Join(root, "Go", "Livro B.pdf"), []byte("pdf-b"), time.Now())

	createBody := `{"name":"Livros","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}

	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}

	queueRec := httptest.NewRecorder()
	queueBody := `{"library_id":"` + created.ID + `","limit":10}`
	router.ServeHTTP(queueRec, httptest.NewRequest(http.MethodPost, "/api/metadata/jobs", strings.NewReader(queueBody)))
	if queueRec.Code != http.StatusAccepted {
		t.Fatalf("expected queue status 202, got %d: %s", queueRec.Code, queueRec.Body.String())
	}
	var queued enqueueMetadataJobsResponse
	if err := json.NewDecoder(queueRec.Body).Decode(&queued); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if queued.Queued != 2 {
		t.Fatalf("expected 2 queued jobs, got %#v", queued)
	}

	jobsRec := httptest.NewRecorder()
	router.ServeHTTP(jobsRec, httptest.NewRequest(http.MethodGet, "/api/metadata/jobs?library_id="+created.ID, nil))
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("expected jobs status 200, got %d: %s", jobsRec.Code, jobsRec.Body.String())
	}
	var jobs metadataJobsResponse
	if err := json.NewDecoder(jobsRec.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode jobs response: %v", err)
	}
	if len(jobs.Items) != 2 {
		t.Fatalf("expected 2 jobs, got %#v", jobs.Items)
	}
	if jobs.Items[0].Filename == "" || jobs.Items[0].RelativePath == "" {
		t.Fatalf("expected job book context, got %#v", jobs.Items[0])
	}

	secondQueueRec := httptest.NewRecorder()
	router.ServeHTTP(secondQueueRec, httptest.NewRequest(http.MethodPost, "/api/metadata/jobs", strings.NewReader(queueBody)))
	if secondQueueRec.Code != http.StatusAccepted {
		t.Fatalf("expected second queue status 202, got %d: %s", secondQueueRec.Code, secondQueueRec.Body.String())
	}
	var secondQueued enqueueMetadataJobsResponse
	if err := json.NewDecoder(secondQueueRec.Body).Decode(&secondQueued); err != nil {
		t.Fatalf("decode second queue response: %v", err)
	}
	if secondQueued.Queued != 0 {
		t.Fatalf("expected no duplicate jobs, got %#v", secondQueued)
	}
}

func TestBookAnnotationsAPIStoresAndExportsMarkdown(t *testing.T) {
	router := newAPITestRouter(t)
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "Estudo.pdf"), []byte("%PDF"), time.Now())

	createBody := `{"name":"Livros","path":` + quoteJSON(root) + `}`
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created libraryResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library: %v", err)
	}

	scanRec := httptest.NewRecorder()
	router.ServeHTTP(scanRec, httptest.NewRequest(http.MethodPost, "/api/libraries/"+created.ID+"/scan", nil))
	if scanRec.Code != http.StatusOK {
		t.Fatalf("expected scan status 200, got %d: %s", scanRec.Code, scanRec.Body.String())
	}

	booksRec := httptest.NewRecorder()
	router.ServeHTTP(booksRec, httptest.NewRequest(http.MethodGet, "/api/books?library_id="+created.ID, nil))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books status 200, got %d: %s", booksRec.Code, booksRec.Body.String())
	}
	var listed booksResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode books response: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("expected one book, got %#v", listed.Items)
	}

	body := `{"kind":"note","page_number":3,"total_pages":10,"note":"Ponto importante para revisar"}`
	noteRec := httptest.NewRecorder()
	router.ServeHTTP(noteRec, httptest.NewRequest(http.MethodPost, "/api/books/"+listed.Items[0].ID+"/annotations", strings.NewReader(body)))
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("expected annotation status 201, got %d: %s", noteRec.Code, noteRec.Body.String())
	}
	var createdAnnotation annotationResponse
	if err := json.NewDecoder(noteRec.Body).Decode(&createdAnnotation); err != nil {
		t.Fatalf("decode annotation response: %v", err)
	}
	if createdAnnotation.Kind != annotations.KindNote || createdAnnotation.Note == "" {
		t.Fatalf("unexpected annotation response %#v", createdAnnotation)
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/books/"+listed.Items[0].ID+"/annotations", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected annotations status 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var annotationsList annotationsResponse
	if err := json.NewDecoder(listRec.Body).Decode(&annotationsList); err != nil {
		t.Fatalf("decode annotations response: %v", err)
	}
	if len(annotationsList.Items) != 1 {
		t.Fatalf("expected one annotation, got %#v", annotationsList.Items)
	}

	exportRec := httptest.NewRecorder()
	router.ServeHTTP(exportRec, httptest.NewRequest(http.MethodGet, "/api/annotations/export", nil))
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected export status 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}
	if !strings.Contains(exportRec.Body.String(), "Ponto importante para revisar") || !strings.Contains(exportRec.Body.String(), "Estudo.pdf") {
		t.Fatalf("expected markdown export to include annotation, got %s", exportRec.Body.String())
	}
}

func newAPITestRouter(t *testing.T) http.Handler {
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
	metadataStore := metadatastore.NewStore(db)
	return NewRouter(Services{
		Libraries:   libraries,
		Books:       bookStore,
		Annotations: annotations.NewStore(db, t.TempDir()),
		Metadata:    metadataStore,
		CachePath:   t.TempDir(),
		Progress:    progressstore.NewStore(db),
		Scanner:     scanner.NewManager(libraries, bookStore),
	})
}

func quoteJSON(value string) string {
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(value)
	return strings.TrimSpace(buf.String())
}

func writeAPITestFile(t *testing.T, path string, body []byte, modTime time.Time) {
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
