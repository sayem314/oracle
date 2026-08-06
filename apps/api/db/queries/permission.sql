-- name: GetUserPermissions :one
SELECT *
FROM user_permissions
WHERE user_id = ?;

-- name: UpsertUserPermissions :one
INSERT INTO user_permissions (user_id, default_verdict, rules, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (user_id) DO UPDATE SET
	default_verdict = excluded.default_verdict,
	rules = excluded.rules,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeleteUserPermissions :exec
DELETE FROM user_permissions
WHERE user_id = ?;
