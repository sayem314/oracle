-- name: CreateLLMProvider :one
INSERT INTO llm_providers (name, provider, base_url, api_key, is_default)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLLMProvider :one
SELECT *
FROM llm_providers
WHERE id = ?;

-- name: ListLLMProviders :many
SELECT *
FROM llm_providers
ORDER BY id;

-- name: GetDefaultLLMProvider :one
SELECT *
FROM llm_providers
WHERE is_default = 1;

-- name: UpdateLLMProvider :one
UPDATE llm_providers
SET name = ?, provider = ?, base_url = ?, api_key = ?, is_default = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: ClearDefaultLLMProviders :exec
UPDATE llm_providers
SET is_default = 0
WHERE is_default = 1;

-- name: DeleteLLMProvider :exec
DELETE FROM llm_providers
WHERE id = ?;

-- name: ListLLMModelsByProvider :many
SELECT *
FROM llm_models
WHERE provider_id = ?
ORDER BY id;

-- name: InsertLLMModel :exec
INSERT INTO llm_models (provider_id, name, is_default)
VALUES (?, ?, ?);

-- name: DeleteLLMModelsByProvider :exec
DELETE FROM llm_models
WHERE provider_id = ?;
