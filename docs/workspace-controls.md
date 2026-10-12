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

Unassigned resources are shared with the workspace. Assigning one or more groups restricts access to those groups' members. Owners and administrators can manage all resources. List/read endpoints, assistant use, keyword retrieval, attached knowledge loading and vector retrieval enforce this policy. Removing an employee clears group memberships.

In Cloud, opt-in SCIM provisioning links active directory users to verified Bastio identities within the seat limit, including pending invitations. It does not create sign-in credentials or implement SAML. SCIM groups become Workspace access groups. Deactivation removes managed employee memberships; owners and administrators remain under manual control. Deleted SCIM groups keep restrictions with no members until an administrator reassigns them. Disabling sync retains existing memberships. Unverified users and users above the seat limit remain unprovisioned; verification or another directory sync can retry linking.

## Account security and evidence

Cloud supports authenticator MFA for password accounts. Enrollment requires the current password and a valid authenticator code. Login then creates a limited, ten-minute challenge session that cannot access protected APIs until a second factor succeeds. TOTP codes cannot be replayed, recovery codes are single-use, and recovery codes are shown once. Enrollment and disabling MFA revoke other browser sessions. Users can view and revoke their sessions.

Evidence exports contain current settings, dated PostgreSQL usage counts, estimated ledger spend and administrative events. Current settings do not prove historical policy. Usage excludes archived messages and gateway events; exports are operational evidence, not compliance certification. Events are limited to 1,000 per export, with explicit truncation metadata; use smaller RFC3339 date ranges to export additional events.

## Verification

Go integration tests use `WORKSPACE_TEST_DATABASE_URL` pointing to a disposable PostgreSQL 18 database with OSS and Cloud migrations. Trace privacy additionally uses `WORKSPACE_TEST_CLICKHOUSE_URL` pointing to isolated migrated ClickHouse. Tests skip these integrations when those variables are absent. No customer data or paid model credentials are required.
