-- name: GetProviderKey :one
SELECT id, customer_id, proxy_id, provider, encrypted_key, is_default, created_at, updated_at
FROM proxy_provider_keys
WHERE customer_id = $1 AND provider = $2
    AND (proxy_id = $3 OR (proxy_id IS NULL AND is_default = true))
ORDER BY proxy_id NULLS LAST
LIMIT 1;

-- name: ListProviderKeysByCustomer :many
SELECT id, customer_id, proxy_id, provider, encrypted_key, is_default, created_at, updated_at
FROM proxy_provider_keys
WHERE customer_id = $1
ORDER BY provider, created_at DESC;

-- name: CreateProviderKey :one
INSERT INTO proxy_provider_keys (customer_id, proxy_id, provider, encrypted_key, is_default)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, customer_id, proxy_id, provider, encrypted_key, is_default, created_at, updated_at;
