-- +goose Up
DROP TABLE llm_models;
DROP TABLE llm_providers;

CREATE TABLE llm_providers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	provider TEXT NOT NULL,
	base_url TEXT NOT NULL,
	api_key TEXT NOT NULL,
	is_default INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE llm_models (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id INTEGER NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	is_default INTEGER NOT NULL DEFAULT 0,
	UNIQUE (provider_id, name)
);

CREATE INDEX idx_llm_models_provider ON llm_models (provider_id, id);

CREATE TABLE user_llm_prefs (
	user_id INTEGER PRIMARY KEY REFERENCES auth_users (id) ON DELETE CASCADE,
	provider_id INTEGER REFERENCES llm_providers (id) ON DELETE SET NULL,
	model TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE user_llm_prefs;
DROP TABLE llm_models;
DROP TABLE llm_providers;

CREATE TABLE llm_providers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	provider TEXT NOT NULL,
	base_url TEXT NOT NULL,
	api_key TEXT NOT NULL,
	is_default INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (user_id, name)
);

CREATE INDEX idx_llm_providers_user ON llm_providers (user_id, id);

CREATE TABLE llm_models (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id INTEGER NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	is_default INTEGER NOT NULL DEFAULT 0,
	UNIQUE (provider_id, name)
);

CREATE INDEX idx_llm_models_provider ON llm_models (provider_id, id);
