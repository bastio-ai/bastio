ALTER TABLE workspace_assistants ADD COLUMN mcp_connection_ids UUID[] NOT NULL DEFAULT '{}';
CREATE TABLE workspace_tool_approvals (
 id UUID PRIMARY KEY,
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL,
 conversation_id UUID NOT NULL REFERENCES workspace_conversations(id) ON DELETE CASCADE,
 message_id UUID REFERENCES workspace_messages(id) ON DELETE CASCADE,
 status TEXT NOT NULL CHECK(status IN ('pending','running','completed','declined','failed')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX workspace_tool_approvals_expiry ON workspace_tool_approvals(expires_at);
-- Only operational state lives here. Protected arguments and context are sealed
-- into a tenant-bound token held by the client (and saved only under chat policy).
