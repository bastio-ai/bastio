DROP TABLE IF EXISTS workspace_mcp_oauth_flows;
ALTER TABLE workspace_mcp_connections DROP COLUMN IF EXISTS auth_type;
ALTER TABLE workspace_mcp_connections DROP COLUMN IF EXISTS encrypted_oauth;
ALTER TABLE workspace_mcp_connections DROP COLUMN IF EXISTS oauth_connected;
