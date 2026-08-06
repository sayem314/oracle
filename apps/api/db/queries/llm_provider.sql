-- name: GetLLMProvider :one
SELECT *
FROM llm_providers
WHERE id = 1;

-- name: UpsertLLMProvider :one
INSERT INTO llm_providers (id, provider, base_url, api_key, model)
VALUES (1, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	provider = excluded.provider,
	base_url = excluded.base_url,
	api_key = excluded.api_key,
	model = excluded.model,
	updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: SetLLMModel :one
UPDATE llm_providers
SET model = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = 1
RETURNING *;
