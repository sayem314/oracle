-- +goose Up
ALTER TABLE jobs ADD COLUMN provider_id INTEGER REFERENCES llm_providers (id) ON DELETE SET NULL;
ALTER TABLE jobs ADD COLUMN model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs DROP COLUMN model;
ALTER TABLE jobs DROP COLUMN provider_id;
