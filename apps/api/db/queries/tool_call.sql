-- name: InsertToolCall :one
INSERT INTO tool_calls (message_id, call_id, name, arguments, status)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateToolCallResult :exec
UPDATE tool_calls
SET result = ?, status = ?
WHERE id = ?;

-- name: SetToolCallStatus :exec
UPDATE tool_calls
SET status = ?
WHERE id = ?;

-- name: GetToolCall :one
SELECT sqlc.embed(tc), m.session_id
FROM tool_calls tc
JOIN messages m ON m.id = tc.message_id
WHERE tc.id = ?;

-- name: CountPendingApprovalsBySession :one
SELECT count(*)
FROM tool_calls tc
JOIN messages m ON m.id = tc.message_id
WHERE m.session_id = ? AND tc.status = 'awaiting_approval';

-- name: ListToolCallsBySession :many
SELECT tc.id, tc.message_id, tc.call_id, tc.name, tc.arguments, tc.result, tc.status, tc.created_at
FROM tool_calls tc
JOIN messages m ON m.id = tc.message_id
WHERE m.session_id = ?
ORDER BY tc.message_id, tc.id;