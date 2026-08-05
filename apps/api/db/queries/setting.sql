-- name: GetUserSettings :one
SELECT *
FROM user_settings
WHERE user_id = ?;

-- name: UpsertUserSettings :one
INSERT INTO user_settings (user_id, llm_provider, llm_base_url, llm_api_key, llm_model, updated_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (user_id) DO UPDATE SET
	llm_provider = excluded.llm_provider,
	llm_base_url = excluded.llm_base_url,
	llm_api_key = excluded.llm_api_key,
	llm_model = excluded.llm_model,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeleteUserSettings :exec
DELETE FROM user_settings
WHERE user_id = ?;
