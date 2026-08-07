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
