package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"alexandria/internal/auth"
)

type authStatusResponse struct {
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func registerAuthRoutes(r chi.Router, authService *auth.Service) {
	r.Get("/api/auth/status", authStatusHandler(authService))
	r.Post("/api/auth/login", loginHandler(authService))
	r.Post("/api/auth/logout", logoutHandler(authService))
}

func authStatusHandler(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := authStatusResponse{
			Enabled:       authService.Enabled(),
			Authenticated: authService.Authenticated(r),
		}
		if response.Authenticated {
			response.Username = authService.Username()
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func loginHandler(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input loginRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "corpo JSON inválido")
			return
		}

		if err := authService.Authenticate(input.Username, input.Password); err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "invalid_credentials", "usuário ou senha inválidos")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao autenticar")
			return
		}
		cookie, err := authService.NewCookie(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "falha ao criar sessão")
			return
		}
		if cookie != nil {
			http.SetCookie(w, cookie)
		}
		writeJSON(w, http.StatusOK, authStatusResponse{
			Enabled:       authService.Enabled(),
			Authenticated: true,
			Username:      authService.Username(),
		})
	}
}

func logoutHandler(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, authService.ClearCookie(r))
		writeJSON(w, http.StatusOK, authStatusResponse{
			Enabled:       authService.Enabled(),
			Authenticated: false,
		})
	}
}

func authMiddleware(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authService == nil || !authService.Enabled() || publicAuthPath(r.URL.Path) || !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}
			if !authService.Authenticated(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "sessão expirada ou não autenticada")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func publicAuthPath(path string) bool {
	return path == "/api/health" || strings.HasPrefix(path, "/api/auth/")
}
