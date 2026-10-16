import type { Trace } from "@/api/client";

export type ObserveFilters = {
  traceType: string;
  status: string;
  provider: string;
  model: string;
  endUser: string;
  search: string;
  security: string;
  from: string;
  to: string;
  environment: string;
  release: string;
  traceName: string;
  // Tag filter: space- or comma-separated `key:value` pairs.
  tags: string;
};

export const TRACE_TYPES = [
  { value: "llm", label: "AI requests" },
  { value: "embedding", label: "Embeddings" },
  { value: "mcp", label: "MCP tools" },
  { value: "local", label: "Local AI" },
  { value: "extension", label: "Browser activity" },
  { value: "other", label: "Other / imported" },
] as const;

const fields = ["status", "provider", "model", "endUser", "search", "security", "from", "to", "environment", "release", "traceName", "tags", "traceType"] as const;
export type TraceSearch = Partial<ObserveFilters>;

export function parseTraceSearch(raw: Record<string, unknown>): TraceSearch {
  const out: TraceSearch = {};
  for (const field of fields) {
    if (typeof raw[field] === "string" && raw[field].trim()) out[field] = raw[field].trim();
  }
  if (out.traceType && !TRACE_TYPES.some((kind) => kind.value === out.traceType)) delete out.traceType;
  if (out.security && !["clean", "threat"].includes(out.security)) delete out.security;
  for (const field of ["from", "to"] as const) {
    if (!out[field]) continue;
    if (Number.isNaN(Date.parse(out[field]))) delete out[field];
    else out[field] = new Date(out[field]).toISOString();
  }
  if (out.tags) {
    out.tags = parseTagFilter(out.tags).join(",");
    if (!out.tags) delete out.tags;
  }
  return out;
}

export function traceFiltersFromSearch(search: TraceSearch): ObserveFilters {
  return { ...emptyFilters, ...search };
}

export function traceFiltersToSearch(filters: ObserveFilters): TraceSearch {
  return parseTraceSearch(filters);
}

export function traceType(trace: Pick<Trace, "provider" | "path">): string {
  if (trace.provider === "mcp") return "mcp";
  if (trace.provider === "ollama" || trace.path === "local/observe") return "local";
  if (trace.path?.toLowerCase().includes("/embeddings")) return "embedding";
  if (trace.provider === "browser-extension") return "extension";
  return trace.provider ? "llm" : "other";
}

export function mergeTraceRows(primary: Trace[], extra: Trace[]): Trace[] {
  return [...primary, ...extra].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at) || b.id.localeCompare(a.id));
}

// Apply the same predicate to projected browser events and placeholder rows.
// Gateway filters also run in ClickHouse before the result limit.
export function matchesTraceFilters(trace: Trace, filters: ObserveFilters): boolean {
  const contains = (value: string | undefined, search: string) => (value || "").toLowerCase().includes(search.toLowerCase());
  if (filters.traceType && traceType(trace) !== filters.traceType) return false;
  if (filters.status && !filters.status.split(",").includes(trace.status || "")) return false;
  if (filters.provider && trace.provider !== filters.provider) return false;
  if (filters.model && trace.model !== filters.model) return false;
  if (filters.environment && trace.environment !== filters.environment) return false;
  if (filters.endUser && trace.end_user_id !== filters.endUser) return false;
  if (filters.release && trace.release !== filters.release) return false;
  if (filters.traceName && !contains(trace.trace_name, filters.traceName)) return false;
  if (filters.search && !contains(trace.path, filters.search) && !contains(trace.trace_name, filters.search) && trace.id !== filters.search) return false;
  if (filters.security === "threat" && !trace.threat_detected) return false;
  if (filters.security === "clean" && trace.threat_detected) return false;
  if (filters.from && Date.parse(trace.started_at) < Date.parse(filters.from)) return false;
  if (filters.to && Date.parse(trace.started_at) > Date.parse(filters.to)) return false;
  return parseTagFilter(filters.tags).every((tag) => {
    const colon = tag.indexOf(":");
    return trace.tags?.[tag.slice(0, colon)] === tag.slice(colon + 1);
  });
}

export const emptyFilters: ObserveFilters = {
  traceType: "",
  status: "",
  provider: "",
  model: "",
  endUser: "",
  search: "",
  security: "",
  from: "",
  to: "",
  environment: "",
  release: "",
  traceName: "",
  tags: "",
};

export function parseTagFilter(raw: string): string[] {
  if (!raw) return [];
  const out: string[] = [];
  for (const piece of raw.split(/[,\s]+/)) {
    const trimmed = piece.trim();
    if (trimmed && trimmed.indexOf(":") > 0 && trimmed.indexOf(":") < trimmed.length - 1) out.push(trimmed);
  }
  return out;
}
