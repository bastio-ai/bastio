// Mirrors the DetectRequest / DetectResponse shapes defined in
// bastio/cmd/server/openapi.yaml. Kept hand-written (not codegen) so the
// SDK has a small dependency surface; regenerate by eye when the server
// schema changes.

export type DetectDirection = "input" | "output";

export type DetectStrategy =
  | "block"
  | "mask"
  | "tokenize"
  | "warn"
  | "log_only";

export interface DetectStep {
  detector: string;
  strategy: DetectStrategy;
  threshold?: number;
  options?: Record<string, string>;
}

export interface DetectMessage {
  role: string;
  content: string;
}

export interface DetectRequest {
  messages: DetectMessage[];
  profile?: string;
  direction?: DetectDirection;
  steps?: DetectStep[];
}

export interface Finding {
  threat_type: "injection" | "pii" | "jailbreak";
  detector_name: string;
  severity: "critical" | "high" | "medium" | "low" | "info";
  score: number;
  confidence: number;
  matched_pattern?: string;
  matched_content?: string;
  action: string;
  message: string;
}

export interface DetectStepResult {
  detector: string;
  strategy: DetectStrategy;
  fired: boolean;
  /**
   * True when the step was configured but not executed because an
   * earlier step short-circuited the pipeline (e.g. block). Useful for
   * surfacing the full intended policy in UIs.
   */
  skipped?: boolean;
  action: string;
  score: number;
  findings?: Finding[];
  duration: number;
}

export interface DetectMessageResult {
  role: string;
  original: string;
  sanitized_content: string;
  action: string;
  should_block: boolean;
  steps: DetectStepResult[];
}

export interface DetectResponse {
  profile: string;
  direction: DetectDirection;
  action: string;
  should_block: boolean;
  messages: DetectMessageResult[];
}

export interface BastioClientOptions {
  /**
   * Full URL to the Bastio API, e.g. "https://bastio.example.com".
   * Trailing slashes are tolerated.
   */
  baseURL: string;
  /**
   * Dashboard session token or proxy API key. Sent as a Bearer token
   * exactly as Bastio's auth middleware expects.
   */
  apiKey?: string;
  /**
   * Overridable fetch — useful for tests and for edge runtimes that
   * need a custom global.
   */
  fetch?: typeof fetch;
  /**
   * Per-call timeout in milliseconds. Defaults to 10_000.
   */
  timeoutMs?: number;
  /**
   * Extra headers applied to every request (e.g. X-Request-Id).
   */
  headers?: Record<string, string>;
}
