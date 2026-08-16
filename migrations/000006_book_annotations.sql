CREATE TABLE book_annotations (
	id TEXT PRIMARY KEY,
	book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('bookmark', 'note')),
	page_number INTEGER CHECK (page_number IS NULL OR page_number > 0),
	total_pages INTEGER CHECK (total_pages IS NULL OR total_pages > 0),
	locator TEXT,
	fraction REAL CHECK (fraction IS NULL OR (fraction >= 0 AND fraction <= 1)),
	note TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX idx_book_annotations_book_created ON book_annotations(book_id, created_at DESC);
