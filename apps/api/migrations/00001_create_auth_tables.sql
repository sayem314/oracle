-- +goose Up
CREATE TABLE auth_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password TEXT,
	email_verified_at DATETIME,
	first_name TEXT NOT NULL DEFAULT '',
	last_name TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'user',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE auth_sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL UNIQUE,
	user_id INTEGER NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL,
	last_access DATETIME NOT NULL,
	metadata TEXT
);

CREATE INDEX idx_auth_sessions_user ON auth_sessions (user_id);

CREATE TABLE auth_verifications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	subject TEXT NOT NULL,
	value TEXT NOT NULL UNIQUE,
	expires_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_auth_verifications_subject ON auth_verifications (subject);

-- +goose Down
DROP TABLE auth_verifications;
DROP TABLE auth_sessions;
DROP TABLE auth_users;
