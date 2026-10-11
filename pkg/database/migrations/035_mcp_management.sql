-- Managed local MCP firewalls. Enrollment tokens are scoped to one server and
-- stored only as hashes; telemetry never stores tool arguments or results.
CREATE TABLE mcp_servers (
 id UUID PRIMARY KEY DEFAULT uuidv7(),
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
 name TEXT NOT NULL,
 client TEXT NOT NULL,
 policy JSONB NOT NULL DEFAULT '{"allowed_tools":[],"blocked_tools":[]}',
 token_hash TEXT NOT NULL UNIQUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_seen_at TIMESTAMPTZ,
 revoked_at TIMESTAMPTZ,
 dropped_events BIGINT NOT NULL DEFAULT 0,
 ingest_window_started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 ingest_window_requests INTEGER NOT NULL DEFAULT 0,
 UNIQUE(customer_id,id)
);
CREATE INDEX mcp_servers_customer_idx ON mcp_servers(customer_id,created_at);
CREATE TABLE mcp_events (
 id UUID PRIMARY KEY,
 customer_id UUID NOT NULL,
 server_id UUID NOT NULL,
 tool_name TEXT NOT NULL,
 direction TEXT NOT NULL CHECK(direction IN ('request','response','tool_description')),
 action TEXT NOT NULL CHECK(action IN ('allowed','blocked','sanitized','warned')),
 categories JSONB NOT NULL,
 duration_ms DOUBLE PRECISION NOT NULL,
 occurred_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(customer_id,server_id) REFERENCES mcp_servers(customer_id,id) ON DELETE RESTRICT
);
CREATE INDEX mcp_events_customer_time_idx ON mcp_events(customer_id,created_at DESC);
CREATE INDEX mcp_events_server_time_idx ON mcp_events(customer_id,server_id,created_at DESC);
