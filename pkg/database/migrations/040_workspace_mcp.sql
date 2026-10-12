CREATE TABLE workspace_mcp_policy (
 customer_id UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
 allow_personal BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE TABLE workspace_mcp_connections (
 id UUID PRIMARY KEY,
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 owner_user_id TEXT NOT NULL,
 name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
 endpoint TEXT NOT NULL,
 scope TEXT NOT NULL CHECK (scope IN ('personal','workspace')),
 allowed_tools TEXT[] NOT NULL DEFAULT '{}',
 encrypted_credential JSONB,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 revoked_at TIMESTAMPTZ
);
CREATE INDEX workspace_mcp_connections_scope_idx ON workspace_mcp_connections(customer_id,scope,owner_user_id) WHERE revoked_at IS NULL;
