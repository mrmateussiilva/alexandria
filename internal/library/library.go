package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrDuplicate   = errors.New("library already exists")
	ErrInvalidName = errors.New("library name is required")
	ErrInvalidPath = errors.New("library path is invalid")
	ErrNotFound    = errors.New("library not found")
)

type Library struct {
	ID        string
	Name      string
	Path      string
	CreatedAt string
	UpdatedAt string
}

type CreateInput struct {
	Name string
	Path string
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) CreateLibrary(ctx context.Context, input CreateInput) (Library, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Library{}, ErrInvalidName
	}

	path, err := normalizeDirectory(input.Path)
	if err != nil {
		return Library{}, err
	}

	now := nowString()
	var lib Library
	err = s.db.QueryRowContext(ctx, `
INSERT INTO libraries (id, name, path, created_at, updated_at)
VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?)
RETURNING id, name, path, created_at, updated_at`,
		name, path, now, now,
	).Scan(&lib.ID, &lib.Name, &lib.Path, &lib.CreatedAt, &lib.UpdatedAt)
	if err != nil {
		if isUniqueConstraint(err) {
			return Library{}, ErrDuplicate
		}
		return Library{}, fmt.Errorf("create library: %w", err)
	}

	return lib, nil
}

func (s *Service) GetLibrary(ctx context.Context, id string) (Library, error) {
	var lib Library
	err := s.db.QueryRowContext(ctx, `
SELECT id, name, path, created_at, updated_at
FROM libraries
WHERE id = ?`, id).Scan(&lib.ID, &lib.Name, &lib.Path, &lib.CreatedAt, &lib.UpdatedAt)
	if err == sql.ErrNoRows {
		return Library{}, ErrNotFound
	}
	if err != nil {
		return Library{}, fmt.Errorf("get library: %w", err)
	}
	return lib, nil
}

func (s *Service) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, path, created_at, updated_at
FROM libraries
ORDER BY name COLLATE NOCASE, created_at`)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Path, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan library: %w", err)
		}
		libs = append(libs, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate libraries: %w", err)
	}

	return libs, nil
}

func (s *Service) DeleteLibrary(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM libraries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted library: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func normalizeDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: path is required", ErrInvalidPath)
	}

	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("%w: make absolute: %v", ErrInvalidPath, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: not a directory", ErrInvalidPath)
	}

	return filepath.Clean(resolved), nil
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isUniqueConstraint(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed")
}
