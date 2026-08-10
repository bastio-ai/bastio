// Overlay (Custom Policies) API client — typed via openapi-fetch.
//
// Previously a plain-fetch client with hand-maintained local types.
// Now flows through the same typed `http`/`unwrap` pair the rest of
// the dashboard uses, with types re-exported from the generated
// OpenAPI schema. Any future backend change that touches overlay
// shapes will fail the dashboard build at codegen time instead of
// slipping through to runtime.

import { http, unwrap } from "./typed";
import type { components } from "./schema";

// ---- Type re-exports from the generated schema ----

export type OverlaySnapshot = components["schemas"]["OverlaySnapshot"];
export type Overlay = components["schemas"]["Overlay"];
export type OverlayVersion = components["schemas"]["OverlayVersion"];
export type VersionState = OverlayVersion["state"];
export type PatternRule = components["schemas"]["OverlayPatternRule"];
export type AccessRule = components["schemas"]["OverlayAccessRule"];
export type DetectorOverride = components["schemas"]["OverlayDetectorOverride"];
export type PIIOverride = components["schemas"]["OverlayPIIOverride"];
export type DetectorOverrides = components["schemas"]["OverlayDetectorOverrides"];
export type PluginDetectorRef = components["schemas"]["OverlayPluginDetectorRef"];
export type AuditEntry = components["schemas"]["OverlayAuditEntry"];
export type ShadowEvent = components["schemas"]["OverlayShadowEvent"];
export type Template = components["schemas"]["OverlayTemplate"];
export type OverlayWarning = components["schemas"]["OverlayWarning"];
export type PreviewSample = components["schemas"]["OverlayPreviewSample"];
export type PreviewSampleResult = components["schemas"]["OverlayPreviewSampleResult"];
export type PreviewSummary = components["schemas"]["OverlayPreviewSummary"];
export type PreviewResult = components["schemas"]["OverlayPreviewResult"];

// ---- Client methods ----

export const overlayApi = {
  list: () => unwrap(http.GET("/v1/overlays", {})).then((r) => r.overlays),

  get: (id: string) =>
    unwrap(http.GET("/v1/overlays/{id}", { params: { path: { id } } })),

  create: (body: components["schemas"]["CreateOverlayRequest"]) =>
    unwrap(http.POST("/v1/overlays", { body })),

  remove: (id: string) =>
    unwrap(http.DELETE("/v1/overlays/{id}", { params: { path: { id } } })),

  listVersions: (id: string) =>
    unwrap(
      http.GET("/v1/overlays/{id}/versions", { params: { path: { id } } }),
    ).then((r) => r.versions),

  getVersion: (id: string, n: number) =>
    unwrap(
      http.GET("/v1/overlays/{id}/versions/{n}", {
        params: { path: { id, n } },
      }),
    ),

  createVersion: (
    id: string,
    body: components["schemas"]["CreateOverlayVersionRequest"],
  ) =>
    unwrap(
      http.POST("/v1/overlays/{id}/versions", {
        params: { path: { id } },
        body,
      }),
    ).then((r) => r.version),

  promoteShadow: (id: string, n: number, reason: string) =>
    unwrap(
      http.POST("/v1/overlays/{id}/versions/{n}/shadow", {
        params: { path: { id, n } },
        body: { reason },
      }),
    ),

  activate: (id: string, n: number, reason: string) =>
    unwrap(
      http.POST("/v1/overlays/{id}/versions/{n}/activate", {
        params: { path: { id, n } },
        body: { reason },
      }),
    ),

  preview: (id: string, n: number, samples: PreviewSample[], proxyID?: string) =>
    unwrap(
      http.POST("/v1/overlays/{id}/versions/{n}/preview", {
        params: { path: { id, n } },
        body: { samples, proxy_id: proxyID },
      }),
    ),

  rollback: (id: string, reason: string) =>
    unwrap(
      http.POST("/v1/overlays/{id}/rollback", {
        params: { path: { id } },
        body: { reason },
      }),
    ),

  audit: (id: string) =>
    unwrap(
      http.GET("/v1/overlays/{id}/audit", { params: { path: { id } } }),
    ).then((r) => r.entries),

  shadowEvents: (id: string) =>
    unwrap(
      http.GET("/v1/overlays/{id}/shadow-events", { params: { path: { id } } }),
    ).then((r) => r.events),

  templates: () =>
    unwrap(http.GET("/v1/overlay-templates", {})).then((r) => r.templates),

  createFromTemplate: (
    body: components["schemas"]["OverlayFromTemplateRequest"],
  ) => unwrap(http.POST("/v1/overlays/from-template", { body })),
};

// Query keys factory — colocated so route code has stable keys.
export const overlayKeys = {
  all: ["overlays"] as const,
  list: () => [...overlayKeys.all, "list"] as const,
  detail: (id: string) => [...overlayKeys.all, "detail", id] as const,
  versions: (id: string) => [...overlayKeys.all, "versions", id] as const,
  audit: (id: string) => [...overlayKeys.all, "audit", id] as const,
  shadowEvents: (id: string) => [...overlayKeys.all, "shadow-events", id] as const,
  templates: () => [...overlayKeys.all, "templates"] as const,
};

// emptySnapshot returns a minimal valid snapshot — used as the initial
// JSON for the "New overlay" editor so users don't stare at {}.
export function emptySnapshot(): OverlaySnapshot {
  return { schema_version: 1 };
}

type ThreatEvent = components["schemas"]["ThreatEvent"];

// snapshotFromThreat seeds an OverlaySnapshot from a flagged threat.
// The "capture a decision from a real request" flow: user clicks
// a button on the threat detail page, we pre-populate the policy
// editor with a single pattern rule drawn from that threat, and they
// edit/confirm before saving.
//
// Heuristics are deliberately simple — the editor is right there for
// the user to refine:
//   - pattern_type=regex if the source contains regex metacharacters,
//     else keyword
//   - source = matched_pattern when present, else first 200 chars of
//     matched_content (so we don't dump an entire user message into a
//     rule unintentionally)
//   - action = block or warn mirrored from the threat; other action
//     values (allow, redact, log) fall back to "warn" so the captured
//     rule never silently upgrades permissiveness
//   - severity = threat.severity if it's one of the known enum values,
//     else "medium"
export function snapshotFromThreat(threat: ThreatEvent): OverlaySnapshot {
  const matched = (threat.matched_pattern || "").trim();
  const content = (threat.matched_content || "").trim();
  const source = matched || content.slice(0, 200);

  const hasRegexMetachars = /[\\[\]()|*+?{}^$]/.test(source);
  const patternType: PatternRule["pattern_type"] = hasRegexMetachars
    ? "regex"
    : "keyword";

  const action: PatternRule["action"] =
    threat.action_taken === "block" ? "block" : "warn";

  const allowedSeverities: PatternRule["severity"][] = [
    "low",
    "medium",
    "high",
    "critical",
  ];
  const severity: PatternRule["severity"] = allowedSeverities.includes(
    threat.severity as PatternRule["severity"],
  )
    ? (threat.severity as PatternRule["severity"])
    : "medium";

  const name = `captured_${threat.detector_name}_${threat.id.slice(0, 8)}`;

  return {
    schema_version: 1,
    additional_patterns: [
      {
        name,
        pattern_type: patternType,
        pattern: source,
        action,
        severity,
      },
    ],
  };
}

// suggestedPolicyNameFromThreat proposes a readable name for a
// brand-new custom policy when it's created from a flagged threat.
export function suggestedPolicyNameFromThreat(threat: ThreatEvent): string {
  return `${threat.detector_name}-captures`;
}

// stateTone maps a version state to a Badge variant the routes use to
// surface current/shadow/superseded visually.
export function stateTone(
  state: VersionState,
): "default" | "secondary" | "outline" | "destructive" {
  switch (state) {
    case "active":
      return "default";
    case "shadow":
      return "secondary";
    case "draft":
      return "outline";
    case "superseded":
      return "outline";
  }
}

// Built-in template presets fallback map
export const BUILTIN_TEMPLATES: Record<string, Template> = {
  healthcare: {
    id: "tpl_healthcare",
    slug: "healthcare",
    name: "Healthcare / HIPAA Shield",
    description: "Tighter PII tokenization and indirect-injection defaults for healthcare workloads handling protected health information (PHI).",
    is_builtin: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    snapshot: {
      schema_version: 1,
      additional_patterns: [
        { name: "mrn_reference", pattern_type: "keyword", pattern: "mrn medical record number patient id", action: "warn", severity: "medium" },
        { name: "insurance_id", pattern_type: "keyword", pattern: "insurance id member id policy number", action: "warn", severity: "medium" },
        { name: "icd10_diagnosis", pattern_type: "regex", pattern: "(?i)\\b[A-Z][0-9]{2}(\\.[0-9]{1,4})?\\b", action: "log", severity: "low" }
      ],
      detector_overrides: {
        pii: { action: "tokenize" },
        indirect_injection: { strategy: "block" },
        output_exfil: { strategy: "block" }
      }
    }
  },
  fintech: {
    id: "tpl_fintech",
    slug: "fintech",
    name: "Financial Services Guardrail",
    description: "Tighter handling of account identifiers, IBAN/SWIFT codes, routing numbers, and API keys for banking and payments.",
    is_builtin: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    snapshot: {
      schema_version: 1,
      additional_patterns: [
        { name: "iban_keyword", pattern_type: "keyword", pattern: "iban swift bic credit card cvv", action: "block", severity: "high" },
        { name: "routing_number", pattern_type: "regex", pattern: "\\b0[0-9]{8}\\b|\\b1[0-3][0-9]{7}\\b", action: "warn", severity: "high" }
      ],
      detector_overrides: {
        secrets: { strategy: "block" },
        injection: { threshold: 0.6, strategy: "block" },
        output_exfil: { strategy: "block" }
      }
    }
  },
  code_assistant: {
    id: "tpl_code_assistant",
    slug: "code_assistant",
    name: "Code Assistant Guard",
    description: "Tuned for developer tools to prevent destructive shell commands (rm -rf, drop table) and secret leakage.",
    is_builtin: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    snapshot: {
      schema_version: 1,
      additional_patterns: [
        { name: "env_var_secret", pattern_type: "regex", pattern: "(?i)\\b[A-Z][A-Z0-9_]*_(TOKEN|SECRET|KEY|PASSWORD)\\s*=", action: "warn", severity: "high" },
        { name: "destructive_command", pattern_type: "regex", pattern: "(?i)(rm\\s+-rf|drop\\s+table|chmod\\s+777)", action: "block", severity: "critical" }
      ],
      detector_overrides: {
        secrets: { strategy: "block" },
        jailbreak: { threshold: 0.7, strategy: "block" }
      }
    }
  },
  customer_support: {
    id: "tpl_customer_support",
    slug: "customer_support",
    name: "Customer Support Assistant",
    description: "PII masking with explicit block on raw exfiltration attempts in outbound responses.",
    is_builtin: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    snapshot: {
      schema_version: 1,
      additional_patterns: [
        { name: "ticket_id", pattern_type: "regex", pattern: "(?i)\\b(ticket|case)[-_ ]?#?[0-9]{4,}", action: "log", severity: "low" }
      ],
      detector_overrides: {
        pii: { action: "mask" },
        output_exfil: { strategy: "block" }
      }
    }
  },
  consumer_chat: {
    id: "tpl_consumer_chat",
    slug: "consumer_chat",
    name: "Consumer Chat Assistant",
    description: "Balanced jailbreak and prompt injection thresholds optimized for consumer UX with low false positives.",
    is_builtin: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    snapshot: {
      schema_version: 1,
      detector_overrides: {
        jailbreak: { threshold: 0.75, strategy: "warn" },
        injection: { threshold: 0.75, strategy: "warn" }
      }
    }
  }
};

// Aliases mapping slug variations to canonical built-in templates
export const TEMPLATE_SLUG_ALIASES: Record<string, string> = {
  "hipaa-phi-guardrail": "healthcare",
  "financial-sec-compliance": "fintech",
  "code-execution-defense": "code_assistant",
  "strict-prompt-protection": "customer_support"
};
