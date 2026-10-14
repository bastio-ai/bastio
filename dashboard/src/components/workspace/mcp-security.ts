// Diagnostics intentionally exclude withheld text and matched values.
export class MCPSecurityError extends Error {
  stage: "arguments" | "result";
  reason: string;
  blockingDetectors: string[];
  categories: string[];
  rules: string[];
  constructor(stage: "arguments" | "result", reason: string, categories: string[], rules: string[], blockingDetectors: string[] = []) {
    super(reason);
    this.blockingDetectors = blockingDetectors; this.stage = stage; this.reason = reason; this.categories = categories; this.rules = rules;
  }
  static from(value: unknown): MCPSecurityError | null {
    if (typeof value !== "object" || value === null) return null;
    const body = value as Record<string, unknown>;
    if ((body.stage !== "arguments" && body.stage !== "result") || typeof body.reason !== "string") return null;
    const identifiers = (values: unknown) => Array.isArray(values) ? [...new Set(values.filter((v): v is string => typeof v === "string" && /^[a-z][a-z0-9_.:-]{0,100}$/.test(v)))].slice(0, 16) : [];
    return new MCPSecurityError(body.stage, body.reason, identifiers(body.categories), identifiers(body.rules), identifiers(body.blocking_detectors));
  }
}
export function mcpCategoryLabel(category: string): string {
  return ({ pii: "Personal data (PII)", jailbreak: "Jailbreak detection", injection: "Prompt injection", indirect_injection: "Indirect prompt injection", secrets: "Secrets", output_exfil: "Data exfiltration" } as Record<string, string>)[category] ?? category;
}
export function mcpRuleLabel(rule: string): string {
  return ({ "pii:phone": "Phone number pattern", "pii:email": "Email address pattern", "jailbreak:structural.encoding_density": "Long encoded-looking character sequence", "pii:credit_card": "Payment card pattern" } as Record<string, string>)[rule] ?? rule.replaceAll("_", " ").replace(":", " · ");
}
