-- 027_backfill_workspace_member_owners.sql
--
-- Real RBAC enforcement is landing alongside this migration. Every
-- workspace endpoint will check workspace_members.role for the
-- caller. That requires every customer-creating user to have a
-- workspace_members row with role='owner'.
--
-- Earlier signup paths inserted the customer + auth_user but NOT a
-- workspace_members row. So founders were role-less and after RBAC
-- lands they would be locked out of their own workspace.
--
-- This migration backfills owner rows for every customer that has
-- an auth_user_id but no matching workspace_members row. Idempotent
-- via the (customer_id, user_id) primary key.
--
-- Going forward, signup paths insert this row inline, in the same
-- transaction as the customer insert — no race window.
--
-- Note: workspace_members.user_id is TEXT (Better Auth subject is
-- a string). customers.auth_user_id is UUID. Cast to text for the
-- join. Email is fetched from auth_users so the team page shows
-- the right address.

INSERT INTO workspace_members (customer_id, user_id, email, role, joined_at)
SELECT
    c.id,
    c.auth_user_id::text,
    LOWER(u.email),
    'owner',
    c.created_at
FROM customers c
JOIN auth_users u ON u.id = c.auth_user_id
WHERE c.auth_user_id IS NOT NULL
ON CONFLICT (customer_id, user_id) DO NOTHING;
