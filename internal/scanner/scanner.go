package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"alexandria/internal/books"
	"alexandria/internal/library"
)

var ErrScanAlreadyRunning = errors.New("scan already running for library")

type Summary struct {
	New       int `json:"new"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Missing   int `json:"missing"`
}

type Manager struct {
	libraries *library.Service
	books     *books.Store

	mu     sync.Mutex
	active map[string]struct{}
}

func NewManager(libraries *library.Service, books *books.Store) *Manager {
	return &Manager{
		libraries: libraries,
		books:     books,
		active:    make(map[string]struct{}),
	}
}

func (m *Manager) ScanLibrary(ctx context.Context, libraryID string) (Summary, error) {
	if !m.tryStart(libraryID) {
		return Summary{}, ErrScanAlreadyRunning
	}
	defer m.finish(libraryID)

	lib, err := m.libraries.GetLibrary(ctx, libraryID)
	if err != nil {
		return Summary{}, err
	}
	return scan(ctx, m.books, lib)
}

func (m *Manager) tryStart(libraryID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.active[libraryID]; ok {
		return false
	}
	m.active[libraryID] = struct{}{}
	return true
}

func (m *Manager) finish(libraryID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, libraryID)
}

func scan(ctx context.Context, store *books.Store, lib library.Library) (Summary, error) {
	existing, err := store.ExistingByLibrary(ctx, lib.ID)
	if err != nil {
		return Summary{}, err
	}

	seen := make(map[string]struct{})
	summary := Summary{}

	walkErr := filepath.WalkDir(lib.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == lib.Path {
				return walkErr
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == lib.Path {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedExtension(filepath.Ext(entry.Name())) {
			return nil
		}

		record, ok := fileRecord(lib.Path, path, entry)
		if !ok {
			return nil
		}
		seen[record.RelativePath] = struct{}{}

		current, ok := existing[record.RelativePath]
		if !ok {
			if err := store.InsertDiscovered(ctx, lib.ID, record); err != nil {
				return err
			}
			summary.New++
			return nil
		}
		if current.FileSize == record.FileSize && current.FileModifiedAt == record.FileModifiedAt && current.Status != books.StatusMissing {
			summary.Unchanged++
			return nil
		}
		if err := store.MarkChanged(ctx, current.ID, record); err != nil {
			return err
		}
		summary.Changed++
		return nil
	})
	if walkErr != nil {
		return Summary{}, fmt.Errorf("scan library: %w", walkErr)
	}

	for relativePath, book := range existing {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		if err := store.MarkMissing(ctx, book.ID); err != nil {
			return Summary{}, err
		}
		summary.Missing++
	}

	return summary, nil
}

func fileRecord(root, path string, entry fs.DirEntry) (books.FileRecord, bool) {
	info, err := entry.Info()
	if err != nil {
		return books.FileRecord{}, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return books.FileRecord{}, false
	}
	if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return books.FileRecord{}, false
	}

	return books.FileRecord{
		RelativePath:   filepath.ToSlash(rel),
		FileSize:       info.Size(),
		FileModifiedAt: formatModTime(info.ModTime()),
	}, true
}

func formatModTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func supportedExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".pdf", ".epub", ".mobi":
		return true
	default:
		return false
	}
}
