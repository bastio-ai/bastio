-- Remote HTTP/SSE MCP Gateway credential vaulting and Human-in-the-Loop approvals.
-- Telemetry and approval rows never store raw tool arguments, results, or plaintext secrets.
ALTER TABLE mcp_servers
 ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'stdio' CHECK(mode IN ('stdio','remote')),
 ADD COLUMN IF NOT EXISTS upstream_url TEXT NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS auth_type TEXT NOT NULL DEFAULT 'none' CHECK(auth_type IN ('none','bearer')),
 ADD COLUMN IF NOT EXISTS encrypted_credential JSONB;

ALTER TABLE mcp_events DROP CONSTRAINT IF EXISTS mcp_events_action_check;
ALTER TABLE mcp_events ADD CONSTRAINT mcp_events_action_check
 CHECK(action IN ('allowed','blocked','sanitized','warned','approval_required','approved','denied'));

CREATE TABLE mcp_approvals (
 id UUID PRIMARY KEY DEFAULT uuidv7(),
 customer_id UUID NOT NULL,
 server_id UUID NOT NULL,
 tool_name TEXT NOT NULL,
 argument_keys JSONB NOT NULL DEFAULT '[]',
 categories JSONB NOT NULL DEFAULT '[]',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','denied','expired')),
 decided_by TEXT NOT NULL DEFAULT '',
 decision_reason TEXT NOT NULL DEFAULT '',
 requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '5 minutes'),
 decided_at TIMESTAMPTZ,
 FOREIGN KEY(customer_id,server_id) REFERENCES mcp_servers(customer_id,id) ON DELETE CASCADE
);
CREATE INDEX mcp_approvals_customer_status_idx ON mcp_approvals(customer_id,status,requested_at DESC);
CREATE INDEX mcp_approvals_server_status_idx ON mcp_approvals(customer_id,server_id,status,requested_at DESC);
