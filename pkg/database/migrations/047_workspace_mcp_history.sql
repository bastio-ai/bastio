-- Payloads stay outside general traces/observations and their broad read APIs.
CREATE TABLE workspace_mcp_history_policy (
 customer_id UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
 mode TEXT NOT NULL DEFAULT 'metadata_only' CHECK(mode IN ('metadata_only','sanitized')),
 retention_days INTEGER NOT NULL DEFAULT 7 CHECK(retention_days BETWEEN 1 AND 90),
 capture_all BOOLEAN NOT NULL DEFAULT FALSE,
 connection_ids UUID[] NOT NULL DEFAULT '{}',
 viewer_user_ids TEXT[] NOT NULL DEFAULT '{}',
 viewer_group_ids UUID[] NOT NULL DEFAULT '{}',
 version BIGINT NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE workspace_mcp_history (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 trace_id UUID NOT NULL,
 connection_id UUID NOT NULL REFERENCES workspace_mcp_connections(id) ON DELETE CASCADE,
 conversation_id UUID REFERENCES workspace_conversations(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 encrypted_content JSONB NOT NULL,
 PRIMARY KEY(customer_id,trace_id)
);
CREATE INDEX workspace_mcp_history_expiry ON workspace_mcp_history(expires_at);
-- No-storage takes immediate precedence, including in-flight captures.
CREATE FUNCTION workspace_mcp_history_privacy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.store_conversation_content THEN
  DELETE FROM workspace_mcp_history WHERE customer_id=NEW.customer_id;
 ELSE
  UPDATE workspace_mcp_history SET expires_at=LEAST(expires_at,created_at+make_interval(days=>NEW.retention_days)) WHERE customer_id=NEW.customer_id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_mcp_history_privacy AFTER UPDATE OF store_conversation_content,retention_days ON workspace_settings FOR EACH ROW EXECUTE FUNCTION workspace_mcp_history_privacy();
CREATE FUNCTION workspace_mcp_history_revoke() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.revoked_at IS NOT NULL THEN DELETE FROM workspace_mcp_history WHERE customer_id=NEW.customer_id AND connection_id=NEW.id; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_mcp_history_revoke AFTER UPDATE OF revoked_at ON workspace_mcp_connections FOR EACH ROW EXECUTE FUNCTION workspace_mcp_history_revoke();
ALTER TABLE workspace_settings ADD COLUMN mcp_history_epoch BIGINT NOT NULL DEFAULT 0;
CREATE FUNCTION workspace_mcp_history_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.store_conversation_content IS DISTINCT FROM OLD.store_conversation_content OR NEW.retention_days IS DISTINCT FROM OLD.retention_days THEN
  NEW.mcp_history_epoch=OLD.mcp_history_epoch+1;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_mcp_history_epoch BEFORE UPDATE ON workspace_settings FOR EACH ROW EXECUTE FUNCTION workspace_mcp_history_epoch();
CREATE FUNCTION workspace_mcp_history_private_chat() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.content_stored THEN DELETE FROM workspace_mcp_history WHERE customer_id=NEW.customer_id AND conversation_id=NEW.id; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_mcp_history_private_chat AFTER UPDATE OF content_stored ON workspace_conversations FOR EACH ROW EXECUTE FUNCTION workspace_mcp_history_private_chat();
CREATE FUNCTION workspace_mcp_history_remove_group() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE workspace_mcp_history_policy SET viewer_group_ids=array_remove(viewer_group_ids,OLD.id),version=version+1 WHERE customer_id=OLD.customer_id AND OLD.id=ANY(viewer_group_ids);
 RETURN OLD;
END $$;
CREATE TRIGGER workspace_mcp_history_remove_group AFTER DELETE ON workspace_access_groups FOR EACH ROW EXECUTE FUNCTION workspace_mcp_history_remove_group();
CREATE OR REPLACE FUNCTION clear_workspace_direct_shares() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM workspace_resource_users WHERE customer_id=OLD.customer_id AND user_id=OLD.user_id;
 UPDATE workspace_mcp_history_policy SET viewer_user_ids=array_remove(viewer_user_ids,OLD.user_id),version=version+1 WHERE customer_id=OLD.customer_id AND OLD.user_id=ANY(viewer_user_ids);
 RETURN OLD;
END $$;
DO $$ BEGIN
 IF to_regclass('workspace_members') IS NOT NULL THEN
  DROP TRIGGER IF EXISTS workspace_member_direct_share_cleanup ON workspace_members;
  CREATE TRIGGER workspace_member_direct_share_cleanup AFTER DELETE ON workspace_members FOR EACH ROW EXECUTE FUNCTION clear_workspace_direct_shares();
 END IF;
END $$;
