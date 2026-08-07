-- +goose Up
ALTER TABLE sessions ADD COLUMN loop_run_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN loop_run_count;
