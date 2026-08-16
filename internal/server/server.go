package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alexandria/internal/config"
	"alexandria/web"
)

func New(cfg config.ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Address + ":" + strconv.Itoa(cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func FrontendHandler() http.HandlerFunc {
	dist := http.FileServer(http.FS(web.Dist()))

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			serveIndex(w)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.URL.Path, ".") {
			serveIndex(w)
			return
		}

		dist.ServeHTTP(w, r)
	}
}

func serveIndex(w http.ResponseWriter) {
	index, ok := web.Index()
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><body><main>Alexandria frontend is not built.</main></body></html>")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(index)
}
