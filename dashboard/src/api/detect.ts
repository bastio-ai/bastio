// Types + fetch wrapper for POST /v1/detect. Kept hand-written (rather
// than from ../schema) so the playground isn't blocked on running
// `npm run generate:api` every time the detect shape evolves. Shapes
// mirror the DetectRequest/DetectResponse schemas in cmd/server/openapi.yaml.

const baseUrl = import.meta.env.VITE_API_URL ?? "";

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
  /** Tag runs initiated from the dashboard so the history panel can list them. */
  source?: "playground";
  /** Associate a run with a proxy; persisted into playground_runs.proxy_id. */
  proxy_id?: string;
  /** Shared across playground runs in this tab so Crescendo can see prior turns. */
  session_id?: string;
}

export interface DetectFinding {
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
  skipped?: boolean;
  action: string;
  score: number;
  /** Threshold this step was compared against. */
  threshold?: number;
  findings?: DetectFinding[];
  duration: number; // nanoseconds
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

export async function detect(req: DetectRequest): Promise<DetectResponse> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (req.session_id) {
    headers["X-Bastio-Session-Id"] = req.session_id;
  }
  const res = await fetch(`${baseUrl}/v1/detect`, {
    method: "POST",
    headers,
    body: JSON.stringify(req),
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`Detect failed: ${res.status} ${text || res.statusText}`);
  }
  return JSON.parse(text) as DetectResponse;
}

// Playground history — runs initiated from the dashboard with
// source="playground". Kept separate from traces so synthetic test
// activity does not pollute production observability.

export interface PlaygroundRun {
  id: string;
  profile_name: string;
  proxy_id?: string;
  direction: DetectDirection;
  prompt: string;
  sanitized_content: string;
  action: string;
  should_block: boolean;
  fired_detectors: string[];
  steps: DetectStepResult[];
  duration_ns: number;
  created_at: string;
}

export async function listPlaygroundRuns(limit = 50): Promise<PlaygroundRun[]> {
  const res = await fetch(`${baseUrl}/v1/playground/runs?limit=${limit}`, {
    method: "GET",
    headers: { "Content-Type": "application/json" },
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`List runs failed: ${res.status} ${text || res.statusText}`);
  }
  return JSON.parse(text) as PlaygroundRun[];
}

export async function deletePlaygroundRun(id: string): Promise<void> {
  const res = await fetch(`${baseUrl}/v1/playground/runs/${id}`, {
    method: "DELETE",
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`Delete run failed: ${res.status}`);
  }
}
