-- Bind gateway credentials to a managed deployment environment.
-- The gateway derives telemetry environment from the authenticated key;
-- client-supplied overrides are opt-in for shared ingress credentials.

ALTER TABLE gateway_api_keys
    ADD COLUMN environment_id UUID REFERENCES environments(id) ON DELETE RESTRICT,
    ADD COLUMN allow_environment_override BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO environments (customer_id, name, kind, description)
SELECT c.id, 'production', 'production', 'Default production boundary'
FROM customers c
WHERE NOT EXISTS (
    SELECT 1 FROM environments e
    WHERE e.customer_id = c.id AND lower(e.name) = 'production'
);

UPDATE gateway_api_keys k
SET environment_id = e.id
FROM environments e
WHERE k.environment_id IS NULL
  AND e.customer_id = k.customer_id
  AND lower(e.name) = 'production';

ALTER TABLE gateway_api_keys
    ALTER COLUMN environment_id SET NOT NULL;

CREATE INDEX gateway_api_keys_environment_idx
    ON gateway_api_keys (customer_id, environment_id)
    WHERE is_active = TRUE;
