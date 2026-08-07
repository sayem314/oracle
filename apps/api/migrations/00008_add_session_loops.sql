-- +goose Up
ALTER TABLE sessions ADD COLUMN loop_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN loop_interval TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN loop_next_run_at INTEGER;
ALTER TABLE sessions ADD COLUMN loop_last_run_at INTEGER;
ALTER TABLE sessions ADD COLUMN loop_last_status TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN loop_error TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_sessions_loop_due ON sessions (loop_next_run_at)
WHERE loop_next_run_at IS NOT NULL;

-- +goose Down
DROP INDEX idx_sessions_loop_due;
ALTER TABLE sessions DROP COLUMN loop_error;
ALTER TABLE sessions DROP COLUMN loop_last_status;
ALTER TABLE sessions DROP COLUMN loop_last_run_at;
ALTER TABLE sessions DROP COLUMN loop_next_run_at;
ALTER TABLE sessions DROP COLUMN loop_interval;
ALTER TABLE sessions DROP COLUMN loop_enabled;
