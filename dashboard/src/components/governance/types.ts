/**
 * Governance presentation helpers — labels, tone mappers.
 *
 * API types live in `@/api/client` (re-exported from the generated OpenAPI
 * schema). This file only carries view-layer concerns (friendly labels,
 * shadcn variant mappings).
 */

import type {
  GovernanceSeverity,
  GovernanceAction,
} from "@/api/client";

// Re-exported under shorter local names for ergonomic imports.
export type Severity = GovernanceSeverity;
export type ActionStr = GovernanceAction;

export const ruleLabels: Record<string, string> = {
  "pii.email": "Email",
  "pii.phone.us": "Phone (US)",
  "pii.phone.e164": "Phone",
  "pii.ssn": "SSN",
  "pii.iban": "IBAN",
  "pii.card": "Card #",
  "pii.passport.us": "Passport (US)",
  "pii.passport.uk": "Passport (UK)",
  "secret.aws_access_key": "AWS key",
  "secret.github_pat": "GitHub PAT",
  "secret.stripe": "Stripe key",
  "secret.slack": "Slack token",
  "secret.google_api": "Google API key",
  "secret.openai": "OpenAI key",
  "secret.openai_project": "OpenAI proj",
  "secret.anthropic": "Anthropic key",
  "secret.jwt": "JWT",
  "secret.private_key": "Private key",
  "secret.high_entropy": "Token (entropy)",
  "code.fenced_block": "Code (fenced)",
  "code.keywords": "Code (kw cluster)",
  "code.sql": "SQL",
  "keyword.custom": "Custom keyword",
};

export function ruleDisplay(id: string): string {
  return ruleLabels[id] ?? id;
}

export function sevTone(s: Severity): "default" | "secondary" | "destructive" {
  if (s === "high") return "destructive";
  if (s === "medium") return "secondary";
  return "default";
}

export function actionTone(a: ActionStr): "default" | "secondary" | "destructive" {
  if (a === "blocked" || a === "overridden") return "destructive";
  if (a === "redirected" || a === "warned") return "secondary";
  return "default";
}

/**
 * Render a user identifier respecting the customer's pseudonymize-PII setting.
 *
 * - When pseudonymize is false (default): show the first 12 chars of the raw ID
 *   followed by an ellipsis. Real emails / IDP subjects show through; that's
 *   what most enterprises want for incident response.
 * - When pseudonymize is true: render a stable "Employee #NNNN" pseudonym
 *   derived deterministically from the raw ID. The same employee always renders
 *   to the same pseudonym so leaderboards still cluster correctly, but the
 *   actual identifier never reaches the rendered HTML or PDF.
 *
 * Pseudonymization happens at the render layer only. The underlying
 * ClickHouse rows still carry the real user_id for audit completeness.
 */
export function formatUserID(rawID: string | undefined, pseudonymize: boolean): string {
  if (!rawID) return "—";
  if (!pseudonymize) {
    return rawID.length > 12 ? `${rawID.slice(0, 12)}…` : rawID;
  }
  return `Employee #${stablePseudoNumber(rawID)}`;
}

// cyrb53 — fast, well-distributed 53-bit string hash. Non-crypto.
function stablePseudoNumber(s: string): string {
  let h1 = 0xdeadbeef ^ 5;
  let h2 = 0x41c6ce57 ^ 5;
  for (let i = 0, ch: number; i < s.length; i++) {
    ch = s.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
  h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
  h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  const num = 4294967296 * (2097151 & h2) + (h1 >>> 0);
  // 4-digit zero-padded for visual consistency
  return String(num % 10000).padStart(4, "0");
}

