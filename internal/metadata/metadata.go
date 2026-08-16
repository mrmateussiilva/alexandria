package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ProviderOpenLibrary = "openlibrary"
	ProviderGemini      = "gemini"
	ProviderLocal       = "local"

	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

var (
	ErrNotFound      = errors.New("metadata not found")
	ErrNoQueuedJob   = errors.New("no queued metadata job")
	ErrJobInProgress = errors.New("metadata job already running")
	ErrInvalidFilter = errors.New("invalid metadata job filter")
)

type Metadata struct {
	BookID        string
	Provider      string
	ProviderKey   string
	Title         string
	Authors       []string
	Description   string
	PublishedYear *int
	CoverURL      *string
	CoverPath     *string
	SourceURL     *string
	Confidence    float64
	CreatedAt     string
	UpdatedAt     string
}

type Job struct {
	ID           string
	BookID       string
	Filename     string
	RelativePath string
	Status       string
	Attempts     int
	LastError    *string
	CreatedAt    string
	UpdatedAt    string
	CompletedAt  *string
}

type JobFilter struct {
	LibraryID string
	Status    string
	Limit     int
	Offset    int
}

type EnqueueMissingInput struct {
	LibraryID string
	Limit     int
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Get(ctx context.Context, bookID string) (Metadata, error) {
	var value Metadata
	var authors string
	var description string
	var published sql.NullInt64
	var coverURL, coverPath, sourceURL sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT book_id, provider, provider_key, title, authors, description, published_year, cover_url, cover_path, source_url, confidence, created_at, updated_at
FROM book_metadata
WHERE book_id = ?`, bookID).Scan(
		&value.BookID,
		&value.Provider,
		&value.ProviderKey,
		&value.Title,
		&authors,
		&description,
		&published,
		&coverURL,
		&coverPath,
		&sourceURL,
		&value.Confidence,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("get book metadata: %w", err)
	}
	if err := json.Unmarshal([]byte(authors), &value.Authors); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata authors: %w", err)
	}
	if published.Valid {
		year := int(published.Int64)
		value.PublishedYear = &year
	}
	value.CoverURL = stringPtr(coverURL)
	value.CoverPath = stringPtr(coverPath)
	value.SourceURL = stringPtr(sourceURL)
	value.Description = description
	return value, nil
}

func (s *Store) Upsert(ctx context.Context, value Metadata) error {
	authors, err := json.Marshal(value.Authors)
	if err != nil {
		return fmt.Errorf("encode metadata authors: %w", err)
	}
	now := nowString()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO book_metadata (
	book_id, provider, provider_key, title, authors, description, published_year, cover_url, cover_path, source_url, confidence, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(book_id) DO UPDATE SET
	provider = excluded.provider,
	provider_key = excluded.provider_key,
	title = excluded.title,
	authors = excluded.authors,
	description = excluded.description,
	published_year = excluded.published_year,
	cover_url = excluded.cover_url,
	cover_path = excluded.cover_path,
	source_url = excluded.source_url,
	confidence = excluded.confidence,
	updated_at = excluded.updated_at`,
		value.BookID,
		value.Provider,
		value.ProviderKey,
		value.Title,
		string(authors),
		value.Description,
		nullableInt(value.PublishedYear),
		nullableString(value.CoverURL),
		nullableString(value.CoverPath),
		nullableString(value.SourceURL),
		value.Confidence,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert book metadata: %w", err)
	}
	return nil
}

func (s *Store) Enqueue(ctx context.Context, bookID string) (Job, error) {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO metadata_jobs (id, book_id, status, attempts, last_error, created_at, updated_at, completed_at)
VALUES (lower(hex(randomblob(16))), ?, ?, 0, NULL, ?, ?, NULL)
ON CONFLICT(book_id) DO UPDATE SET
	status = ?,
	last_error = NULL,
	updated_at = ?,
	completed_at = NULL
WHERE metadata_jobs.status <> ?`,
		bookID,
		JobQueued,
		now,
		now,
		JobQueued,
		now,
		JobRunning,
	)
	if err != nil {
		return Job{}, fmt.Errorf("enqueue metadata job: %w", err)
	}

	job, err := s.GetJobByBook(ctx, bookID)
	if err != nil {
		return Job{}, err
	}
	if job.Status == JobRunning {
		return job, ErrJobInProgress
	}
	return job, nil
}

func (s *Store) EnqueueMissing(ctx context.Context, input EnqueueMissingInput) (int, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := `
SELECT b.id
FROM books b
LEFT JOIN book_metadata m ON m.book_id = b.id
LEFT JOIN metadata_jobs j ON j.book_id = b.id AND j.status IN (?, ?)
WHERE b.status <> 'missing' AND m.book_id IS NULL AND j.book_id IS NULL`
	args := []any{JobQueued, JobRunning}
	if strings.TrimSpace(input.LibraryID) != "" {
		query += " AND b.library_id = ?"
		args = append(args, strings.TrimSpace(input.LibraryID))
	}
	query += " ORDER BY b.relative_path COLLATE NOCASE LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("list books missing metadata: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan book missing metadata: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate books missing metadata: %w", err)
	}

	queued := 0
	for _, id := range ids {
		if _, err := s.Enqueue(ctx, id); err != nil && !errors.Is(err, ErrJobInProgress) {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func (s *Store) GetJobByBook(ctx context.Context, bookID string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `
SELECT id, book_id, status, attempts, last_error, created_at, updated_at, completed_at
FROM metadata_jobs
WHERE book_id = ?`, bookID))
}

func (s *Store) ListJobs(ctx context.Context, filter JobFilter) ([]Job, error) {
	filter = normalizeJobFilter(filter)
	if filter.Status != "" && !ValidJobStatus(filter.Status) {
		return nil, fmt.Errorf("%w: status", ErrInvalidFilter)
	}

	query := `
SELECT j.id, j.book_id, b.filename, b.relative_path, j.status, j.attempts, j.last_error, j.created_at, j.updated_at, j.completed_at
FROM metadata_jobs j
JOIN books b ON b.id = j.book_id`
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if filter.LibraryID != "" {
		conditions = append(conditions, "b.library_id = ?")
		args = append(args, filter.LibraryID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "j.status = ?")
		args = append(args, filter.Status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY j.updated_at DESC, j.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list metadata jobs: %w", err)
	}
	defer rows.Close()

	var result []Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metadata jobs: %w", err)
	}
	return result, nil
}

func (s *Store) ClaimNext(ctx context.Context) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin metadata job claim: %w", err)
	}
	defer tx.Rollback()

	job, err := scanJob(tx.QueryRowContext(ctx, `
SELECT id, book_id, status, attempts, last_error, created_at, updated_at, completed_at
FROM metadata_jobs
WHERE status = ?
ORDER BY updated_at, created_at
LIMIT 1`, JobQueued))
	if err == ErrNotFound {
		return Job{}, ErrNoQueuedJob
	}
	if err != nil {
		return Job{}, err
	}

	now := nowString()
	result, err := tx.ExecContext(ctx, `
UPDATE metadata_jobs
SET status = ?, attempts = attempts + 1, updated_at = ?
WHERE id = ? AND status = ?`, JobRunning, now, job.ID, JobQueued)
	if err != nil {
		return Job{}, fmt.Errorf("mark metadata job running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Job{}, fmt.Errorf("check claimed metadata job: %w", err)
	}
	if rows == 0 {
		return Job{}, ErrNoQueuedJob
	}
	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit metadata job claim: %w", err)
	}

	job.Status = JobRunning
	job.Attempts++
	job.UpdatedAt = now
	return job, nil
}

func (s *Store) Complete(ctx context.Context, jobID string) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
UPDATE metadata_jobs
SET status = ?, last_error = NULL, updated_at = ?, completed_at = ?
WHERE id = ?`, JobSucceeded, now, now, jobID)
	if err != nil {
		return fmt.Errorf("complete metadata job: %w", err)
	}
	return nil
}

func (s *Store) Fail(ctx context.Context, jobID string, cause error) error {
	now := nowString()
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE metadata_jobs
SET status = ?, last_error = ?, updated_at = ?, completed_at = ?
WHERE id = ?`, JobFailed, message, now, now, jobID)
	if err != nil {
		return fmt.Errorf("fail metadata job: %w", err)
	}
	return nil
}

func scanJob(row *sql.Row) (Job, error) {
	var job Job
	var lastError, completedAt sql.NullString
	err := row.Scan(
		&job.ID,
		&job.BookID,
		&job.Status,
		&job.Attempts,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan metadata job: %w", err)
	}
	job.LastError = stringPtr(lastError)
	job.CompletedAt = stringPtr(completedAt)
	return job, nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJobRows(row jobScanner) (Job, error) {
	var job Job
	var lastError, completedAt sql.NullString
	err := row.Scan(
		&job.ID,
		&job.BookID,
		&job.Filename,
		&job.RelativePath,
		&job.Status,
		&job.Attempts,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	)
	if err != nil {
		return Job{}, fmt.Errorf("scan metadata job: %w", err)
	}
	job.LastError = stringPtr(lastError)
	job.CompletedAt = stringPtr(completedAt)
	return job, nil
}

func ValidJobStatus(status string) bool {
	switch status {
	case JobQueued, JobRunning, JobSucceeded, JobFailed:
		return true
	default:
		return false
	}
}

func normalizeJobFilter(filter JobFilter) JobFilter {
	filter.LibraryID = strings.TrimSpace(filter.LibraryID)
	filter.Status = strings.TrimSpace(filter.Status)
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

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func CleanTitle(filename, extension string) string {
	title := filename
	if extension != "" && strings.HasSuffix(strings.ToLower(title), strings.ToLower(extension)) {
		title = strings.TrimSpace(title[:len(title)-len(extension)])
	}
	if beforeAuthor, _, ok := strings.Cut(title, " - "); ok {
		title = beforeAuthor
	}
	title = stripBracketedSuffix(title)
	replacer := strings.NewReplacer("_", " ", ".", " ", "-", " ")
	title = replacer.Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	return title
}

func stripBracketedSuffix(value string) string {
	for {
		value = strings.TrimSpace(value)
		if value == "" {
			return value
		}
		last := value[len(value)-1]
		var open byte
		switch last {
		case ')':
			open = '('
		case ']':
			open = '['
		default:
			return value
		}
		index := strings.LastIndexByte(value, open)
		if index < 0 {
			return value
		}
		value = value[:index]
	}
}
