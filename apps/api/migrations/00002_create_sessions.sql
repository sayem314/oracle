-- +goose Up
CREATE TABLE sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	loop_enabled INTEGER NOT NULL DEFAULT 0,
	loop_interval TEXT NOT NULL DEFAULT '',
	loop_next_run_at INTEGER,
	loop_last_run_at INTEGER,
	loop_last_status TEXT NOT NULL DEFAULT '',
	loop_error TEXT NOT NULL DEFAULT '',
	loop_run_count INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sessions_updated ON sessions (updated_at DESC);

CREATE INDEX idx_sessions_loop_due ON sessions (loop_next_run_at)
WHERE loop_next_run_at IS NOT NULL;

-- +goose Down
DROP TABLE sessions;
