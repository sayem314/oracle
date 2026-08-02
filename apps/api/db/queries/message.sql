-- name: AppendMessage :one
INSERT INTO messages (session_id, role, content)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListMessages :many
SELECT *
FROM messages
WHERE session_id = ?
ORDER BY id ASC
LIMIT ? OFFSET ?;

-- name: CountMessages :one
SELECT COUNT(*)
FROM messages
WHERE session_id = ?;

-- name: DeleteMessagesBySession :exec
DELETE FROM messages
WHERE session_id = ?;
