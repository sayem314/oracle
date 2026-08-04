-- +goose Up
CREATE TABLE sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
	title TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sessions_user_updated ON sessions (user_id, updated_at DESC);

-- +goose Down
DROP TABLE sessions;
