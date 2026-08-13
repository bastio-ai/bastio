import { useEffect, useMemo, useState, type ReactElement } from "react";
import {
  Activity,
  Bot,
  Box,
  ChevronDown,
  ChevronRight,
  Layers,
  Search,
  ShieldCheck,
  Sparkles,
  Wrench,
  Zap,
} from "lucide-react";
import type { Observation, TraceThreatDetection } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { cn, formatDuration } from "@/lib/utils";

type Props = {
  spans: Observation[];
  traceStartMs: number;
  traceDurationMs: number;
  selectedSpanId: string | null;
  onSelect: (span: Observation) => void;
  threats?: TraceThreatDetection[];
};

type TreeNode = Observation & { children: TreeNode[] };

const typeIcon: Record<string, typeof Activity> = {
  generation: Sparkles,
  span: Box,
  event: Zap,
  tool: Wrench,
  retrieval: Search,
  embedding: Layers,
  guardrail: ShieldCheck,
  agent: Bot,
};

// Recursive span tree with inline waterfall bar per row. Highlights spans
// that triggered a threat (red underline) so the security signal travels
// with the structure of the call, not a separate panel.
export function SpanTree({
  spans,
  traceStartMs,
  traceDurationMs,
  selectedSpanId,
  onSelect,
  threats = [],
}: Props) {
  const tree = useMemo(() => buildTree(spans), [spans]);
  const threatSpanIds = useMemo(() => spanIdsWithThreats(spans, threats), [spans, threats]);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    // By default expand the root plus any parent of the currently-selected
    // node so the selection is always visible.
    const next = new Set<string>();
    const addAncestors = (id: string | null) => {
      if (!id) return;
      const map = new Map(spans.map((s) => [s.id, s]));
      let cur = map.get(id);
      while (cur && cur.parent_id) {
        next.add(cur.parent_id);
        cur = map.get(cur.parent_id);
      }
    };
    for (const n of tree) next.add(n.id);
    addAncestors(selectedSpanId);
    setExpanded(next);
  }, [tree, selectedSpanId, spans]);

  if (!spans.length) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-muted-foreground">
        <Activity className="h-5 w-5" />
        <p className="text-xs">No spans recorded for this trace yet.</p>
      </div>
    );
  }

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const total = Math.max(traceDurationMs, 1);

  const rows: ReactElement[] = [];
  const walk = (node: TreeNode, depth: number) => {
    const startMs = new Date(node.started_at).getTime() - traceStartMs;
    const widthPct = Math.max(0.5, ((node.duration_ms ?? 0) / total) * 100);
    const offsetPct = Math.min(99, Math.max(0, (startMs / total) * 100));
    const Icon = typeIcon[node.type] ?? Activity;
    const isExpanded = expanded.has(node.id);
    const isSelected = node.id === selectedSpanId;
    const hasThreat = threatSpanIds.has(node.id);
    const barTone = hasThreat
      ? "bg-destructive/70"
      : node.status === "error"
      ? "bg-destructive/70"
      : "bg-primary/70";

    rows.push(
      <div
        key={node.id}
        className={cn(
          "group grid w-full grid-cols-[minmax(10rem,16rem)_1fr_3.5rem] items-center gap-2 border-l-2 px-2 py-1.5 text-left text-xs transition-colors",
          isSelected
            ? "border-foreground bg-muted/40"
            : "border-transparent hover:bg-muted/20",
          hasThreat ? "underline decoration-destructive decoration-2 underline-offset-2" : "",
        )}
      >
        <div className="flex items-center gap-1" style={{ paddingLeft: `${depth * 12}px` }}>
          {node.children.length ? (
            <button
              type="button"
              aria-label={`${isExpanded ? "Collapse" : "Expand"} ${node.name || node.type}`}
              aria-expanded={isExpanded}
              onClick={(e) => {
                e.stopPropagation();
                toggle(node.id);
              }}
              className="flex h-4 w-4 items-center justify-center rounded text-muted-foreground hover:text-foreground"
            >
              {isExpanded ? (
                <ChevronDown className="h-3 w-3" />
              ) : (
                <ChevronRight className="h-3 w-3" />
              )}
            </button>
          ) : (
            <span className="h-4 w-4" />
          )}
          <button
            type="button"
            onClick={() => onSelect(node)}
            className="flex min-w-0 flex-1 items-center gap-1.5 rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-current={isSelected ? "true" : undefined}
          >
            <Icon className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
            <span className="truncate font-mono">{node.name || node.type}</span>
            {node.status === "error" ? (
              <Badge variant="destructive" className="px-1 py-0 text-[9px]">
                err
              </Badge>
            ) : null}
          </button>
        </div>
        <button
          type="button"
          onClick={() => onSelect(node)}
          className="relative h-2 rounded bg-muted/50 outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={`Select ${node.name || node.type}; duration ${formatSpanDuration(node.duration_ms ?? 0)}`}
        >
          <div
            className={cn("absolute top-0 h-2 rounded", barTone)}
            style={{ left: `${offsetPct}%`, width: `${widthPct}%` }}
          />
        </button>
        <span className="text-right font-mono text-[10px] tabular-nums text-muted-foreground">
          {formatSpanDuration(node.duration_ms ?? 0)}
        </span>
      </div>,
    );

    if (isExpanded) {
      for (const child of node.children) walk(child, depth + 1);
    }
  };
  for (const n of tree) walk(n, 0);

  return <div className="divide-y divide-border/20">{rows}</div>;
}

function formatSpanDuration(durationMs: number) {
  return durationMs < 1 ? "<1ms" : formatDuration(durationMs);
}

function buildTree(spans: Observation[]): TreeNode[] {
  const byId = new Map<string, TreeNode>();
  for (const s of spans) byId.set(s.id, { ...s, children: [] });
  const roots: TreeNode[] = [];
  for (const s of spans) {
    const node = byId.get(s.id);
    if (!node) continue;
    if (s.parent_id && byId.has(s.parent_id)) {
      byId.get(s.parent_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  }
  const sortByStart = (a: TreeNode, b: TreeNode) =>
    new Date(a.started_at).getTime() - new Date(b.started_at).getTime();
  const sortRecursive = (nodes: TreeNode[]) => {
    nodes.sort(sortByStart);
    for (const n of nodes) sortRecursive(n.children);
  };
  sortRecursive(roots);
  return roots;
}

// Crude correlation: any span whose name or tool_name matches a detector or
// threat_type is considered involved. Good-enough until the detector writer
// emits span_id on security_threat_logs.
function spanIdsWithThreats(
  spans: Observation[],
  threats: TraceThreatDetection[],
): Set<string> {
  const set = new Set<string>();
  if (!threats.length) return set;
  const needles = threats.flatMap((t) => [t.detector_name, t.threat_type].filter(Boolean));
  if (!needles.length) return set;
  for (const s of spans) {
    const hay = `${s.name ?? ""} ${s.tool_name ?? ""} ${s.type ?? ""}`.toLowerCase();
    if (needles.some((n) => hay.includes(String(n).toLowerCase()))) {
      set.add(s.id);
    }
  }
  return set;
}
