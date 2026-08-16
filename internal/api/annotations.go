package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"alexandria/internal/annotations"
	"alexandria/internal/books"
)

type annotationResponse struct {
	ID         string   `json:"id"`
	BookID     string   `json:"book_id"`
	Kind       string   `json:"kind"`
	PageNumber *int     `json:"page_number"`
	TotalPages *int     `json:"total_pages"`
	Locator    *string  `json:"locator"`
	Fraction   *float64 `json:"fraction"`
	Note       string   `json:"note"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

type annotationsResponse struct {
	Items []annotationResponse `json:"items"`
}

type createAnnotationRequest struct {
	Kind       string   `json:"kind"`
	PageNumber *int     `json:"page_number"`
	TotalPages *int     `json:"total_pages"`
	Locator    *string  `json:"locator"`
	Fraction   *float64 `json:"fraction"`
	Note       string   `json:"note"`
}

func registerAnnotationRoutes(r chi.Router, svc Services) {
	r.Get("/api/annotations/export", exportAnnotationsHandler(svc.Annotations))
	r.Get("/api/books/{id}/annotations", listAnnotationsHandler(svc.Books, svc.Annotations))
	r.Post("/api/books/{id}/annotations", createAnnotationHandler(svc.Books, svc.Annotations))
}

func listAnnotationsHandler(bookStore *books.Store, annotationStore *annotations.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		if _, err := bookStore.GetBook(r.Context(), bookID); err != nil {
			writeBookLookupError(w, err)
			return
		}

		items, err := annotationStore.ListByBook(r.Context(), bookID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao listar anotações")
			return
		}
		writeJSON(w, http.StatusOK, annotationsResponse{Items: publicAnnotations(items)})
	}
}

func createAnnotationHandler(bookStore *books.Store, annotationStore *annotations.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "id")
		book, err := bookStore.GetBook(r.Context(), bookID)
		if err != nil {
			writeBookLookupError(w, err)
			return
		}

		var input createAnnotationRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "corpo JSON inválido")
			return
		}

		created, err := annotationStore.Create(r.Context(), annotations.CreateInput{
			BookID:       book.ID,
			BookTitle:    book.Filename,
			RelativePath: book.RelativePath,
			Kind:         input.Kind,
			PageNumber:   input.PageNumber,
			TotalPages:   input.TotalPages,
			Locator:      input.Locator,
			Fraction:     input.Fraction,
			Note:         input.Note,
		})
		if err != nil {
			if errors.Is(err, annotations.ErrInvalidAnnotation) {
				writeError(w, http.StatusBadRequest, "invalid_annotation", "anotação inválida")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao salvar anotação")
			return
		}
		writeJSON(w, http.StatusCreated, publicAnnotation(created))
	}
}

func exportAnnotationsHandler(annotationStore *annotations.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, err := annotationStore.EnsureExportFile()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao abrir arquivo de anotações")
			return
		}
		file, err := os.Open(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao abrir arquivo de anotações")
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao ler arquivo de anotações")
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="alexandria-clippings.md"`)
		http.ServeContent(w, r, "alexandria-clippings.md", info.ModTime(), file)
	}
}

func publicAnnotations(items []annotations.Annotation) []annotationResponse {
	result := make([]annotationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, publicAnnotation(item))
	}
	return result
}

func publicAnnotation(item annotations.Annotation) annotationResponse {
	return annotationResponse{
		ID:         item.ID,
		BookID:     item.BookID,
		Kind:       item.Kind,
		PageNumber: item.PageNumber,
		TotalPages: item.TotalPages,
		Locator:    item.Locator,
		Fraction:   item.Fraction,
		Note:       item.Note,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

func writeBookLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, books.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "livro não encontrado")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar livro")
}
