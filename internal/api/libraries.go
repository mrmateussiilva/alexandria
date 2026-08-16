package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"alexandria/internal/books"
	"alexandria/internal/library"
	"alexandria/internal/scanner"
)

type libraryResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type librariesResponse struct {
	Items []libraryResponse `json:"items"`
}

type folderResponse struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	ParentPath string `json:"parent_path"`
	BookCount  int    `json:"book_count"`
}

type foldersResponse struct {
	ParentPath string           `json:"parent_path"`
	Items      []folderResponse `json:"items"`
}

type createLibraryRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func registerLibraryRoutes(r chi.Router, svc Services) {
	r.Get("/api/libraries", listLibrariesHandler(svc.Libraries))
	r.Post("/api/libraries", createLibraryHandler(svc.Libraries))
	r.Get("/api/libraries/{id}", getLibraryHandler(svc.Libraries))
	r.Delete("/api/libraries/{id}", deleteLibraryHandler(svc.Libraries))
	if svc.Books != nil {
		r.Get("/api/libraries/{id}/folders", listLibraryFoldersHandler(svc.Libraries, svc.Books))
	}
	if svc.Scanner != nil {
		r.Post("/api/libraries/{id}/scan", scanLibraryHandler(svc.Scanner))
	}
}

func listLibrariesHandler(libraries *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := libraries.ListLibraries(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao listar bibliotecas")
			return
		}
		writeJSON(w, http.StatusOK, librariesResponse{Items: publicLibraries(items)})
	}
}

func createLibraryHandler(libraries *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input createLibraryRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "corpo JSON inválido")
			return
		}

		lib, err := libraries.CreateLibrary(r.Context(), library.CreateInput{
			Name: input.Name,
			Path: input.Path,
		})
		if err != nil {
			switch {
			case errors.Is(err, library.ErrInvalidName):
				writeError(w, http.StatusBadRequest, "invalid_name", "nome da biblioteca é obrigatório")
			case errors.Is(err, library.ErrInvalidPath):
				writeError(w, http.StatusBadRequest, "invalid_path", "caminho da biblioteca deve ser um diretório existente")
			case errors.Is(err, library.ErrDuplicate):
				writeError(w, http.StatusConflict, "duplicate_library", "caminho da biblioteca já está cadastrado")
			default:
				writeError(w, http.StatusInternalServerError, "internal_error", "falha ao criar biblioteca")
			}
			return
		}

		writeJSON(w, http.StatusCreated, publicLibrary(lib))
	}
}

func getLibraryHandler(libraries *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lib, err := libraries.GetLibrary(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, library.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "biblioteca não encontrada")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar biblioteca")
			return
		}
		writeJSON(w, http.StatusOK, publicLibrary(lib))
	}
}

func deleteLibraryHandler(libraries *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := libraries.DeleteLibrary(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, library.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "biblioteca não encontrada")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao excluir biblioteca")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listLibraryFoldersHandler(libraries *library.Service, bookStore *books.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		libraryID := chi.URLParam(r, "id")
		if _, err := libraries.GetLibrary(r.Context(), libraryID); err != nil {
			if errors.Is(err, library.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "biblioteca não encontrada")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao buscar biblioteca")
			return
		}

		parentPath, err := books.NormalizeFolderPath(r.URL.Query().Get("parent"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", "caminho de pasta inválido")
			return
		}

		folders, err := bookStore.ListFolders(r.Context(), libraryID, parentPath)
		if err != nil {
			if errors.Is(err, books.ErrInvalidFilter) {
				writeError(w, http.StatusBadRequest, "invalid_filter", "caminho de pasta inválido")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao listar pastas")
			return
		}
		writeJSON(w, http.StatusOK, foldersResponse{
			ParentPath: parentPath,
			Items:      publicFolders(folders),
		})
	}
}

func scanLibraryHandler(scans *scanner.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summary, err := scans.ScanLibrary(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			switch {
			case errors.Is(err, scanner.ErrScanAlreadyRunning):
				writeError(w, http.StatusConflict, "scan_already_running", "scan já está em execução para esta biblioteca")
			case errors.Is(err, library.ErrNotFound):
				writeError(w, http.StatusNotFound, "not_found", "biblioteca não encontrada")
			default:
				writeError(w, http.StatusInternalServerError, "internal_error", "falha ao escanear biblioteca")
			}
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func publicFolders(items []books.Folder) []folderResponse {
	result := make([]folderResponse, 0, len(items))
	for _, item := range items {
		result = append(result, folderResponse{
			Path:       item.Path,
			Name:       item.Name,
			ParentPath: item.ParentPath,
			BookCount:  item.BookCount,
		})
	}
	return result
}

func publicLibraries(items []library.Library) []libraryResponse {
	result := make([]libraryResponse, 0, len(items))
	for _, item := range items {
		result = append(result, publicLibrary(item))
	}
	return result
}

func publicLibrary(lib library.Library) libraryResponse {
	return libraryResponse{
		ID:        lib.ID,
		Name:      lib.Name,
		CreatedAt: lib.CreatedAt,
		UpdatedAt: lib.UpdatedAt,
	}
}
