-- name: GetAPIKeyByHash :one
SELECT id, customer_id, name, key_prefix, scopes, rate_limit_rpm, expires_at, last_used_at, is_active, created_at
FROM gateway_api_keys
WHERE key_hash = $1 AND is_active = true;

-- name: ListAPIKeysByCustomer :many
SELECT id, customer_id, name, key_prefix, scopes, rate_limit_rpm, expires_at, last_used_at, is_active, created_at
FROM gateway_api_keys
WHERE customer_id = $1
ORDER BY created_at DESC;

-- name: CreateAPIKey :one
INSERT INTO gateway_api_keys (customer_id, name, key_hash, key_prefix, scopes, rate_limit_rpm)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, customer_id, name, key_prefix, scopes, rate_limit_rpm, expires_at, last_used_at, is_active, created_at;

-- name: UpdateAPIKeyLastUsed :exec
UPDATE gateway_api_keys SET last_used_at = now() WHERE id = $1;

-- name: RevokeAPIKey :exec
UPDATE gateway_api_keys SET is_active = false WHERE id = $1 AND customer_id = $2;
