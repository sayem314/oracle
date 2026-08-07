-- name: GetSettings :one
SELECT *
FROM settings
WHERE id = 1;

-- name: UpsertSettings :one
INSERT INTO settings (id, permission_default, permission_rules, instructions)
VALUES (1, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	permission_default = excluded.permission_default,
	permission_rules = excluded.permission_rules,
	instructions = excluded.instructions,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;