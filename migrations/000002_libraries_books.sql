CREATE TABLE libraries (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL CHECK (length(trim(name)) > 0),
	path TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE books (
	id TEXT PRIMARY KEY,
	library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	relative_path TEXT NOT NULL CHECK (length(trim(relative_path)) > 0),
	filename TEXT NOT NULL CHECK (length(trim(filename)) > 0),
	extension TEXT NOT NULL CHECK (length(trim(extension)) > 0),
	file_size INTEGER NOT NULL CHECK (file_size >= 0),
	file_modified_at TEXT NOT NULL,
	page_count INTEGER CHECK (page_count IS NULL OR page_count >= 0),
	status TEXT NOT NULL CHECK (status IN ('discovered', 'changed', 'missing')),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE (library_id, relative_path)
);

CREATE INDEX idx_books_library_id ON books(library_id);
CREATE INDEX idx_books_library_status ON books(library_id, status);
CREATE INDEX idx_books_filename ON books(filename);
