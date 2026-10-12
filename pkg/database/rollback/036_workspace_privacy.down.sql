DROP INDEX IF EXISTS workspace_messages_search;
ALTER TABLE workspace_messages DROP COLUMN IF EXISTS cost_cents_exact;
ALTER TABLE workspace_conversations DROP COLUMN IF EXISTS content_stored;
ALTER TABLE workspace_settings DROP COLUMN IF EXISTS store_conversation_content;
