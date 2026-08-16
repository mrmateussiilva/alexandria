package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"alexandria/internal/ai"
	"alexandria/internal/annotations"
	"alexandria/internal/api"
	"alexandria/internal/auth"
	"alexandria/internal/books"
	"alexandria/internal/config"
	"alexandria/internal/database"
	"alexandria/internal/library"
	"alexandria/internal/metadata"
	"alexandria/internal/progress"
	"alexandria/internal/scanner"
	"alexandria/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.Migrate(context.Background(), db); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.Cache.Path, 0o755); err != nil {
		logger.Error("create cache directory", "path", cfg.Cache.Path, "error", err)
		os.Exit(1)
	}
	annotationsPath := filepath.Join(filepath.Dir(cfg.Database.Path), "annotations")
	if err := os.MkdirAll(annotationsPath, 0o755); err != nil {
		logger.Error("create annotations directory", "path", annotationsPath, "error", err)
		os.Exit(1)
	}
	authService, err := auth.New(cfg.Auth)
	if err != nil {
		logger.Error("configure auth", "error", err)
		os.Exit(1)
	}
	if !authService.Enabled() {
		logger.Warn("auth disabled; set ALEXANDRIA_AUTH_PASSWORD or ALEXANDRIA_AUTH_PASSWORD_HASH to enable login")
	}

	libraryService := library.NewService(db)
	bookStore := books.NewStore(db)
	progressStore := progress.NewStore(db)
	annotationStore := annotations.NewStore(db, annotationsPath)
	metadataStore := metadata.NewStore(db)
	var aiClient *ai.Client
	if cfg.AI.Enabled {
		aiClient = ai.NewClient(cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Timeout, nil)
		logger.Info("AI metadata fallback enabled", "provider", "gemini", "model", cfg.AI.Model)
	}
	metadataWorker := metadata.NewWorker(
		metadataStore,
		bookStore,
		metadata.NewOpenLibraryClient(nil),
		aiClient,
		cfg.Cache.Path,
		logger,
	)
	scanManager := scanner.NewManager(libraryService, bookStore)

	handler := api.NewRouter(api.Services{
		Libraries:   libraryService,
		Books:       bookStore,
		Annotations: annotationStore,
		Auth:        authService,
		Metadata:    metadataStore,
		MetaWorker:  metadataWorker,
		CachePath:   cfg.Cache.Path,
		Progress:    progressStore,
		Scanner:     scanManager,
	})
	httpServer := server.New(cfg.Server, handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go metadataWorker.Run(ctx)
	go func() {
		logger.Info("starting server", "address", cfg.Server.Address, "port", cfg.Server.Port)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown server", "error", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}
}
