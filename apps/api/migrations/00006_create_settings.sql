-- +goose Up
CREATE TABLE settings (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	permission_default TEXT NOT NULL DEFAULT 'ask',
	permission_rules TEXT NOT NULL DEFAULT '',
	instructions TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE settings;
