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
