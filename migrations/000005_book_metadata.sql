CREATE TABLE book_metadata (
	book_id TEXT PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
	provider TEXT NOT NULL CHECK (length(trim(provider)) > 0),
	provider_key TEXT NOT NULL CHECK (length(trim(provider_key)) > 0),
	title TEXT NOT NULL CHECK (length(trim(title)) > 0),
	authors TEXT NOT NULL DEFAULT '[]',
	published_year INTEGER,
	cover_url TEXT,
	cover_path TEXT,
	source_url TEXT,
	confidence REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE metadata_jobs (
	id TEXT PRIMARY KEY,
	book_id TEXT NOT NULL UNIQUE REFERENCES books(id) ON DELETE CASCADE,
	status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
	attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
	last_error TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	completed_at TEXT
);

CREATE INDEX idx_metadata_jobs_status_updated ON metadata_jobs(status, updated_at);
