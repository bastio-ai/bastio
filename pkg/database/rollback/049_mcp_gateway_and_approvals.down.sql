DROP TABLE IF EXISTS mcp_approvals;
ALTER TABLE mcp_events DROP CONSTRAINT IF EXISTS mcp_events_action_check;
ALTER TABLE mcp_events ADD CONSTRAINT mcp_events_action_check
 CHECK(action IN ('allowed','blocked','sanitized','warned'));
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS encrypted_credential;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS auth_type;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS upstream_url;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS mode;
