package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"alexandria/internal/annotations"
	"alexandria/internal/auth"
	"alexandria/internal/books"
	"alexandria/internal/library"
	"alexandria/internal/metadata"
	"alexandria/internal/progress"
	"alexandria/internal/scanner"
	"alexandria/internal/server"
)

type Services struct {
	Libraries   *library.Service
	Books       *books.Store
	Annotations *annotations.Store
	Auth        *auth.Service
	Metadata    *metadata.Store
	MetaWorker  *metadata.Worker
	CachePath   string
	Progress    *progress.Store
	Scanner     *scanner.Manager
}

func NewRouter(services ...Services) chi.Router {
	var svc Services
	if len(services) > 0 {
		svc = services[0]
	}

	r := chi.NewRouter()
	r.Use(securityHeaders)
	r.Use(authMiddleware(svc.Auth))
	r.Get("/api/health", healthHandler)
	if svc.Auth != nil {
		registerAuthRoutes(r, svc.Auth)
	}
	if svc.Libraries != nil {
		registerLibraryRoutes(r, svc)
	}
	if svc.Books != nil {
		registerBookRoutes(r, svc)
	}
	if svc.Books != nil && svc.Annotations != nil {
		registerAnnotationRoutes(r, svc)
	}
	r.NotFound(server.FrontendHandler())
	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' blob: data:",
			"font-src 'self' blob: data:",
			"connect-src 'self' blob:",
			"frame-src blob:",
			"worker-src 'self' blob:",
			"object-src 'none'",
			"base-uri 'self'",
		}, "; "))
		next.ServeHTTP(w, r)
	})
}
