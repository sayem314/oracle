-- name: CreateSession :one
INSERT INTO sessions (title)
VALUES (?)
RETURNING *;

-- name: GetSession :one
SELECT *
FROM sessions
WHERE id = ?;

-- name: ListSessions :many
SELECT *
FROM sessions
ORDER BY updated_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: UpdateSessionTitle :exec
UPDATE sessions
SET title = ?
WHERE id = ?;

-- name: TouchSession :exec
UPDATE sessions
SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;

-- name: ListDueSessionLoops :many
SELECT *
FROM sessions
WHERE loop_next_run_at IS NOT NULL
  AND loop_next_run_at <= ?
ORDER BY loop_next_run_at
LIMIT 50;

-- name: ClaimSessionLoop :execrows
UPDATE sessions
SET loop_last_status = 'running',
    loop_last_run_at = ?,
    loop_error = '',
    loop_next_run_at = NULL
WHERE id = ?
  AND loop_next_run_at = ?
  AND loop_last_status != 'running';

-- name: UpdateSessionLoopResult :exec
UPDATE sessions
SET loop_last_status = ?,
    loop_error = ?,
    loop_next_run_at = ?,
    loop_run_count = loop_run_count + 1
WHERE id = ?;

-- name: RecoverStaleLoops :execrows
UPDATE sessions
SET loop_last_status = 'error',
    loop_error = 'interrupted by restart',
    loop_next_run_at = CASE WHEN loop_enabled = 1 THEN CAST(strftime('%s', 'now') AS INTEGER) ELSE NULL END
WHERE loop_last_status = 'running';

-- name: UpdateSessionLoop :exec
UPDATE sessions
SET loop_enabled = ?,
    loop_interval = ?,
    loop_next_run_at = ?
WHERE id = ?;

-- name: SetSessionLoopNextRun :exec
UPDATE sessions
SET loop_next_run_at = ?
WHERE id = ?;
