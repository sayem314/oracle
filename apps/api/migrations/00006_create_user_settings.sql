-- +goose Up
CREATE TABLE user_settings (
	user_id INTEGER PRIMARY KEY REFERENCES auth_users (id) ON DELETE CASCADE,
	llm_provider TEXT NOT NULL DEFAULT '',
	llm_base_url TEXT NOT NULL DEFAULT '',
	llm_api_key TEXT NOT NULL DEFAULT '',
	llm_model TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE user_settings;
