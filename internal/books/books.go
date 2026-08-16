package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	StatusDiscovered = "discovered"
	StatusChanged    = "changed"
	StatusMissing    = "missing"
)

var (
	ErrInvalidFilter   = errors.New("invalid book filter")
	ErrNotFound        = errors.New("book not found")
	ErrFileUnavailable = errors.New("book file is unavailable")
)

type Book struct {
	ID             string
	LibraryID      string
	RelativePath   string
	Filename       string
	Extension      string
	FileSize       int64
	FileModifiedAt string
	PageCount      *int
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

type Folder struct {
	Path       string
	Name       string
	ParentPath string
	BookCount  int
}

type ExistingBook struct {
	ID             string
	RelativePath   string
	FileSize       int64
	FileModifiedAt string
	Status         string
}

type FileRecord struct {
	RelativePath   string
	FileSize       int64
	FileModifiedAt string
}

type FileLocation struct {
	Book
	LibraryPath string
}

type ListFilter struct {
	LibraryID string
	Status    string
	Query     string
	Folder    string
	Limit     int
	Offset    int
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) GetBook(ctx context.Context, id string) (Book, error) {
	var book Book
	var pageCount sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT id, library_id, relative_path, filename, extension, file_size, file_modified_at, page_count, status, created_at, updated_at
FROM books
WHERE id = ?`, id).Scan(
		&book.ID,
		&book.LibraryID,
		&book.RelativePath,
		&book.Filename,
		&book.Extension,
		&book.FileSize,
		&book.FileModifiedAt,
		&pageCount,
		&book.Status,
		&book.CreatedAt,
		&book.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("get book: %w", err)
	}
	book.PageCount = intPtr(pageCount)
	return book, nil
}

func (s *Store) GetFileLocation(ctx context.Context, id string) (FileLocation, error) {
	var location FileLocation
	var pageCount sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT b.id, b.library_id, b.relative_path, b.filename, b.extension, b.file_size, b.file_modified_at, b.page_count, b.status, b.created_at, b.updated_at, l.path
FROM books b
JOIN libraries l ON l.id = b.library_id
WHERE b.id = ?`, id).Scan(
		&location.ID,
		&location.LibraryID,
		&location.RelativePath,
		&location.Filename,
		&location.Extension,
		&location.FileSize,
		&location.FileModifiedAt,
		&pageCount,
		&location.Status,
		&location.CreatedAt,
		&location.UpdatedAt,
		&location.LibraryPath,
	)
	if err == sql.ErrNoRows {
		return FileLocation{}, ErrNotFound
	}
	if err != nil {
		return FileLocation{}, fmt.Errorf("get book file location: %w", err)
	}
	if location.Status == StatusMissing {
		return FileLocation{}, ErrFileUnavailable
	}
	location.PageCount = intPtr(pageCount)
	return location, nil
}

func (s *Store) ListBooks(ctx context.Context, filter ListFilter) ([]Book, error) {
	filter = normalizeFilter(filter)
	if filter.Status != "" && !ValidStatus(filter.Status) {
		return nil, fmt.Errorf("%w: status", ErrInvalidFilter)
	}
	if filter.Folder != "" {
		folder, err := NormalizeFolderPath(filter.Folder)
		if err != nil {
			return nil, err
		}
		filter.Folder = folder
	}

	query := `
SELECT id, library_id, relative_path, filename, extension, file_size, file_modified_at, page_count, status, created_at, updated_at
FROM books`
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 6)

	if filter.LibraryID != "" {
		conditions = append(conditions, "library_id = ?")
		args = append(args, filter.LibraryID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		conditions = append(conditions, "(filename LIKE ? ESCAPE '\\' OR relative_path LIKE ? ESCAPE '\\')")
		pattern := likePattern(filter.Query)
		args = append(args, pattern, pattern)
	}
	if filter.Folder != "" {
		conditions = append(conditions, "relative_path LIKE ? ESCAPE '\\'")
		args = append(args, likePrefix(filter.Folder+"/"))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY relative_path COLLATE NOCASE LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	var result []Book
	for rows.Next() {
		var book Book
		var pageCount sql.NullInt64
		if err := rows.Scan(
			&book.ID,
			&book.LibraryID,
			&book.RelativePath,
			&book.Filename,
			&book.Extension,
			&book.FileSize,
			&book.FileModifiedAt,
			&pageCount,
			&book.Status,
			&book.CreatedAt,
			&book.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		book.PageCount = intPtr(pageCount)
		result = append(result, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}

	return result, nil
}

func (s *Store) ListFolders(ctx context.Context, libraryID, parentPath string) ([]Folder, error) {
	parentPath, err := NormalizeFolderPath(parentPath)
	if err != nil {
		return nil, err
	}

	query := `
SELECT relative_path
FROM books
WHERE library_id = ? AND status <> ?`
	args := []any{libraryID, StatusMissing}
	if parentPath != "" {
		query += " AND relative_path LIKE ? ESCAPE '\\'"
		args = append(args, likePrefix(parentPath+"/"))
	}
	query += " ORDER BY relative_path COLLATE NOCASE"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list folder paths: %w", err)
	}
	defer rows.Close()

	byPath := make(map[string]*Folder)
	prefix := ""
	if parentPath != "" {
		prefix = parentPath + "/"
	}

	for rows.Next() {
		var relativePath string
		if err := rows.Scan(&relativePath); err != nil {
			return nil, fmt.Errorf("scan folder path: %w", err)
		}
		if prefix != "" {
			if !strings.HasPrefix(relativePath, prefix) {
				continue
			}
			relativePath = strings.TrimPrefix(relativePath, prefix)
		}

		segment, _, ok := strings.Cut(relativePath, "/")
		if !ok || !validFolderSegment(segment) {
			continue
		}

		folderPath := segment
		if parentPath != "" {
			folderPath = parentPath + "/" + segment
		}
		folder := byPath[folderPath]
		if folder == nil {
			folder = &Folder{
				Path:       folderPath,
				Name:       segment,
				ParentPath: parentPath,
			}
			byPath[folderPath] = folder
		}
		folder.BookCount++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate folder paths: %w", err)
	}

	result := make([]Folder, 0, len(byPath))
	for _, folder := range byPath {
		result = append(result, *folder)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (s *Store) ExistingByLibrary(ctx context.Context, libraryID string) (map[string]ExistingBook, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, relative_path, file_size, file_modified_at, status
FROM books
WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list existing books: %w", err)
	}
	defer rows.Close()

	result := make(map[string]ExistingBook)
	for rows.Next() {
		var book ExistingBook
		if err := rows.Scan(&book.ID, &book.RelativePath, &book.FileSize, &book.FileModifiedAt, &book.Status); err != nil {
			return nil, fmt.Errorf("scan existing book: %w", err)
		}
		result[book.RelativePath] = book
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing books: %w", err)
	}
	return result, nil
}

func (s *Store) InsertDiscovered(ctx context.Context, libraryID string, file FileRecord) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO books (
	id, library_id, relative_path, filename, extension, file_size, file_modified_at, page_count, status, created_at, updated_at
)
VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		libraryID,
		file.RelativePath,
		filepath.Base(file.RelativePath),
		strings.ToLower(filepath.Ext(file.RelativePath)),
		file.FileSize,
		file.FileModifiedAt,
		StatusDiscovered,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert discovered book: %w", err)
	}
	return nil
}

func (s *Store) MarkChanged(ctx context.Context, id string, file FileRecord) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
UPDATE books
SET file_size = ?, file_modified_at = ?, filename = ?, extension = ?, status = ?, updated_at = ?
WHERE id = ?`,
		file.FileSize,
		file.FileModifiedAt,
		filepath.Base(file.RelativePath),
		strings.ToLower(filepath.Ext(file.RelativePath)),
		StatusChanged,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("mark changed book: %w", err)
	}
	return nil
}

func (s *Store) MarkMissing(ctx context.Context, id string) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
UPDATE books
SET status = ?, updated_at = ?
WHERE id = ? AND status <> ?`, StatusMissing, now, id, StatusMissing)
	if err != nil {
		return fmt.Errorf("mark missing book: %w", err)
	}
	return nil
}

func ValidStatus(status string) bool {
	switch status {
	case StatusDiscovered, StatusChanged, StatusMissing:
		return true
	default:
		return false
	}
}

func SupportedReaderExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".pdf", ".epub", ".mobi":
		return true
	default:
		return false
	}
}

func NormalizeFolderPath(folderPath string) (string, error) {
	folderPath = strings.TrimSpace(strings.ReplaceAll(folderPath, "\\", "/"))
	if folderPath == "" || folderPath == "." {
		return "", nil
	}
	if strings.HasPrefix(folderPath, "/") {
		return "", fmt.Errorf("%w: folder", ErrInvalidFilter)
	}

	clean := path.Clean(folderPath)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: folder", ErrInvalidFilter)
	}
	for _, segment := range strings.Split(clean, "/") {
		if !validFolderSegment(segment) {
			return "", fmt.Errorf("%w: folder", ErrInvalidFilter)
		}
	}
	return clean, nil
}

func FolderPath(relativePath string) string {
	relativePath = strings.Trim(strings.ReplaceAll(relativePath, "\\", "/"), "/")
	index := strings.LastIndex(relativePath, "/")
	if index < 0 {
		return ""
	}
	return relativePath[:index]
}

func TopLevelFolder(relativePath string) string {
	folder := FolderPath(relativePath)
	if folder == "" {
		return ""
	}
	segment, _, _ := strings.Cut(folder, "/")
	return segment
}

func normalizeFilter(filter ListFilter) ListFilter {
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func likePattern(query string) string {
	return "%" + likeEscaped(query) + "%"
}

func likePrefix(prefix string) string {
	return likeEscaped(prefix) + "%"
}

func likeEscaped(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func validFolderSegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".."
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}
