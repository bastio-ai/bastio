-- Backfill a 'default' security_profiles row for every customer that
-- doesn't have one yet. Required because before this point only the
-- DefaultCustomerID got a seeded row (via migrations/001) — every
-- additional tenant created later landed without one, which left
-- their Security Center page blank in the dashboard (ListProfiles
-- filters on customer_id and there was nothing to return).
--
-- New tenants now get the row inserted at signup/customer-creation
-- time, so this migration only handles the historical gap.
-- Idempotent via the unique constraint on (customer_id, proxy_id, name).
--
-- Schema column defaults supply every non-name field so this stays in
-- lockstep with whatever migration the schema is at — adding a new
-- detector column doesn't require touching this file.

INSERT INTO security_profiles (customer_id, name)
SELECT id, 'default'
FROM customers
ON CONFLICT (customer_id, proxy_id, name) DO NOTHING;
