package progress

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidProgress = errors.New("invalid reading progress")

type Progress struct {
	BookID      string
	CurrentPage int
	TotalPages  *int
	Locator     *string
	Fraction    *float64
	CreatedAt   string
	UpdatedAt   string
}

type UpdateInput struct {
	CurrentPage int
	TotalPages  *int
	Locator     *string
	Fraction    *float64
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) GetProgress(ctx context.Context, bookID string) (Progress, error) {
	var progress Progress
	var totalPages sql.NullInt64
	var locator sql.NullString
	var fraction sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
SELECT book_id, current_page, total_pages, locator, fraction, created_at, updated_at
FROM reading_progress
WHERE book_id = ?`, bookID).Scan(
		&progress.BookID,
		&progress.CurrentPage,
		&totalPages,
		&locator,
		&fraction,
		&progress.CreatedAt,
		&progress.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Progress{BookID: bookID, CurrentPage: 1}, nil
	}
	if err != nil {
		return Progress{}, fmt.Errorf("get reading progress: %w", err)
	}
	progress.TotalPages = intPtr(totalPages)
	progress.Locator = stringPtr(locator)
	progress.Fraction = floatPtr(fraction)
	return progress, nil
}

func (s *Store) PutProgress(ctx context.Context, bookID string, input UpdateInput) (Progress, error) {
	if input.CurrentPage < 1 {
		return Progress{}, fmt.Errorf("%w: current_page", ErrInvalidProgress)
	}
	if input.TotalPages != nil {
		if *input.TotalPages < 1 || input.CurrentPage > *input.TotalPages {
			return Progress{}, fmt.Errorf("%w: total_pages", ErrInvalidProgress)
		}
	}
	if input.Fraction != nil && (*input.Fraction < 0 || *input.Fraction > 1) {
		return Progress{}, fmt.Errorf("%w: fraction", ErrInvalidProgress)
	}

	now := nowString()
	var totalPages any
	if input.TotalPages != nil {
		totalPages = *input.TotalPages
	}
	var locator any
	if input.Locator != nil && *input.Locator != "" {
		locator = *input.Locator
	}
	var fraction any
	if input.Fraction != nil {
		fraction = *input.Fraction
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO reading_progress (book_id, current_page, total_pages, locator, fraction, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(book_id) DO UPDATE SET
	current_page = excluded.current_page,
	total_pages = excluded.total_pages,
	locator = excluded.locator,
	fraction = excluded.fraction,
	updated_at = excluded.updated_at`,
		bookID,
		input.CurrentPage,
		totalPages,
		locator,
		fraction,
		now,
		now,
	)
	if err != nil {
		return Progress{}, fmt.Errorf("put reading progress: %w", err)
	}
	return s.GetProgress(ctx, bookID)
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
