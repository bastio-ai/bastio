-- name: GetCustomer :one
SELECT id, name, slug, settings, created_at, updated_at
FROM customers
WHERE id = $1;

-- name: GetCustomerBySlug :one
SELECT id, name, slug, settings, created_at, updated_at
FROM customers
WHERE slug = $1;

-- name: ListCustomers :many
SELECT id, name, slug, settings, created_at, updated_at
FROM customers
ORDER BY created_at DESC;

-- name: CreateCustomer :one
INSERT INTO customers (name, slug, settings)
VALUES ($1, $2, $3)
RETURNING id, name, slug, settings, created_at, updated_at;
