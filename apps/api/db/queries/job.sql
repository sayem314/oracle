-- name: CreateJob :one
INSERT INTO jobs (session_id, schedule, prompt, enabled, next_run_at, model)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetJob :one
SELECT *
FROM jobs
WHERE id = ?;

-- name: ListJobs :many
SELECT *
FROM jobs
ORDER BY id;

-- name: UpdateJob :one
UPDATE jobs
SET schedule = ?, prompt = ?, enabled = ?, next_run_at = ?, model = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteJob :exec
DELETE FROM jobs
WHERE id = ?;

-- name: ListDueJobs :many
SELECT *
FROM jobs
WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
ORDER BY next_run_at, id;

-- name: ClaimJob :execrows
UPDATE jobs
SET next_run_at = sqlc.arg(new_next_run_at),
	last_run_at = sqlc.arg(last_run_at),
	last_status = 'running'
WHERE id = sqlc.arg(id)
	AND enabled = 1
	AND next_run_at = sqlc.arg(expected_next_run_at);

-- name: SetJobStatus :exec
UPDATE jobs
SET last_status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetJobSession :exec
UPDATE jobs
SET session_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;