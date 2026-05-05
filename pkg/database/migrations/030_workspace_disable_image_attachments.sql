-- Migration 030 — workspace admin toggle to disable image attachments.
--
-- Workspace chat lets users attach images to messages (uploaded to
-- /chat-attachments, persisted as inline base64 in the message body,
-- passed through to the model as multimodal content). Images
-- bypass the security engine — we strip the base64 before scanning
-- the surrounding text and recompose afterwards, so the engine
-- never sees the pixel content. The model provider's safety layer
-- is the only thing inspecting image bytes.
--
-- For high-sensitivity workspaces (regulated industries, paranoid
-- security teams, customer-data-heavy use cases), that's not
-- acceptable. This column gives admins a binary kill switch:
-- TRUE = the chat surface hides the attach button and the
-- /chat-attachments endpoint rejects multipart uploads.
--
-- Default FALSE preserves current behaviour for every existing
-- workspace.

ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS disable_image_attachments BOOLEAN NOT NULL DEFAULT FALSE;
