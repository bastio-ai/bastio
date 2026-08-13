import { useMemo, useState } from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowLeft,
  ChevronRight,
  Eye,
  GitCompare,
  History,
  Plus,
  ShieldQuestion,
  Trash2,
  Undo2,
  Zap,
} from "lucide-react";

import {
  overlayApi,
  overlayKeys,
  stateTone,
  type OverlayVersion,
  type OverlayWarning,
  type PreviewResult,
  type ShadowEvent,
  type AuditEntry,
} from "@/api/overlay";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { SkeletonRows } from "@/components/skeleton";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";

type Tab = "versions" | "audit" | "shadow-events";

export function OverlayDetailPage() {
  const { id } = useParams({ from: "/overlays/$id" });
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [tab, setTab] = useState<Tab>("versions");

  const overlayQuery = useQuery({
    queryKey: overlayKeys.detail(id),
    queryFn: () => overlayApi.get(id),
  });
  const versionsQuery = useQuery({
    queryKey: overlayKeys.versions(id),
    queryFn: () => overlayApi.listVersions(id),
  });

  const rollback = useMutation({
    mutationFn: () => overlayApi.rollback(id, "rollback from dashboard"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.detail(id) });
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
    },
  });

  const remove = useMutation({
    mutationFn: () => overlayApi.remove(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.list() });
      navigate({ to: "/overlays" });
    },
  });

  const promoteShadow = useMutation({
    mutationFn: (n: number) => overlayApi.promoteShadow(id, n, "promote from dashboard"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
    },
  });

  const activate = useMutation({
    mutationFn: (n: number) => overlayApi.activate(id, n, "activate from dashboard"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.detail(id) });
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
    },
  });

  const overlay = overlayQuery.data?.overlay;
  const activeVersion = overlayQuery.data?.active_version;
  const activeWarnings = overlayQuery.data?.active_warnings ?? [];

  if (overlayQuery.isLoading) return <SkeletonRows count={5} />;
  if (overlayQuery.isError || !overlay) {
    return (
      <Card className="border-border/50 mt-4">
        <CardContent className="p-6 text-center text-sm text-muted-foreground">
          Overlay not found.{" "}
          <button
            className="underline"
            onClick={() => navigate({ to: "/overlays" })}
          >
            Back to Custom Policies
          </button>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <div className="flex items-center gap-2 text-xs text-muted-foreground mb-2">
        <button
          className="inline-flex items-center hover:text-foreground"
          onClick={() => navigate({ to: "/overlays" })}
        >
          <ArrowLeft className="h-3 w-3" /> Custom Policies
        </button>
        <ChevronRight className="h-3 w-3" />
        <span className="font-mono">{overlay.name}</span>
      </div>

      <AdminPageHeader
        eyebrow="Custom policy"
        title={<span className="font-mono">{overlay.name}</span>}
        description={overlay.description || "No policy description has been provided."}
        badge={activeVersion ? <Badge variant="default" className="px-1.5 py-0 text-[10px]">active v{activeVersion.version}</Badge> : <Badge variant="outline" className="px-1.5 py-0 text-[10px]">no active version</Badge>}
        actions={<>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={rollback.isPending}
            onClick={() => rollback.mutate()}
          >
            <Undo2 className="h-3 w-3" /> Rollback
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs text-destructive"
            disabled={remove.isPending}
            onClick={() => {
              const isActive = !!overlay.active_version_id;
              const msg = isActive
                ? `Delete "${overlay.name}"?\n\nThis policy is currently ACTIVE — its rules and overrides are applying to traffic right now. Deleting it will immediately revert this tenant to the core security profile.\n\nAll versions, shadow events, and audit history will be permanently removed.\n\nThis cannot be undone.`
                : `Delete "${overlay.name}"?\n\nAll versions, shadow events, and audit history will be permanently removed. This cannot be undone.`;
              if (confirm(msg)) {
                remove.mutate();
              }
            }}
          >
            <Trash2 className="h-3 w-3" /> Delete
          </Button>
          <Button
            size="sm"
            className="h-8 text-xs"
            onClick={() =>
              navigate({
                to: "/overlays/$id/versions/new",
                params: { id },
                search: { from_threat: undefined },
              })
            }
          >
            <Plus className="h-3 w-3" /> New version
          </Button>
        </>}
      />

      <AdminSummaryStrip items={[
        { label: "Versions", value: versionsQuery.data?.length ?? 0, detail: "Immutable history" },
        { label: "Active version", value: activeVersion ? `v${activeVersion.version}` : "None", detail: activeVersion?.commit_message || "Core profile only", tone: activeVersion ? "success" : "warning" },
        { label: "Draft versions", value: (versionsQuery.data ?? []).filter((version) => version.state === "draft").length, detail: "Not applied to traffic" },
        { label: "Security warnings", value: activeWarnings.length, detail: activeWarnings.length ? "Active version loosens defaults" : "No weakened defaults", tone: activeWarnings.length ? "danger" : "success" },
      ]} />

      {activeWarnings.length > 0 ? (
        <WarningBanner warnings={activeWarnings} />
      ) : null}

      <div className="flex gap-1 border-b border-border/50 mb-3 text-xs">
        <TabButton active={tab === "versions"} onClick={() => setTab("versions")}>
          <History className="h-3 w-3 mr-1" /> Versions
        </TabButton>
        <TabButton active={tab === "shadow-events"} onClick={() => setTab("shadow-events")}>
          <ShieldQuestion className="h-3 w-3 mr-1" /> Shadow events
        </TabButton>
        <TabButton active={tab === "audit"} onClick={() => setTab("audit")}>
          <Zap className="h-3 w-3 mr-1" /> Audit
        </TabButton>
      </div>

      {tab === "versions" ? (
        <VersionsTable
          id={id}
          versions={versionsQuery.data ?? []}
          loading={versionsQuery.isLoading}
          onPromoteShadow={(n) => promoteShadow.mutate(n)}
          onActivate={(n) => activate.mutate(n)}
        />
      ) : tab === "shadow-events" ? (
        <ShadowEventsTable id={id} />
      ) : (
        <AuditTable id={id} />
      )}
    </>
  );
}

// WarningBanner flags any override on the active version that weakens
// security relative to the shipped defaults. Visible by default on the
// detail page; invisible when the server returns no warnings.
function WarningBanner({ warnings }: { warnings: OverlayWarning[] }) {
  return (
    <Card className="border-destructive/40 bg-destructive/5 mb-4">
      <CardContent className="p-3 space-y-1">
        <p className="text-[10px] font-semibold uppercase tracking-wider text-destructive">
          Active version loosens security
        </p>
        <ul className="space-y-0.5 text-xs">
          {warnings.map((w, i) => (
            <li key={i} className="flex items-start gap-2">
              <span className="mt-0.5">•</span>
              <span>
                <span className="font-mono">{w.detector}.{w.field}</span>:{" "}
                <span className="text-muted-foreground">{w.from}</span>
                {" → "}
                <span className="font-semibold">{w.to}</span>
                <span className="text-muted-foreground"> — {w.message}</span>
              </span>
            </li>
          ))}
        </ul>
        <p className="text-[11px] text-muted-foreground pt-1">
          These overrides are permitted, but they make detection less strict than the shipped defaults. Rolling back to a prior version reverts them instantly.
        </p>
      </CardContent>
    </Card>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center px-3 py-1.5 border-b-2 -mb-px transition-colors ${
        active
          ? "border-foreground text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function VersionsTable({
  id,
  versions,
  loading,
  onPromoteShadow,
  onActivate,
}: {
  id: string;
  versions: OverlayVersion[];
  loading: boolean;
  onPromoteShadow: (n: number) => void;
  onActivate: (n: number) => void;
}) {
  const [previewVersion, setPreviewVersion] = useState<number | null>(null);
  const [diffVersion, setDiffVersion] = useState<OverlayVersion | null>(null);
  const activeVersion = versions.find((v) => v.state === "active") ?? null;
  if (loading) return <SkeletonRows count={3} />;
  if (!versions.length) {
    return (
      <Card className="border-border/50">
        <CardContent className="p-6 text-center text-sm text-muted-foreground">
          No versions yet. Create one from the button above.
        </CardContent>
      </Card>
    );
  }
  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent border-border/50">
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Version
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                State
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Commit
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Author
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Created
              </TableHead>
              <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Actions
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {versions.map((v) => (
              <TableRow key={v.id} className="border-border/30">
                <TableCell className="font-mono text-xs">v{v.version}</TableCell>
                <TableCell>
                  <Badge variant={stateTone(v.state)} className="text-[10px] px-1.5 py-0">
                    {v.state}
                  </Badge>
                </TableCell>
                <TableCell className="text-xs">{v.commit_message || "—"}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {v.created_by || "—"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {new Date(v.created_at).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-2">
                    {activeVersion && activeVersion.id !== v.id ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 text-[11px]"
                        onClick={() => setDiffVersion(v)}
                      >
                        <GitCompare className="h-3 w-3" /> Diff
                      </Button>
                    ) : null}
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 text-[11px]"
                      onClick={() => setPreviewVersion(v.version)}
                    >
                      <Eye className="h-3 w-3" /> Preview
                    </Button>
                    {v.state === "draft" ? (
                      <>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 text-[11px]"
                          onClick={() => onPromoteShadow(v.version)}
                        >
                          Shadow
                        </Button>
                        <Button
                          size="sm"
                          className="h-7 text-[11px]"
                          onClick={() => onActivate(v.version)}
                        >
                          Activate
                        </Button>
                      </>
                    ) : v.state === "shadow" ? (
                      <Button
                        size="sm"
                        className="h-7 text-[11px]"
                        onClick={() => onActivate(v.version)}
                      >
                        Promote to active
                      </Button>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
      {previewVersion !== null ? (
        <PreviewDialog
          id={id}
          version={previewVersion}
          onClose={() => setPreviewVersion(null)}
        />
      ) : null}
      {diffVersion && activeVersion ? (
        <DiffDialog
          active={activeVersion}
          candidate={diffVersion}
          onClose={() => setDiffVersion(null)}
        />
      ) : null}
    </Card>
  );
}

// DiffDialog shows a line-level diff between the currently-active
// version's snapshot and a selected candidate version. JSON is
// pretty-printed with stable key order, then each side is marked
// with + / - / = via a short LCS pass. Good enough for overlays
// where snapshots stay small; no external diff dep required.
function DiffDialog({
  active,
  candidate,
  onClose,
}: {
  active: OverlayVersion;
  candidate: OverlayVersion;
  onClose: () => void;
}) {
  const activeText = useMemo(() => stableStringify(active.snapshot), [active.snapshot]);
  const candidateText = useMemo(
    () => stableStringify(candidate.snapshot),
    [candidate.snapshot],
  );
  const rows = useMemo(
    () => diffLines(activeText.split("\n"), candidateText.split("\n")),
    [activeText, candidateText],
  );

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
      <Card className="border-border/50 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
        <CardContent className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold">
              Diff — active (v{active.version}) → candidate (v{candidate.version})
            </h3>
            <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}>
              ✕
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Lines present only in the active version show in red; lines added by v
            {candidate.version} show in green.
          </p>
          <pre className="rounded border border-border/40 bg-muted/10 p-2 font-mono text-[11px] leading-snug overflow-x-auto">
            {rows.map((r, i) => (
              <div
                key={i}
                className={
                  r.kind === "add"
                    ? "bg-green-500/10 text-green-400"
                    : r.kind === "del"
                      ? "bg-red-500/10 text-red-400"
                      : "text-muted-foreground"
                }
              >
                <span className="select-none pr-2">
                  {r.kind === "add" ? "+" : r.kind === "del" ? "-" : " "}
                </span>
                {r.text}
              </div>
            ))}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}

// stableStringify serialises a JSON value with keys sorted so the
// diff highlights real changes rather than key-ordering noise.
function stableStringify(value: unknown, indent = 2): string {
  return JSON.stringify(sortKeys(value), null, indent);
}

function sortKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortKeys);
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const k of Object.keys(value as Record<string, unknown>).sort()) {
      out[k] = sortKeys((value as Record<string, unknown>)[k]);
    }
    return out;
  }
  return value;
}

// diffLines is a minimal LCS-based line diff. Good enough for the
// small snapshots we're comparing (typically <200 lines). Returns
// rows tagged as "same", "add", or "del" in display order.
type DiffRow = { kind: "same" | "add" | "del"; text: string };

function diffLines(oldLines: string[], newLines: string[]): DiffRow[] {
  const m = oldLines.length;
  const n = newLines.length;
  // LCS DP table. Array access is bounded by the loop conditions
  // below, so the non-null assertions are safe — strict-index access
  // just can't infer that.
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i]![j] =
        oldLines[i]! === newLines[j]!
          ? dp[i + 1]![j + 1]! + 1
          : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (oldLines[i]! === newLines[j]!) {
      rows.push({ kind: "same", text: oldLines[i]! });
      i++;
      j++;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      rows.push({ kind: "del", text: oldLines[i]! });
      i++;
    } else {
      rows.push({ kind: "add", text: newLines[j]! });
      j++;
    }
  }
  while (i < m) rows.push({ kind: "del", text: oldLines[i++]! });
  while (j < n) rows.push({ kind: "add", text: newLines[j++]! });
  return rows;
}

// PreviewDialog lets the operator paste sample content strings and
// see how the candidate version's effective profile would verdict
// each one compared to the current active. Answers "would this
// change what gets blocked?" without flipping to shadow mode.
function PreviewDialog({
  id,
  version,
  onClose,
}: {
  id: string;
  version: number;
  onClose: () => void;
}) {
  const [samplesText, setSamplesText] = useState("");
  const [result, setResult] = useState<PreviewResult | null>(null);

  const preview = useMutation({
    mutationFn: async () => {
      const samples = samplesText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean)
        .map((content) => ({ content }));
      if (samples.length === 0) {
        throw new Error("Enter at least one sample (one per line)");
      }
      return overlayApi.preview(id, version, samples);
    },
    onSuccess: (r) => setResult(r),
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
      <Card className="border-border/50 w-full max-w-3xl max-h-[90vh] overflow-y-auto">
        <CardContent className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold">Preview impact — v{version}</h3>
            <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}>
              ✕
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Paste one sample per line. Each line is scanned through both the current active effective profile and v{version}'s effective profile; divergences are flagged.
          </p>
          <textarea
            placeholder="user might say: ignore all previous instructions..."
            value={samplesText}
            onChange={(e) => setSamplesText(e.target.value)}
            rows={6}
            spellCheck={false}
            className="w-full rounded border border-border/50 bg-muted/20 p-2 font-mono text-[11px] leading-relaxed focus:outline-none"
          />
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onClose}>
              Close
            </Button>
            <Button
              size="sm"
              className="h-8 text-xs"
              onClick={() => preview.mutate()}
              disabled={preview.isPending || !samplesText.trim()}
            >
              Run preview
            </Button>
          </div>
          {preview.isError ? (
            <p className="text-xs text-destructive">
              {(preview.error as Error | undefined)?.message ?? "Preview failed"}
            </p>
          ) : null}
          {result ? <PreviewResultsView result={result} /> : null}
        </CardContent>
      </Card>
    </div>
  );
}

function PreviewResultsView({ result }: { result: PreviewResult }) {
  const s = result.summary;
  return (
    <div className="space-y-2 pt-2 border-t border-border/30">
      <div className="flex flex-wrap gap-2 text-[11px]">
        <Badge variant="outline">Total {s.total}</Badge>
        <Badge variant="outline">Matching {s.matching}</Badge>
        {s.would_block > 0 ? (
          <Badge variant="secondary">Would block {s.would_block}</Badge>
        ) : null}
        {s.would_allow > 0 ? (
          <Badge variant="destructive">Would allow {s.would_allow}</Badge>
        ) : null}
        {s.threshold_diff > 0 ? (
          <Badge variant="outline">Different action {s.threshold_diff}</Badge>
        ) : null}
      </div>
      <div className="rounded border border-border/40 bg-muted/10 divide-y divide-border/20">
        {result.results.map((r, i) => (
          <div key={i} className="p-2 text-[11px] space-y-1">
            <div className="font-mono break-all">{r.content}</div>
            <div className="flex items-center gap-2 text-muted-foreground">
              <span>active: <span className="font-mono">{r.active_action}</span></span>
              <span>→</span>
              <span>candidate: <span className="font-mono">{r.candidate_action}</span></span>
              {r.divergence ? (
                <Badge
                  variant={
                    r.divergence === "would_allow"
                      ? "destructive"
                      : r.divergence === "would_block"
                        ? "secondary"
                        : "outline"
                  }
                  className="text-[10px] px-1.5 py-0"
                >
                  {r.divergence.replace(/_/g, " ")}
                </Badge>
              ) : (
                <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                  matching
                </Badge>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function ShadowEventsTable({ id }: { id: string }) {
  const { data: events = [], isLoading } = useQuery({
    queryKey: overlayKeys.shadowEvents(id),
    queryFn: () => overlayApi.shadowEvents(id),
  });

  if (isLoading) return <SkeletonRows count={3} />;
  if (!events.length) {
    return (
      <Card className="border-border/50">
        <CardContent className="p-6 text-center text-sm text-muted-foreground">
          No shadow events recorded. Promote a draft to shadow, then send traffic to
          see what would diverge under the candidate overlay.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent border-border/50">
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Divergence
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Active
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Shadow
              </TableHead>
              <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                When
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((e: ShadowEvent) => (
              <TableRow key={e.id} className="border-border/30">
                <TableCell>
                  <Badge
                    variant={
                      e.divergence === "would_allow"
                        ? "destructive"
                        : e.divergence === "would_block"
                          ? "secondary"
                          : "outline"
                    }
                    className="text-[10px] px-1.5 py-0"
                  >
                    {e.divergence.replace(/_/g, " ")}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">{e.active_action}</TableCell>
                <TableCell className="font-mono text-xs">{e.shadow_action}</TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {new Date(e.created_at).toLocaleString()}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function AuditTable({ id }: { id: string }) {
  const { data: entries = [], isLoading } = useQuery({
    queryKey: overlayKeys.audit(id),
    queryFn: () => overlayApi.audit(id),
  });

  if (isLoading) return <SkeletonRows count={3} />;
  if (!entries.length) {
    return (
      <Card className="border-border/50">
        <CardContent className="p-6 text-center text-sm text-muted-foreground">
          No audit entries.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent border-border/50">
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Event
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Actor
              </TableHead>
              <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                Reason
              </TableHead>
              <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                When
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.map((a: AuditEntry) => (
              <TableRow key={a.id} className="border-border/30">
                <TableCell>
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                    {a.event}
                  </Badge>
                </TableCell>
                <TableCell className="text-xs">{a.actor || "—"}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {a.reason || "—"}
                </TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {new Date(a.created_at).toLocaleString()}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
