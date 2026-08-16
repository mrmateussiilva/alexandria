package annotations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	KindBookmark = "bookmark"
	KindNote     = "note"

	exportFileName = "clippings.md"
)

var (
	ErrInvalidAnnotation = errors.New("invalid annotation")
	ErrNotFound          = errors.New("annotation not found")
)

type Annotation struct {
	ID         string
	BookID     string
	Kind       string
	PageNumber *int
	TotalPages *int
	Locator    *string
	Fraction   *float64
	Note       string
	CreatedAt  string
	UpdatedAt  string
}

type CreateInput struct {
	BookID       string
	BookTitle    string
	RelativePath string
	Kind         string
	PageNumber   *int
	TotalPages   *int
	Locator      *string
	Fraction     *float64
	Note         string
}

type Store struct {
	db        *sql.DB
	exportDir string
	mu        sync.Mutex
}

func NewStore(db *sql.DB, exportDir string) *Store {
	return &Store{db: db, exportDir: exportDir}
}

func (s *Store) ListByBook(ctx context.Context, bookID string) ([]Annotation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, book_id, kind, page_number, total_pages, locator, fraction, note, created_at, updated_at
FROM book_annotations
WHERE book_id = ?
ORDER BY created_at DESC`, bookID)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()

	var result []Annotation
	for rows.Next() {
		value, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate annotations: %w", err)
	}
	return result, nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Annotation, error) {
	input = normalizeInput(input)
	if err := validateInput(input); err != nil {
		return Annotation{}, err
	}

	now := nowString()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO book_annotations (
	id, book_id, kind, page_number, total_pages, locator, fraction, note, created_at, updated_at
)
VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, book_id, kind, page_number, total_pages, locator, fraction, note, created_at, updated_at`,
		input.BookID,
		input.Kind,
		nullableInt(input.PageNumber),
		nullableInt(input.TotalPages),
		nullableString(input.Locator),
		nullableFloat(input.Fraction),
		input.Note,
		now,
		now,
	)
	value, err := scanAnnotation(row)
	if err != nil {
		return Annotation{}, err
	}

	if err := s.appendExport(input, value); err != nil {
		return Annotation{}, err
	}
	return value, nil
}

func (s *Store) ExportPath() string {
	if strings.TrimSpace(s.exportDir) == "" {
		return ""
	}
	return filepath.Join(s.exportDir, exportFileName)
}

func (s *Store) EnsureExportFile() (string, error) {
	path := s.ExportPath()
	if path == "" {
		return "", fmt.Errorf("annotation export path is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create annotation export directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat annotation export file: %w", err)
	}
	if err := os.WriteFile(path, []byte("# Alexandria - Notas e Marcações\n\n"), 0o644); err != nil {
		return "", fmt.Errorf("create annotation export file: %w", err)
	}
	return path, nil
}

func (s *Store) appendExport(input CreateInput, value Annotation) error {
	path := s.ExportPath()
	if path == "" {
		return fmt.Errorf("annotation export path is not configured")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create annotation export directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open annotation export file: %w", err)
	}
	defer file.Close()

	if stat, err := file.Stat(); err == nil && stat.Size() == 0 {
		if _, err := file.WriteString("# Alexandria - Notas e Marcações\n\n"); err != nil {
			return fmt.Errorf("write annotation export header: %w", err)
		}
	}

	if _, err := file.WriteString(formatExportEntry(input, value)); err != nil {
		return fmt.Errorf("write annotation export entry: %w", err)
	}
	return nil
}

type annotationScanner interface {
	Scan(dest ...any) error
}

func scanAnnotation(row annotationScanner) (Annotation, error) {
	var value Annotation
	var pageNumber, totalPages sql.NullInt64
	var locator sql.NullString
	var fraction sql.NullFloat64
	err := row.Scan(
		&value.ID,
		&value.BookID,
		&value.Kind,
		&pageNumber,
		&totalPages,
		&locator,
		&fraction,
		&value.Note,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Annotation{}, ErrNotFound
	}
	if err != nil {
		return Annotation{}, fmt.Errorf("scan annotation: %w", err)
	}
	value.PageNumber = intPtr(pageNumber)
	value.TotalPages = intPtr(totalPages)
	value.Locator = stringPtr(locator)
	value.Fraction = floatPtr(fraction)
	return value, nil
}

func normalizeInput(input CreateInput) CreateInput {
	input.BookID = strings.TrimSpace(input.BookID)
	input.BookTitle = strings.TrimSpace(input.BookTitle)
	input.RelativePath = strings.TrimSpace(input.RelativePath)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Note = strings.TrimSpace(input.Note)
	input.Locator = cleanStringPtr(input.Locator)
	return input
}

func validateInput(input CreateInput) error {
	if input.BookID == "" {
		return fmt.Errorf("%w: book_id", ErrInvalidAnnotation)
	}
	if input.Kind != KindBookmark && input.Kind != KindNote {
		return fmt.Errorf("%w: kind", ErrInvalidAnnotation)
	}
	if input.Kind == KindNote && input.Note == "" {
		return fmt.Errorf("%w: note", ErrInvalidAnnotation)
	}
	if input.PageNumber != nil && *input.PageNumber <= 0 {
		return fmt.Errorf("%w: page_number", ErrInvalidAnnotation)
	}
	if input.TotalPages != nil && *input.TotalPages <= 0 {
		return fmt.Errorf("%w: total_pages", ErrInvalidAnnotation)
	}
	if input.Fraction != nil && (*input.Fraction < 0 || *input.Fraction > 1) {
		return fmt.Errorf("%w: fraction", ErrInvalidAnnotation)
	}
	if input.PageNumber == nil && input.Locator == nil && input.Fraction == nil {
		return fmt.Errorf("%w: location", ErrInvalidAnnotation)
	}
	return nil
}

func formatExportEntry(input CreateInput, value Annotation) string {
	var builder strings.Builder
	title := input.BookTitle
	if title == "" {
		title = input.RelativePath
	}
	if title == "" {
		title = value.BookID
	}

	builder.WriteString("## ")
	builder.WriteString(markdownLine(title))
	builder.WriteString("\n\n")
	builder.WriteString("- Tipo: ")
	builder.WriteString(kindLabel(value.Kind))
	builder.WriteString("\n")
	if input.RelativePath != "" {
		builder.WriteString("- Arquivo: ")
		builder.WriteString(markdownLine(input.RelativePath))
		builder.WriteString("\n")
	}
	if value.PageNumber != nil {
		builder.WriteString("- Página: ")
		builder.WriteString(fmt.Sprintf("%d", *value.PageNumber))
		if value.TotalPages != nil {
			builder.WriteString(fmt.Sprintf(" de %d", *value.TotalPages))
		}
		builder.WriteString("\n")
	}
	if value.Fraction != nil {
		builder.WriteString("- Progresso: ")
		builder.WriteString(fmt.Sprintf("%d%%", int((*value.Fraction)*100)))
		builder.WriteString("\n")
	}
	if value.Locator != nil {
		builder.WriteString("- Localização: ")
		builder.WriteString(markdownLine(*value.Locator))
		builder.WriteString("\n")
	}
	builder.WriteString("- Criado em: ")
	builder.WriteString(value.CreatedAt)
	builder.WriteString("\n\n")
	if value.Note != "" {
		for _, line := range strings.Split(value.Note, "\n") {
			builder.WriteString("> ")
			builder.WriteString(markdownLine(line))
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("---\n\n")
	return builder.String()
}

func kindLabel(kind string) string {
	if kind == KindNote {
		return "Nota"
	}
	return "Marcação"
}

func markdownLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimRight(value, "\n")
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func floatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
