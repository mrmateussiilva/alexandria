package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"alexandria/internal/ai"
	"alexandria/internal/books"
)

const maxCoverBytes = 2 << 20

type Worker struct {
	store     *Store
	books     *books.Store
	client    *OpenLibraryClient
	ai        *ai.Client
	http      *http.Client
	cachePath string
	logger    *slog.Logger
	wake      chan struct{}
}

func NewWorker(store *Store, bookStore *books.Store, client *OpenLibraryClient, aiClient *ai.Client, cachePath string, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store:     store,
		books:     bookStore,
		client:    client,
		ai:        aiClient,
		http:      &http.Client{Timeout: 30 * time.Second},
		cachePath: cachePath,
		logger:    logger,
		wake:      make(chan struct{}, 1),
	}
}

func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		w.drain(ctx)

		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		processed, err := w.processOne(ctx)
		if err != nil {
			w.logger.Error("process metadata job", "error", err)
		}
		if !processed {
			return
		}

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimNext(ctx)
	if errors.Is(err, ErrNoQueuedJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	err = w.runJob(ctx, job)
	if err != nil {
		if failErr := w.store.Fail(ctx, job.ID, err); failErr != nil {
			return true, fmt.Errorf("%w; fail job: %v", err, failErr)
		}
		w.logger.Warn("metadata job failed", "book_id", job.BookID, "job", job.ID, "error", err)
		return true, nil
	}

	if err := w.store.Complete(ctx, job.ID); err != nil {
		return true, err
	}
	return true, nil
}

func (w *Worker) runJob(ctx context.Context, job Job) error {
	book, err := w.books.GetBook(ctx, job.BookID)
	if err != nil {
		return fmt.Errorf("get book for metadata: %w", err)
	}
	location, err := w.books.GetFileLocation(ctx, job.BookID)
	if err != nil {
		return fmt.Errorf("get book file for metadata: %w", err)
	}

	title := CleanTitle(book.Filename, book.Extension)
	localCoverPath, localCoverErr := w.extractLocalCover(ctx, location)
	if localCoverErr != nil && !errors.Is(localCoverErr, ErrNoLocalCover) {
		w.logger.Warn("extract local cover", "book_id", book.ID, "relative_path", book.RelativePath, "error", localCoverErr)
	}

	result, err := w.client.Search(ctx, title)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if w.ai != nil && w.ai.Enabled() {
			aiResult, aiErr := w.ai.Enrich(ctx, ai.Input{
				Title:        title,
				Filename:     book.Filename,
				RelativePath: book.RelativePath,
			})
			if aiErr == nil {
				return w.storeAIMetadata(ctx, book, aiResult, localCoverPath)
			}
			w.logger.Warn("gemini metadata fallback failed", "book_id", book.ID, "relative_path", book.RelativePath, "error", aiErr)
		}
		w.logger.Warn("metadata providers failed; using local metadata", "book_id", book.ID, "relative_path", book.RelativePath, "error", err)
		return w.storeLocalMetadata(ctx, book, title, localCoverPath)
	}

	value := Metadata{
		BookID:        book.ID,
		Provider:      ProviderOpenLibrary,
		ProviderKey:   result.ProviderKey,
		Title:         result.Title,
		Authors:       result.Authors,
		Description:   "",
		PublishedYear: result.PublishedYear,
		CoverURL:      result.CoverURL,
		SourceURL:     result.SourceURL,
		Confidence:    result.Confidence,
	}
	if localCoverPath != "" {
		value.CoverPath = &localCoverPath
	}
	if result.CoverURL != nil {
		coverPath, err := w.downloadCover(ctx, book.ID, *result.CoverURL)
		if err != nil {
			w.logger.Warn("download metadata cover", "book_id", book.ID, "error", err)
		} else {
			value.CoverPath = &coverPath
		}
	}

	if err := w.store.Upsert(ctx, value); err != nil {
		return err
	}
	w.logger.Info("metadata updated", "book_id", book.ID, "provider", value.Provider, "confidence", value.Confidence)
	return nil
}

func (w *Worker) storeLocalMetadata(ctx context.Context, book books.Book, title, coverPath string) error {
	value := Metadata{
		BookID:      book.ID,
		Provider:    ProviderLocal,
		ProviderKey: "local:" + book.ID,
		Title:       title,
		Authors:     []string{},
		Description: "",
		Confidence:  0.25,
	}
	if coverPath != "" {
		value.CoverPath = &coverPath
	}
	if err := w.store.Upsert(ctx, value); err != nil {
		return err
	}
	w.logger.Info("local metadata updated", "book_id", book.ID, "relative_path", book.RelativePath)
	return nil
}

func (w *Worker) storeAIMetadata(ctx context.Context, book books.Book, result ai.Result, coverPath string) error {
	value := Metadata{
		BookID:        book.ID,
		Provider:      ProviderGemini,
		ProviderKey:   "gemini:" + book.ID,
		Title:         result.Title,
		Authors:       result.Authors,
		Description:   result.Description,
		PublishedYear: result.PublishedYear,
		Confidence:    0.55,
	}
	if coverPath != "" {
		value.CoverPath = &coverPath
	}
	if err := w.store.Upsert(ctx, value); err != nil {
		return err
	}
	w.logger.Info("AI metadata updated", "book_id", book.ID, "provider", value.Provider, "model", w.ai.Model())
	return nil
}

func (w *Worker) downloadCover(ctx context.Context, bookID, coverURL string) (string, error) {
	if w.cachePath == "" {
		return "", fmt.Errorf("cover cache path is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return "", fmt.Errorf("create cover request: %w", err)
	}
	req.Header.Set("User-Agent", "Alexandria/0.1 (self-hosted personal library)")

	resp, err := w.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("download cover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download cover: status %d", resp.StatusCode)
	}

	dir := filepath.Join(w.cachePath, "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cover cache directory: %w", err)
	}
	relativePath := filepath.Join("covers", bookID+".jpg")
	path := filepath.Join(w.cachePath, relativePath)
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create cover cache file: %w", err)
	}
	defer file.Close()

	limited := io.LimitReader(resp.Body, maxCoverBytes+1)
	written, err := io.Copy(file, limited)
	if err != nil {
		return "", fmt.Errorf("write cover cache file: %w", err)
	}
	if written > maxCoverBytes {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("cover is larger than %d bytes", maxCoverBytes)
	}
	return relativePath, nil
}
