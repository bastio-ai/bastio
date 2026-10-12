-- Fail safely on rollback: preserve selected-person and empty restrictions as
-- legacy groups rather than silently sharing restricted knowledge/assistants.
DO $$ BEGIN
 IF to_regclass('workspace_members') IS NOT NULL THEN
  DROP TRIGGER IF EXISTS workspace_member_direct_share_cleanup ON workspace_members;
 END IF;
END $$;
DROP FUNCTION IF EXISTS clear_workspace_direct_shares();
INSERT INTO workspace_access_groups(id,customer_id,name)
SELECT resource_id,customer_id,'Rollback ACL ' || resource_type || ' ' || resource_id::text
FROM workspace_resource_sharing WHERE restricted AND resource_type IN ('knowledge','assistant')
ON CONFLICT DO NOTHING;
INSERT INTO workspace_access_group_members(customer_id,group_id,user_id)
SELECT u.customer_id,u.resource_id,u.user_id FROM workspace_resource_users u
JOIN workspace_resource_sharing p ON p.customer_id=u.customer_id AND p.resource_type=u.resource_type AND p.resource_id=u.resource_id
WHERE p.restricted AND p.resource_type IN ('knowledge','assistant') ON CONFLICT DO NOTHING;
INSERT INTO workspace_resource_access(customer_id,resource_type,resource_id,group_id)
SELECT customer_id,resource_type,resource_id,resource_id FROM workspace_resource_sharing
WHERE restricted AND resource_type IN ('knowledge','assistant') ON CONFLICT DO NOTHING;
-- Old binaries cannot enforce MCP recipient ACLs. Revoke those connections.
UPDATE workspace_mcp_connections c SET revoked_at=COALESCE(c.revoked_at,now())
FROM workspace_resource_sharing p WHERE c.customer_id=p.customer_id AND c.id=p.resource_id AND p.resource_type='mcp' AND p.restricted;
DELETE FROM workspace_resource_access WHERE resource_type='mcp';
ALTER TABLE workspace_resource_access DROP CONSTRAINT IF EXISTS workspace_resource_access_resource_type_check;
ALTER TABLE workspace_resource_access ADD CONSTRAINT workspace_resource_access_resource_type_check CHECK (resource_type IN ('knowledge','assistant'));
DROP TABLE IF EXISTS workspace_resource_users;
DROP TABLE IF EXISTS workspace_resource_sharing;
