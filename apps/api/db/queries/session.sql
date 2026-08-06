-- name: CreateSession :one
INSERT INTO sessions (user_id, title)
VALUES (?, ?)
RETURNING *;

-- name: GetSession :one
SELECT *
FROM sessions
WHERE id = ?;

-- name: ListSessions :many
SELECT *
FROM sessions
WHERE user_id = ?
ORDER BY updated_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: UpdateSessionTitle :exec
UPDATE sessions
SET title = ?
WHERE id = ?;

-- name: UpdateSessionSummary :exec
UPDATE sessions
SET summary = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: TouchSession :exec
UPDATE sessions
SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;
