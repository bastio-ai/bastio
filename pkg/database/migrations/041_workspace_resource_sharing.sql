-- Explicit restriction survives removal of the last recipient. Existing group
-- assignments remain restricted; unassigned resources keep their current access.
ALTER TABLE workspace_resource_access DROP CONSTRAINT workspace_resource_access_resource_type_check;
ALTER TABLE workspace_resource_access ADD CONSTRAINT workspace_resource_access_resource_type_check CHECK (resource_type IN ('knowledge','assistant','mcp'));

CREATE TABLE workspace_resource_sharing (
 customer_id UUID NOT NULL,
 resource_type TEXT NOT NULL CHECK (resource_type IN ('knowledge','assistant','mcp')),
 resource_id UUID NOT NULL,
 restricted BOOLEAN NOT NULL DEFAULT FALSE,
 PRIMARY KEY(customer_id,resource_type,resource_id)
);
INSERT INTO workspace_resource_sharing(customer_id,resource_type,resource_id,restricted)
SELECT DISTINCT customer_id,resource_type,resource_id,TRUE FROM workspace_resource_access;

CREATE TABLE workspace_resource_users (
 customer_id UUID NOT NULL,
 resource_type TEXT NOT NULL CHECK (resource_type IN ('knowledge','assistant','mcp')),
 resource_id UUID NOT NULL,
 user_id TEXT NOT NULL,
 PRIMARY KEY(customer_id,resource_type,resource_id,user_id)
);
CREATE INDEX workspace_resource_users_member_idx ON workspace_resource_users(customer_id,user_id);

-- Existing Cloud installs already have workspace_members when OSS migrations
-- run; fresh installs add that table in the later Cloud migration. Creating
-- this trigger conditionally also restores cleanup after a rollback/reapply.
DO $$ BEGIN
 IF to_regclass('workspace_members') IS NOT NULL THEN
  CREATE OR REPLACE FUNCTION clear_workspace_direct_shares() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
  BEGIN
   DELETE FROM workspace_resource_users WHERE customer_id=OLD.customer_id AND user_id=OLD.user_id;
   RETURN OLD;
  END $fn$;
  DROP TRIGGER IF EXISTS workspace_member_direct_share_cleanup ON workspace_members;
  CREATE TRIGGER workspace_member_direct_share_cleanup AFTER DELETE ON workspace_members
  FOR EACH ROW EXECUTE FUNCTION clear_workspace_direct_shares();
 END IF;
END $$;

