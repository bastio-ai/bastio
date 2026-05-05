-- Bastio v2 Initial Schema
-- Core tables for gateway, security, proxy management

-- PostgreSQL 18: uuidv7() is built-in, no extension needed

-- Customers (tenants)
CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Proxies (gateway endpoints that route to LLM providers)
CREATE TABLE proxies (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    listen_path TEXT NOT NULL,
    target_provider TEXT NOT NULL, -- openai, anthropic, bedrock, vertex, azure, ollama
    target_model TEXT NOT NULL DEFAULT '',
    settings JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(customer_id, slug)
);

-- Gateway API keys (authenticate requests to the gateway)
CREATE TABLE gateway_api_keys (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    key_hash TEXT NOT NULL UNIQUE, -- SHA-256 hash of the API key
    key_prefix TEXT NOT NULL, -- First 8 chars for identification (e.g., "sk-bast-")
    scopes TEXT[] NOT NULL DEFAULT '{}',
    rate_limit_rpm INTEGER, -- requests per minute, NULL = no limit
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_hash ON gateway_api_keys(key_hash) WHERE is_active = true;

-- Provider keys (credentials for LLM providers, encrypted)
CREATE TABLE proxy_provider_keys (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    proxy_id UUID REFERENCES proxies(id) ON DELETE SET NULL,
    provider TEXT NOT NULL, -- openai, anthropic, bedrock, vertex, azure, ollama
    encrypted_key JSONB NOT NULL, -- {ciphertext, nonce, version}
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Security profiles (per-proxy or per-customer security configuration)
CREATE TABLE security_profiles (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    proxy_id UUID REFERENCES proxies(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT 'default',
    injection_enabled BOOLEAN NOT NULL DEFAULT true,
    injection_threshold REAL NOT NULL DEFAULT 0.7,
    pii_enabled BOOLEAN NOT NULL DEFAULT true,
    pii_action TEXT NOT NULL DEFAULT 'redact', -- block, redact, warn, log
    jailbreak_enabled BOOLEAN NOT NULL DEFAULT true,
    jailbreak_threshold REAL NOT NULL DEFAULT 0.8,
    bot_detection_enabled BOOLEAN NOT NULL DEFAULT false,
    custom_patterns_enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(customer_id, proxy_id, name)
);

-- Security patterns (custom regex/keyword patterns for threat detection)
CREATE TABLE security_patterns (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    pattern_type TEXT NOT NULL, -- regex, keyword, semantic
    pattern TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT 'block', -- block, warn, log
    severity TEXT NOT NULL DEFAULT 'medium', -- low, medium, high, critical
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Security access rules (IP allowlists, geo restrictions, etc.)
CREATE TABLE security_access_rules (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    profile_id UUID REFERENCES security_profiles(id) ON DELETE CASCADE,
    rule_type TEXT NOT NULL, -- ip_allowlist, ip_blocklist, geo_allow, geo_block
    value TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Rate limit rules (beyond simple per-key RPM)
CREATE TABLE rate_limit_rules (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    proxy_id UUID REFERENCES proxies(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    window_seconds INTEGER NOT NULL DEFAULT 60,
    max_requests INTEGER NOT NULL DEFAULT 100,
    max_tokens INTEGER, -- token-based rate limiting
    scope TEXT NOT NULL DEFAULT 'api_key', -- api_key, ip, user_id
    action TEXT NOT NULL DEFAULT 'reject', -- reject, queue, throttle
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Provider health tracking
CREATE TABLE provider_health (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'healthy', -- healthy, degraded, down
    last_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    latency_p50_ms INTEGER,
    latency_p95_ms INTEGER,
    error_rate REAL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, model)
);

-- Model routing rules (load balancing, fallback)
CREATE TABLE model_routing_rules (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    proxy_id UUID NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 100, -- for weighted routing
    is_fallback BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_customers_updated_at BEFORE UPDATE ON customers FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER trg_proxies_updated_at BEFORE UPDATE ON proxies FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER trg_provider_keys_updated_at BEFORE UPDATE ON proxy_provider_keys FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER trg_security_profiles_updated_at BEFORE UPDATE ON security_profiles FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER trg_provider_health_updated_at BEFORE UPDATE ON provider_health FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Seed default customer for OSS single-tenant mode
INSERT INTO customers (id, name, slug) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Default', 'default')
ON CONFLICT (slug) DO NOTHING;

-- Seed default security profile
INSERT INTO security_profiles (customer_id, name) VALUES
    ('00000000-0000-0000-0000-000000000001', 'default')
ON CONFLICT (customer_id, proxy_id, name) DO NOTHING;
