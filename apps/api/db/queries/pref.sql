-- name: GetUserLLMPrefs :one
SELECT *
FROM user_llm_prefs
WHERE user_id = ?;

-- name: UpsertUserLLMPrefs :one
INSERT INTO user_llm_prefs (user_id, provider_id, model, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (user_id) DO UPDATE SET
	provider_id = excluded.provider_id,
	model = excluded.model,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeleteUserLLMPrefs :exec
DELETE FROM user_llm_prefs
WHERE user_id = ?;
