-- name: ListPrompts :many
-- Returns one row per prompt with its latest version metadata so the
-- Prompts list page can render without N+1 queries.
SELECT
    p.id, p.customer_id, p.name, p.description, p.created_at, p.updated_at,
    v.version AS latest_version,
    v.content_type AS latest_content_type,
    v.labels AS latest_labels,
    v.created_at AS latest_created_at
FROM prompts p
LEFT JOIN LATERAL (
    SELECT version, content_type, labels, created_at
    FROM prompt_versions
    WHERE prompt_id = p.id
    ORDER BY version DESC
    LIMIT 1
) v ON TRUE
WHERE p.customer_id = $1
ORDER BY p.updated_at DESC;

-- name: GetPromptByName :one
SELECT id, customer_id, name, description, created_at, updated_at
FROM prompts
WHERE customer_id = $1 AND name = $2;

-- name: CreatePrompt :one
INSERT INTO prompts (customer_id, name, description)
VALUES ($1, $2, $3)
RETURNING id, customer_id, name, description, created_at, updated_at;

-- name: TouchPrompt :exec
UPDATE prompts SET updated_at = NOW() WHERE id = $1 AND customer_id = $2;

-- name: DeletePrompt :exec
-- Deletes only when no versions reference the prompt. Cascaded cleanup
-- is handled explicitly in the handler so UI can warn the user first.
DELETE FROM prompts p
WHERE p.id = $1 AND p.customer_id = $2
  AND NOT EXISTS (SELECT 1 FROM prompt_versions pv WHERE pv.prompt_id = p.id);

-- name: ListPromptVersions :many
SELECT id, prompt_id, customer_id, version, content, content_type,
       config, labels, commit_message, created_by, created_at
FROM prompt_versions
WHERE customer_id = $1 AND prompt_id = $2
ORDER BY version DESC;

-- name: GetPromptVersion :one
SELECT id, prompt_id, customer_id, version, content, content_type,
       config, labels, commit_message, created_by, created_at
FROM prompt_versions
WHERE customer_id = $1 AND prompt_id = $2 AND version = $3;

-- name: GetLatestPromptVersion :one
SELECT id, prompt_id, customer_id, version, content, content_type,
       config, labels, commit_message, created_by, created_at
FROM prompt_versions
WHERE customer_id = $1 AND prompt_id = $2
ORDER BY version DESC
LIMIT 1;

-- name: GetPromptVersionByLabel :one
-- Used by the SDK fetch endpoint when a caller asks for prompts/:name?label=production.
-- Returns the newest version carrying the label (labels move with releases).
SELECT id, prompt_id, customer_id, version, content, content_type,
       config, labels, commit_message, created_by, created_at
FROM prompt_versions
WHERE customer_id = $1 AND prompt_id = $2 AND $3 = ANY(labels)
ORDER BY version DESC
LIMIT 1;

-- name: NextPromptVersion :one
-- Atomic next version number. Used inside the CreatePromptVersion txn.
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM prompt_versions
WHERE prompt_id = $1;

-- name: CreatePromptVersion :one
INSERT INTO prompt_versions (
    prompt_id, customer_id, version, content, content_type,
    config, labels, commit_message, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, prompt_id, customer_id, version, content, content_type,
          config, labels, commit_message, created_by, created_at;

-- name: SetVersionLabels :one
-- Replaces the labels array on a specific version. "production" and similar
-- deploy labels typically belong on exactly one version at a time; the
-- handler enforces that semantic when writing.
UPDATE prompt_versions
SET labels = $4
WHERE customer_id = $1 AND prompt_id = $2 AND version = $3
RETURNING id, prompt_id, customer_id, version, content, content_type,
          config, labels, commit_message, created_by, created_at;

-- name: ClearLabelOnOtherVersions :exec
-- Removes a given label from every version of a prompt except the one
-- being promoted. Keeps deploy labels unique per prompt.
UPDATE prompt_versions
SET labels = array_remove(labels, $3::text)
WHERE customer_id = $1 AND prompt_id = $2 AND version != $4
  AND $3 = ANY(labels);
