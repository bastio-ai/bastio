ALTER TABLE workspace_conversations ADD COLUMN IF NOT EXISTS is_setup_test BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE workspace_provider_models (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 provider TEXT NOT NULL,
 credential_fingerprint TEXT NOT NULL,
 source TEXT NOT NULL CHECK(source IN ('provider','catalog')),
 models JSONB NOT NULL DEFAULT '[]',
 discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 error_code TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(customer_id,provider)
);
CREATE TABLE workspace_model_checks (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 provider TEXT NOT NULL,
 model TEXT NOT NULL,
 credential_fingerprint TEXT NOT NULL,
 checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 success BOOLEAN NOT NULL,
 error_code TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(customer_id,provider,model)
);
CREATE TABLE workspace_model_test_limits (
 customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL,
 window_started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 attempts INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(customer_id,user_id)
);
CREATE TABLE workspace_setup_review (
 customer_id UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
 privacy_fingerprint TEXT NOT NULL,
 reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
