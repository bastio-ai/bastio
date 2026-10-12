---
title: Workspace controls
description: Conversation storage, spending, group access, account security and operational evidence.
---

Managers configure privacy, approved models, spending and groups in **Workspace → AI controls**. Account MFA and browser sessions are under **Profile & Security**. **Compliance & Audit** exports real Workspace operational records.

## Conversation storage

“Do not store conversation content” applies to new writes. Bastio suppresses message bodies, conversation titles, attachment content, message metadata containing citations, provider error text and security snippets in PostgreSQL and ClickHouse. Temporary chats bypass the optional Cloud response cache, including when saved-history mode is re-enabled later. The cache also checks the current workspace policy before reading or writing a response. Operational identifiers, model names, token counts, estimated costs and security decisions remain. Existing saved content and archives are not erased; previously cached responses follow their existing expiry policy. Knowledge documents are separate stored resources.

Temporary conversations use browser memory while the chat view remains open. Leaving chat, refreshing, signing out or closing the tab loses their text. Browser history sent to the API accepts at most 100 user/assistant turns and a total context of 32 MiB; replayed text passes the security engine. System/tool roles cannot be supplied through this history. The client retains the most recent 100 turns for continued conversation. A temporary conversation remains temporary after a manager re-enables storage.

Bastio does not collect conversation copies for training through this feature. Provider retention is governed separately by the connected provider's settings and terms. Images still require the workspace image policy; text-only inspection does not inspect image pixels.

## Spending

The monthly limit is in USD, across employees, and resets at the start of each UTC calendar month. Zero disables it. The UI warns at 80%. PostgreSQL serializes spending reservations before provider calls, so concurrent requests cannot independently consume the same allowance. Each request reserves conservatively for input and up to 8,192 output tokens; successful responses settle to reported usage and Bastio's model-price estimates. Missing usage retains a conservative estimate. Ollama has no upstream model charge in this ledger; hosting expenses are excluded.

A capped workspace rejects models without a known price. Provider failures keep their allowance reserved because a failed connection may still have incurred charges. Operators can inspect `workspace_spend` by tenant and reconcile uncertain reservations against provider billing; there is no automatic expiry that could silently release incurred spend. This is an estimated-cost control, not a guarantee that provider invoices match Bastio's price table. Deleting or archiving conversation messages does not refund spend.

## Knowledge and assistant access

### Upload processing and personal data

The original uploaded file is retained in private server storage. Bastio extracts and scans its text with the workspace security profile before indexing. Detected PII, secrets and document instructions follow the selected actions: Block quarantines files; Mask or Tokenize processes the searchable text; Warn and Log only allow unchanged text. Tokenized knowledge keeps placeholders without a restoration mapping. Masking can preserve an email domain or a card’s last four digits, so it is not a guarantee of anonymity. Only extracted text is inspected; image pixels are not scanned.

Bastio Cloud uses the self-hosted BGE-M3 model over the private WireGuard network with verified TLS. Both document chunks and search queries pass through security processing before embedding. Existing ready sources are gradually re-indexed against the current security profile; blocked sources are quarantined. Vector storage records the model revision, so retrieval does not mix model versions or dimensions. Embedding failures retain keyword retrieval and are retried during background indexing. Managers can see remaining indexing work under Knowledge.

Processed text and search chunks are stored in PostgreSQL. When an embedding client is configured, processed chunks are sent to that service to generate vectors. Without one, retrieval uses local keyword matching. During chat, selected authorized passages are checked again and included in the request to the chosen model provider. Uploading knowledge does not send the whole document to a conversational model for summarization.

The conversation storage setting does not apply to knowledge files. Masking searchable text does not erase the original uploaded file, and archiving a source does not permanently delete it. Existing indexed sources are not automatically rewritten when a profile changes. Cloud ingestion and retrieval honor the Workspace fail-closed setting: missing scanner dependencies or profile lookup failures prevent indexing and withhold unverified passages.

Admins choose who can use each knowledge source or assistant: everyone in the workspace, selected groups, selected people, or groups plus selected people. The selections are additive. A restricted resource with no recipients is available only to owners and admins; removing the last recipient never opens it to the whole workspace. New file uploads and pasted sources default to owners/admins only in the dashboard, and their chosen recipients are saved in the same transaction as the source. API clients that omit sharing on creation retain the previous everyone-in-workspace default. Existing resources retain their previous sharing until changed. Admins can adjust sharing from each resource or under **Workspace → AI controls → Teams and sharing**. Owners and admins can manage restricted resources. Employee list/read endpoints, assistant selection and continued use, keyword retrieval, attached knowledge loading and vector retrieval enforce the same policy. Sharing an assistant does not grant access to attached knowledge: both permissions apply. Removing an employee clears group membership and individual grants, including when SCIM deprovisions them. Existing saved conversations are retained under the conversation storage policy; changes to sharing prevent future use and retrieval rather than erasing prior messages.

In Cloud, opt-in SCIM provisioning links active directory users to verified Bastio identities within the seat limit, including pending invitations. It does not create sign-in credentials or implement SAML. SCIM groups become Workspace access groups. Deactivation removes managed employee memberships; owners and administrators remain under manual control. Deleted SCIM groups keep restrictions with no members until an administrator reassigns them. Disabling sync retains existing memberships. Unverified users and users above the seat limit remain unprovisioned; verification or another directory sync can retry linking.

## Workspace MCP integrations

Members open **Integrations** beside the chat model picker to add a personal remote MCP connection, discover tools, review their arguments and explicitly run a tool. Protected results can be inserted into the chat draft; they are not sent to the selected model until the member sends that draft. Tools are not executed autonomously by the chat model.

Admins use **Workspace → Integrations** to create a connection shared with everyone, selected groups, selected people, or both groups and people. Admins can also set a tool allowlist and enable or disable personal connections. Discovery and every tool call check the current sharing policy. Personal connections are visible and usable only by their owner; administrators cannot use another member's personal credential. Shared credentials authorize the permitted tools for the selected recipients, but credentials are encrypted per tenant and connection and are never returned to clients. A restricted connection with no recipients is admin-only.

Supported endpoints use public HTTPS Streamable HTTP, with bearer-token or no authentication. Current per-request MCP and initialization/session-based 2025 protocols accept JSON or request-scoped SSE responses. Interactive OAuth, local stdio processes, server sampling/elicitation and binary/image results are not exposed by this Workspace connector. Existing standalone stdio firewall functionality remains separate.

Arguments are inspected before forwarding, including nested strings, escaped characters and numeric identifiers. Tool names are filtered against the connection allowlist, destructive command patterns are rejected, and returned descriptions, schemas, text and structured results are scanned as untrusted content. Block withholds requests or results; Mask/Tokenize rewrite detected values; Warn/Log only allow the originals. Token placeholders have no restoration mapping in this flow. Result blocking cannot undo an action already performed by the remote tool. Calls are not automatically retried after uncertain failures.

Integration audit records retain operation identifiers, tool names and security categories, without raw arguments, results or credentials. Inserting a result into a saved chat follows the conversation storage policy; connected services retain their own data according to their configuration. Private/loopback/link-local destinations, DNS rebinding and redirects are denied for member-supplied endpoints.

## Account security and evidence

Cloud supports authenticator MFA for password accounts. Enrollment requires the current password and a valid authenticator code. Login then creates a limited, ten-minute challenge session that cannot access protected APIs until a second factor succeeds. TOTP codes cannot be replayed, recovery codes are single-use, and recovery codes are shown once. Enrollment and disabling MFA revoke other browser sessions. Users can view and revoke their sessions.

Evidence exports contain current settings, dated PostgreSQL usage counts, estimated ledger spend and administrative events. Current settings do not prove historical policy. Usage excludes archived messages and gateway events; exports are operational evidence, not compliance certification. Events are limited to 1,000 per export, with explicit truncation metadata; use smaller RFC3339 date ranges to export additional events.

## Verification

Go integration tests use `WORKSPACE_TEST_DATABASE_URL` pointing to a disposable PostgreSQL 18 database with OSS and Cloud migrations. Trace privacy additionally uses `WORKSPACE_TEST_CLICKHOUSE_URL` pointing to isolated migrated ClickHouse. Tests skip these integrations when those variables are absent. No customer data or paid model credentials are required.
