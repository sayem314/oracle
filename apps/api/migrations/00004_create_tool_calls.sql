-- +goose Up
CREATE TABLE tool_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
	call_id TEXT NOT NULL,
	name TEXT NOT NULL,
	arguments TEXT NOT NULL DEFAULT '',
	result TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tool_calls_message ON tool_calls (message_id, id);

-- +goose Down
DROP TABLE tool_calls;
