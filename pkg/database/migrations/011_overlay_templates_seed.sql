-- Seed built-in overlay templates for common verticals.
--
-- Each template is a pre-built OverlaySnapshot (see
-- internal/security/overlay/schema.go) that tenants can clone via
-- POST /api/v1/overlays/from-template as a starting point.
--
-- Snapshot contents are delta-only: they ADD to the core security
-- configuration, and optionally tighten thresholds/strategies. None
-- of these templates disable a core detector (the schema disallows it).
--
-- Template content is conservative: sensible defaults per vertical,
-- not aggressive blocking. Operators should shadow, observe, and
-- adjust before activating on production traffic.
--
-- Idempotent: ON CONFLICT (slug) DO NOTHING so re-applying the
-- migration (or upgrading over a hand-seeded DB) is safe.

INSERT INTO tenant_policy_overlay_templates (slug, name, description, snapshot)
VALUES
(
    'healthcare',
    'Healthcare / HIPAA context',
    'Tighter PII handling and indirect-injection defaults for healthcare workloads handling protected health information. Start from this template if you process patient data.',
    $$ {
        "schema_version": 1,
        "additional_patterns": [
            {"name": "mrn_reference", "pattern_type": "keyword", "pattern": "mrn medical record number patient id", "action": "warn", "severity": "medium"},
            {"name": "insurance_id", "pattern_type": "keyword", "pattern": "insurance id member id policy number", "action": "warn", "severity": "medium"}
        ],
        "detector_overrides": {
            "pii": {"action": "block"},
            "indirect_injection": {"strategy": "block"},
            "output_exfil": {"strategy": "block"}
        }
    } $$::jsonb
),
(
    'fintech',
    'Financial services',
    'Tighter handling of account identifiers, routing numbers, and secrets. Use for banking, payments, or trading workloads.',
    $$ {
        "schema_version": 1,
        "additional_patterns": [
            {"name": "iban_keyword", "pattern_type": "keyword", "pattern": "iban swift bic", "action": "warn", "severity": "medium"},
            {"name": "routing_number", "pattern_type": "regex", "pattern": "\\b0[0-9]{8}\\b|\\b1[0-3][0-9]{7}\\b", "action": "warn", "severity": "high"}
        ],
        "detector_overrides": {
            "secrets": {"strategy": "block"},
            "injection": {"threshold": 0.6, "strategy": "block"},
            "output_exfil": {"strategy": "block"}
        }
    } $$::jsonb
),
(
    'consumer_chat',
    'Consumer chat / general assistant',
    'Softer jailbreak handling — warn rather than block — optimized for general consumer chat where false positives hurt UX.',
    $$ {
        "schema_version": 1,
        "detector_overrides": {
            "jailbreak": {"threshold": 0.75, "strategy": "warn"},
            "injection": {"threshold": 0.75, "strategy": "warn"}
        }
    } $$::jsonb
),
(
    'code_assistant',
    'Code assistant / developer tools',
    'Tuned for dev tools where base64/hex are common in legitimate content but secret leakage must be blocked.',
    $$ {
        "schema_version": 1,
        "additional_patterns": [
            {"name": "env_var_secret", "pattern_type": "regex", "pattern": "(?i)\\b[A-Z][A-Z0-9_]*_(TOKEN|SECRET|KEY|PASSWORD)\\s*=", "action": "warn", "severity": "high"}
        ],
        "detector_overrides": {
            "secrets": {"strategy": "block"}
        }
    } $$::jsonb
),
(
    'customer_support',
    'Customer support',
    'PII masking with explicit block on raw exfil attempts in outbound responses. Good fit for ticketing and help-desk assistants.',
    $$ {
        "schema_version": 1,
        "additional_patterns": [
            {"name": "ticket_id", "pattern_type": "regex", "pattern": "(?i)\\b(ticket|case)[-_ ]?#?[0-9]{4,}", "action": "log", "severity": "low"}
        ],
        "detector_overrides": {
            "pii": {"action": "mask"},
            "output_exfil": {"strategy": "block"}
        }
    } $$::jsonb
)
ON CONFLICT (slug) DO NOTHING;
