-- Dashboard tests use short-lived, gateway-scoped credentials internally.
ALTER TABLE gateway_api_keys ADD COLUMN IF NOT EXISTS is_ephemeral BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS gateway_api_keys_ephemeral_expiry_idx ON gateway_api_keys (customer_id, expires_at) WHERE is_ephemeral = TRUE;
