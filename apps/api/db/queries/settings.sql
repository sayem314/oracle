-- name: GetSettings :one
SELECT *
FROM settings
WHERE id = 1;

-- name: UpsertSettings :one
INSERT INTO settings (id, permission_default, permission_rules)
VALUES (1, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	permission_default = excluded.permission_default,
	permission_rules = excluded.permission_rules,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;