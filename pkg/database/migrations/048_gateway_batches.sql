-- Operational state only: encrypted resource metadata, reversible token vaults
-- and quarantined upload chunks. Analytics and prompts are never stored here.
CREATE TABLE gateway_batch_state (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
 scope TEXT NOT NULL,
 kind TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 encrypted_payload JSONB NOT NULL,
 received_bytes BIGINT NOT NULL DEFAULT 0,
	reserved_bytes BIGINT NOT NULL DEFAULT 0,
 completed BOOLEAN NOT NULL DEFAULT FALSE,
 lease_until TIMESTAMPTZ,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(customer_id,scope,kind,resource_id)
);
CREATE INDEX gateway_batch_state_expiry_idx ON gateway_batch_state(expires_at,customer_id);
CREATE TABLE gateway_batch_chunks (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
 scope TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 chunk_offset BIGINT NOT NULL,
 encrypted_payload JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(customer_id,scope,resource_id,chunk_offset)
);
CREATE INDEX gateway_batch_chunks_expiry_idx ON gateway_batch_chunks(expires_at,customer_id);
