ALTER TABLE workspace_mcp_connections ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'none' CHECK(auth_type IN ('none','bearer','oauth'));
ALTER TABLE workspace_mcp_connections ADD COLUMN encrypted_oauth JSONB;
ALTER TABLE workspace_mcp_connections ADD COLUMN oauth_connected BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE workspace_mcp_connections SET auth_type='bearer' WHERE encrypted_credential IS NOT NULL;
CREATE TABLE workspace_mcp_oauth_flows (
 state_hash TEXT PRIMARY KEY,
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL,
 connection_id UUID NOT NULL REFERENCES workspace_mcp_connections(id) ON DELETE CASCADE,
 encrypted_flow JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workspace_mcp_oauth_flows_expiry ON workspace_mcp_oauth_flows(expires_at);
