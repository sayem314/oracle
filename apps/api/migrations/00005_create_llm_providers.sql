-- +goose Up
CREATE TABLE llm_providers (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	provider TEXT NOT NULL DEFAULT 'mock',
	base_url TEXT NOT NULL DEFAULT '',
	api_key TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE llm_providers;
