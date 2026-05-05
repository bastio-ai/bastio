-- name: GetSecurityProfile :one
SELECT id, customer_id, proxy_id, name,
    injection_enabled, injection_threshold,
    pii_enabled, pii_action,
    pii_scan_response, pii_restore_response, pii_token_style,
    jailbreak_enabled, jailbreak_threshold,
    bot_detection_enabled, custom_patterns_enabled,
    created_at, updated_at
FROM security_profiles
WHERE id = $1 AND customer_id = $2;

-- name: GetDefaultSecurityProfile :one
SELECT id, customer_id, proxy_id, name,
    injection_enabled, injection_threshold,
    pii_enabled, pii_action,
    pii_scan_response, pii_restore_response, pii_token_style,
    jailbreak_enabled, jailbreak_threshold,
    bot_detection_enabled, custom_patterns_enabled,
    created_at, updated_at
FROM security_profiles
WHERE customer_id = $1 AND name = 'default'
LIMIT 1;

-- name: ListSecurityProfilesByCustomer :many
SELECT id, customer_id, proxy_id, name,
    injection_enabled, injection_threshold,
    pii_enabled, pii_action,
    pii_scan_response, pii_restore_response, pii_token_style,
    jailbreak_enabled, jailbreak_threshold,
    bot_detection_enabled, custom_patterns_enabled,
    created_at, updated_at
FROM security_profiles
WHERE customer_id = $1
ORDER BY created_at DESC;

-- name: CreateSecurityProfile :one
INSERT INTO security_profiles (customer_id, proxy_id, name,
    injection_enabled, injection_threshold,
    pii_enabled, pii_action,
    pii_scan_response, pii_restore_response, pii_token_style,
    jailbreak_enabled, jailbreak_threshold,
    bot_detection_enabled, custom_patterns_enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, customer_id, proxy_id, name,
    injection_enabled, injection_threshold,
    pii_enabled, pii_action,
    pii_scan_response, pii_restore_response, pii_token_style,
    jailbreak_enabled, jailbreak_threshold,
    bot_detection_enabled, custom_patterns_enabled,
    created_at, updated_at;
