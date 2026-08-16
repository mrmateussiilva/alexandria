package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"alexandria/internal/books"
	"alexandria/internal/filesystem"
	"alexandria/internal/metadata"
	"alexandria/internal/progress"
)

type bookResponse struct {
	ID             string `json:"id"`
	LibraryID      string `json:"library_id"`
	RelativePath   string `json:"relative_path"`
	Folder         string `json:"folder"`
	Category       string `json:"category"`
	Filename       string `json:"filename"`
	Extension      string `json:"extension"`
	FileSize       int64  `json:"file_size"`
	FileModifiedAt string `json:"file_modified_at"`
	PageCount      *int   `json:"page_count"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type booksResponse struct {
	Items  []bookResponse `json:"items"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type progressResponse struct {
	BookID      string   `json:"book_id"`
	CurrentPage int      `json:"current_page"`
	TotalPages  *int     `json:"total_pages"`
	Locator     *string  `json:"locator"`
	Fraction    *float64 `json:"fraction"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

type metadataResponse struct {
	BookID        string   `json:"book_id"`
	Provider      string   `json:"provider"`
	ProviderKey   string   `json:"provider_key"`
	Title         string   `json:"title"`
	Authors       []string `json:"authors"`
	Description   string   `json:"description"`
	PublishedYear *int     `json:"published_year"`
	CoverURL      *string  `json:"cover_url"`
	SourceURL     *string  `json:"source_url"`
	Confidence    float64  `json:"confidence"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type metadataJobResponse struct {
	ID           string  `json:"id"`
	BookID       string  `json:"book_id"`
	Filename     string  `json:"filename,omitempty"`
	RelativePath string  `json:"relative_path,omitempty"`
	Status       string  `json:"status"`
	Attempts     int     `json:"attempts"`
	LastError    *string `json:"last_error"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	CompletedAt  *string `json:"completed_at"`
}

type metadataJobsResponse struct {
	Items  []metadataJobResponse `json:"items"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

type enqueueMetadataJobsRequest struct {
	LibraryID string `json:"library_id"`
	Limit     int    `json:"limit"`
}

type enqueueMetadataJobsResponse struct {
	Queued int `json:"queued"`
}

type updateProgressRequest struct {
	CurrentPage int      `json:"current_page"`
	TotalPages  *int     `json:"total_pages"`
	Locator     *string  `json:"locator"`
	Fraction    *float64 `json:"fraction"`
}

func registerBookRoutes(r chi.Router, svc Services) {
	r.Get("/api/books", listBooksHandler(svc.Books))
	r.Get("/api/books/{id}", getBookHandler(svc.Books))
	r.Get("/api/books/{id}/file", getBookFileHandler(svc.Books))
	if svc.Metadata != nil {
		r.Get("/api/metadata/jobs", listMetadataJobsHandler(svc.Metadata))
		r.Post("/api/metadata/jobs", enqueueMetadataJobsHandler(svc.Metadata, svc.MetaWorker))
		r.Get("/api/books/{id}/metadata", getMetadataHandler(svc.Books, svc.Metadata))
		r.Get("/api/books/{id}/cover", getBookCoverHandler(svc.Metadata, svc.CachePath))
		r.Post("/api/books/{id}/metadata/refresh", refreshMetadataHandler(svc.Books, svc.Metadata, svc.MetaWorker))
	}
	if svc.Progress != nil {
		r.Get("/api/books/{id}/progress", getProgressHandler(svc.Books, svc.Progress))
		r.Put("/api/books/{id}/progress", putProgressHandler(svc.Books, svc.Progress))
	}
}

func listMetadataJobsHandler(metadataStore *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, ok := intQuery(w, r, "limit", 50)
		if !ok {
			return
		}
		offset, ok := intQuery(w, r, "offset", 0)
		if !ok {
			return
		}

		jobs, err := metadataStore.ListJobs(r.Context(), metadata.JobFilter{
			LibraryID: r.URL.Query().Get("library_id"),
			Status:    r.URL.Query().Get("status"),
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			if errors.Is(err, metadata.ErrInvalidFilter) {
				writeError(w, http.StatusBadRequest, "invalid_filter", "filtro de jobs inválido")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao listar jobs de metadados")
			return
		}
		writeJSON(w, http.StatusOK, metadataJobsResponse{
			Items:  publicMetadataJobs(jobs),
			Limit:  normalizedLimit(limit),
			Offset: normalizedOffset(offset),
		})
	}
}

func enqueueMetadataJobsHandler(metadataStore *metadata.Store, worker *metadata.Worker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input enqueueMetadataJobsRequest
		if r.Body != nil {
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "invalid_json", "corpo JSON inválido")
				return
			}
		}

		queued, err := metadataStore.EnqueueMissing(r.Context(), metadata.EnqueueMissingInput{
			LibraryID: input.LibraryID,
			Limit:     input.Limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao enfileirar jobs de metadados")
			return
		}
		if worker != nil && queued > 0 {
			worker.Notify()
		}
		writeJSON(w, http.StatusAccepted, enqueueMetadataJobsResponse{Queued: queued})
	}
}

func listBooksHandler(store *books.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, ok := intQuery(w, r, "limit", 50)
		if !ok {
			return
		}
		offset, ok := intQuery(w, r, "offset", 0)
		if !ok {
			return
		}

		filter := books.ListFilter{
			LibraryID: r.URL.Query().Get("library_id"),
			Status:    r.URL.Query().Get("status"),
			Query:     r.URL.Query().Get("q"),
			Folder:    r.URL.Query().Get("folder"),
			Limit:     limit,
			Offset:    offset,
		}

		items, err := store.ListBooks(r.Context(), filter)
		if err != nil {
			if errors.Is(err, books.ErrInvalidFilter) {
				writeError(w, http.StatusBadRequest, "invalid_filter", "filtro de livros inválido")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao listar livros")
			return
		}

		writeJSON(w, http.StatusOK, booksResponse{
			Items:  publicBooks(items),
			Limit:  normalizedLimit(limit),
			Offset: normalizedOffset(offset),
		})
	}
}

func getBookHandler(store *books.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		book, err := store.GetBook(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, books.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
			return
		}
		writeJSON(w, http.StatusOK, publicBook(book))
	}
}

func getMetadataHandler(bookStore *books.Store, metadataStore *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		if _, err := bookStore.GetBook(r.Context(), bookID); err != nil {
			if errors.Is(err, books.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
			return
		}

		value, err := metadataStore.Get(r.Context(), bookID)
		if err != nil {
			if errors.Is(err, metadata.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "metadados não encontrados")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar metadados")
			return
		}
		writeJSON(w, http.StatusOK, publicMetadata(value))
	}
}

func refreshMetadataHandler(bookStore *books.Store, metadataStore *metadata.Store, worker *metadata.Worker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		if _, err := bookStore.GetBook(r.Context(), bookID); err != nil {
			if errors.Is(err, books.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
			return
		}

		job, err := metadataStore.Enqueue(r.Context(), bookID)
		if err != nil && !errors.Is(err, metadata.ErrJobInProgress) {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao enfileirar busca de metadados")
			return
		}
		if worker != nil {
			worker.Notify()
		}
		writeJSON(w, http.StatusAccepted, publicMetadataJob(job))
	}
}

func getBookCoverHandler(metadataStore *metadata.Store, cachePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := metadataStore.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, metadata.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "metadados não encontrados")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar metadados")
			return
		}
		if value.CoverPath == nil || *value.CoverPath == "" {
			writeError(w, http.StatusNotFound, "not_found", "capa não encontrada")
			return
		}
		path, ok := resolveCacheFile(cachePath, *value.CoverPath)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "capa não encontrada")
			return
		}

		file, err := os.Open(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "capa não encontrada")
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "capa não encontrada")
			return
		}
		w.Header().Set("Content-Type", contentTypeForCoverPath(*value.CoverPath))
		http.ServeContent(w, r, "cover.jpg", info.ModTime(), file)
	}
}

func resolveCacheFile(cachePath, relativePath string) (string, bool) {
	if cachePath == "" || relativePath == "" || filepath.IsAbs(relativePath) {
		return "", false
	}
	root := filepath.Clean(cachePath)
	path := filepath.Join(root, filepath.Clean(relativePath))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

func getBookFileHandler(store *books.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		location, err := store.GetFileLocation(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeBookFileError(w, err)
			return
		}
		if !books.SupportedReaderExtension(location.Extension) {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_format", "formato não suportado pelo leitor")
			return
		}

		path, info, err := filesystem.ResolveLibraryFile(location.LibraryPath, location.RelativePath)
		if err != nil {
			if errors.Is(err, filesystem.ErrPathEscape) {
				writeError(w, http.StatusForbidden, "forbidden", "arquivo do livro está fora da biblioteca")
				return
			}
			writeBookFileError(w, books.ErrFileUnavailable)
			return
		}

		file, err := os.Open(path)
		if err != nil {
			writeBookFileError(w, books.ErrFileUnavailable)
			return
		}
		defer file.Close()

		w.Header().Set("Content-Type", contentTypeForExtension(location.Extension))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": location.Filename}))
		http.ServeContent(w, r, location.Filename, info.ModTime(), file)
	}
}

func getProgressHandler(bookStore *books.Store, progressStore *progress.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		if _, err := bookStore.GetBook(r.Context(), bookID); err != nil {
			if errors.Is(err, books.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
			return
		}

		current, err := progressStore.GetProgress(r.Context(), bookID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar progresso de leitura")
			return
		}
		writeJSON(w, http.StatusOK, publicProgress(current))
	}
}

func putProgressHandler(bookStore *books.Store, progressStore *progress.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		if _, err := bookStore.GetBook(r.Context(), bookID); err != nil {
			if errors.Is(err, books.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
			return
		}

		var input updateProgressRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "corpo JSON inválido")
			return
		}

		updated, err := progressStore.PutProgress(r.Context(), bookID, progress.UpdateInput{
			CurrentPage: input.CurrentPage,
			TotalPages:  input.TotalPages,
			Locator:     input.Locator,
			Fraction:    input.Fraction,
		})
		if err != nil {
			if errors.Is(err, progress.ErrInvalidProgress) {
				writeError(w, http.StatusBadRequest, "invalid_progress", "progresso de leitura inválido")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao atualizar progresso de leitura")
			return
		}
		writeJSON(w, http.StatusOK, publicProgress(updated))
	}
}

func intQuery(w http.ResponseWriter, r *http.Request, key string, fallback int) (int, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", key+" deve ser um número inteiro")
		return 0, false
	}
	return value, true
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func normalizedOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func publicBooks(items []books.Book) []bookResponse {
	result := make([]bookResponse, 0, len(items))
	for _, item := range items {
		result = append(result, publicBook(item))
	}
	return result
}

func publicBook(book books.Book) bookResponse {
	return bookResponse{
		ID:             book.ID,
		LibraryID:      book.LibraryID,
		RelativePath:   book.RelativePath,
		Folder:         books.FolderPath(book.RelativePath),
		Category:       books.TopLevelFolder(book.RelativePath),
		Filename:       book.Filename,
		Extension:      book.Extension,
		FileSize:       book.FileSize,
		FileModifiedAt: book.FileModifiedAt,
		PageCount:      book.PageCount,
		Status:         book.Status,
		CreatedAt:      book.CreatedAt,
		UpdatedAt:      book.UpdatedAt,
	}
}

func publicProgress(value progress.Progress) progressResponse {
	return progressResponse{
		BookID:      value.BookID,
		CurrentPage: value.CurrentPage,
		TotalPages:  value.TotalPages,
		Locator:     value.Locator,
		Fraction:    value.Fraction,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func publicMetadata(value metadata.Metadata) metadataResponse {
	coverURL := value.CoverURL
	if value.CoverPath != nil {
		apiCoverURL := "/api/books/" + value.BookID + "/cover"
		coverURL = &apiCoverURL
	}
	return metadataResponse{
		BookID:        value.BookID,
		Provider:      value.Provider,
		ProviderKey:   value.ProviderKey,
		Title:         value.Title,
		Authors:       value.Authors,
		Description:   value.Description,
		PublishedYear: value.PublishedYear,
		CoverURL:      coverURL,
		SourceURL:     value.SourceURL,
		Confidence:    value.Confidence,
		CreatedAt:     value.CreatedAt,
		UpdatedAt:     value.UpdatedAt,
	}
}

func publicMetadataJob(job metadata.Job) metadataJobResponse {
	return metadataJobResponse{
		ID:           job.ID,
		BookID:       job.BookID,
		Filename:     job.Filename,
		RelativePath: job.RelativePath,
		Status:       job.Status,
		Attempts:     job.Attempts,
		LastError:    job.LastError,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
		CompletedAt:  job.CompletedAt,
	}
}

func publicMetadataJobs(jobs []metadata.Job) []metadataJobResponse {
	result := make([]metadataJobResponse, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, publicMetadataJob(job))
	}
	return result
}

func contentTypeForExtension(extension string) string {
	switch strings.ToLower(extension) {
	case ".pdf":
		return "application/pdf"
	case ".epub":
		return "application/epub+zip"
	case ".mobi":
		return "application/x-mobipocket-ebook"
	default:
		return "application/octet-stream"
	}
}

func contentTypeForCoverPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func writeBookFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, books.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
	case errors.Is(err, books.ErrFileUnavailable):
		writeError(w, http.StatusNotFound, "file_unavailable", "arquivo do livro indisponível")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "falha ao abrir arquivo do livro")
	}
}
