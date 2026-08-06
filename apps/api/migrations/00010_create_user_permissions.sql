-- +goose Up
CREATE TABLE user_permissions (
	user_id INTEGER PRIMARY KEY REFERENCES auth_users (id) ON DELETE CASCADE,
	default_verdict TEXT,
	rules TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE user_permissions;
