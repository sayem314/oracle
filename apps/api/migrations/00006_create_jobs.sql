-- +goose Up
CREATE TABLE jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER REFERENCES sessions (id) ON DELETE SET NULL,
	schedule TEXT NOT NULL,
	prompt TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	last_run_at DATETIME,
	last_status TEXT NOT NULL DEFAULT '',
	next_run_at DATETIME,
	model TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_jobs_due ON jobs (enabled, next_run_at);

-- +goose Down
DROP TABLE jobs;
