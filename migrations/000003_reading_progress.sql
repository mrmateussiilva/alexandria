CREATE TABLE reading_progress (
	book_id TEXT PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
	current_page INTEGER NOT NULL CHECK (current_page >= 1),
	total_pages INTEGER CHECK (total_pages IS NULL OR total_pages >= 1),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
