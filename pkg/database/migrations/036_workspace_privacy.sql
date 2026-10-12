-- Storage mode affects new writes; existing content is not silently deleted.
ALTER TABLE workspace_settings ADD COLUMN store_conversation_content BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE workspace_conversations ADD COLUMN content_stored BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE workspace_messages ADD COLUMN cost_cents_exact DOUBLE PRECISION NOT NULL DEFAULT 0;
UPDATE workspace_messages SET cost_cents_exact = cost_cents;
CREATE INDEX workspace_messages_search ON workspace_messages USING GIN (to_tsvector('simple', content));
