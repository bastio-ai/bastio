-- name: GetProxy :one
SELECT id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active, created_at, updated_at
FROM proxies
WHERE id = $1 AND customer_id = $2;

-- name: ListProxiesByCustomer :many
SELECT id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active, created_at, updated_at
FROM proxies
WHERE customer_id = $1
ORDER BY created_at DESC;

-- name: CreateProxy :one
INSERT INTO proxies (customer_id, name, slug, listen_path, target_provider, target_model, settings)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active, created_at, updated_at;

-- name: UpdateProxy :exec
UPDATE proxies
SET name = $3, target_provider = $4, target_model = $5, settings = $6, is_active = $7
WHERE id = $1 AND customer_id = $2;

-- name: DeleteProxy :exec
DELETE FROM proxies
WHERE id = $1 AND customer_id = $2;
