import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowLeft,
  ChevronRight,
  Eye,
  GitCompare,
  Layers3,
  Plus,
  Trash2,
  Undo2,
} from "lucide-react";

import {
  overlayApi,
  overlayKeys,
  type OverlayVersion,
  type OverlayWarning,
  type PreviewResult,
  type ShadowEvent,
  type AuditEntry,
} from "@/api/overlay";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  AdminUnderlineNav,
} from "@/components/admin/admin-primitives";
import { cn } from "@/lib/utils";

type Tab = "versions" | "shadow-events" | "audit";

export function OverlayDetailPage() {
  const { id } = useParams({ from: "/overlays/$id" });
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [tab, setTab] = useState<Tab>("versions");
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);

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
      setConfirmDeleteOpen(false);
      qc.invalidateQueries({ queryKey: overlayKeys.list() });
      navigate({ to: "/overlays" });
    },
  });

  const promoteShadow = useMutation({
    mutationFn: (n: number) =>
      overlayApi.promoteShadow(id, n, "promote from dashboard"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
    },
  });

  const activate = useMutation({
    mutationFn: (n: number) =>
      overlayApi.activate(id, n, "activate from dashboard"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.detail(id) });
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
    },
  });

  const overlay = overlayQuery.data?.overlay;
  const activeVersion = overlayQuery.data?.active_version;
  const activeWarnings = overlayQuery.data?.active_warnings ?? [];

  if (overlayQuery.isLoading) {
    return (
      <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
        <SkeletonRows count={5} />
      </div>
    );
  }

  if (overlayQuery.isError || !overlay) {
    return (
      <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
        <div className="border-y border-border-subtle">
          <EmptyState
            icon={<Layers3 className="size-5" aria-hidden="true" />}
            title="Policy not found"
            description="This custom policy may have been deleted or belongs to another workspace."
            action={
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => navigate({ to: "/overlays" })}
              >
                Back to custom policies
              </Button>
            }
          />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <nav
        aria-label="Breadcrumb"
        className="mb-3 flex items-center gap-1.5 text-xs text-muted-foreground"
      >
        <Link
          to="/overlays"
          className="inline-flex items-center gap-1 hover:text-foreground"
        >
          <ArrowLeft className="size-3" aria-hidden="true" />
          Custom policies
        </Link>
        <ChevronRight className="size-3" aria-hidden="true" />
        <span className="font-mono text-foreground">{overlay.name}</span>
      </nav>

      <AdminPageHeader
        eyebrow="Custom policy"
        title={<span className="font-mono">{overlay.name}</span>}
        description={
          overlay.description || "No policy description has been provided."
        }
        bordered={false}
        className="mb-4"
        badge={
          <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 rounded-full",
                activeVersion ? "bg-success" : "bg-muted-foreground/50",
              )}
            />
            {activeVersion
              ? `active v${activeVersion.version}`
              : "no active version"}
          </span>
        }
        actions={
          <>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              disabled={rollback.isPending}
              onClick={() => rollback.mutate()}
            >
              <Undo2 className="size-3.5" aria-hidden="true" />
              Rollback
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 text-xs text-destructive"
              disabled={remove.isPending}
              onClick={() => setConfirmDeleteOpen(true)}
            >
              <Trash2 className="size-3.5" aria-hidden="true" />
              Delete
            </Button>
            <Button
              type="button"
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
              <Plus className="size-3.5" aria-hidden="true" />
              New version
            </Button>
          </>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Versions",
            value: versionsQuery.data?.length ?? 0,
            detail: "Immutable history",
          },
          {
            label: "Active version",
            value: activeVersion ? `v${activeVersion.version}` : "None",
            detail: activeVersion?.commit_message || "Core profile only",
            tone: activeVersion ? "success" : "warning",
          },
          {
            label: "Draft versions",
            value: (versionsQuery.data ?? []).filter(
              (version) => version.state === "draft",
            ).length,
            detail: "Not applied to traffic",
          },
          {
            label: "Security warnings",
            value: activeWarnings.length,
            detail: activeWarnings.length
              ? "Active version loosens defaults"
              : "No weakened defaults",
            tone: activeWarnings.length ? "danger" : "success",
          },
        ]}
      />

      {activeWarnings.length > 0 ? (
        <WarningBanner warnings={activeWarnings} />
      ) : null}

      <AdminUnderlineNav
        ariaLabel="Custom policy sections"
        value={tab}
        onChange={setTab}
        items={[
          {
            id: "versions",
            label: "Versions",
            count: versionsQuery.data?.length ?? 0,
          },
          { id: "shadow-events", label: "Shadow events" },
          { id: "audit", label: "Audit" },
        ]}
      />

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

      <Dialog open={confirmDeleteOpen} onOpenChange={setConfirmDeleteOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Delete policy “{overlay.name}”?</DialogTitle>
            <DialogDescription>
              {overlay.active_version_id
                ? "This policy is currently enforcing on live traffic. Deleting it immediately reverts this scope to the core security profile and permanently removes all versions, shadow events, and audit history."
                : "All versions, shadow events, and audit history for this policy will be permanently removed. This cannot be undone."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => setConfirmDeleteOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              className="h-8 text-xs"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              {remove.isPending ? "Deleting…" : "Delete policy"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function WarningBanner({ warnings }: { warnings: OverlayWarning[] }) {
  return (
    <div className="mb-6 space-y-1.5 border-l-2 border-destructive pl-3.5 text-xs">
      <p className="font-medium text-destructive">
        Active version loosens security defaults
      </p>
      <ul className="space-y-1 text-muted-foreground">
        {warnings.map((w, i) => (
          <li key={i}>
            <span className="font-mono text-foreground">
              {w.detector}.{w.field}
            </span>
            : <span>{w.from}</span> →{" "}
            <span className="font-medium text-foreground">{w.to}</span> —{" "}
            {w.message}
          </li>
        ))}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        Rolling back to a prior version reverts these overrides immediately.
      </p>
    </div>
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

  if (loading) {
    return (
      <div className="border-y border-border-subtle py-4">
        <SkeletonRows count={3} />
      </div>
    );
  }

  if (!versions.length) {
    return (
      <div className="border-y border-border-subtle py-8 text-center text-xs text-muted-foreground">
        No versions yet. Create one from the button above.
      </div>
    );
  }

  return (
    <div className="border-y border-border-subtle">
      <div
        className="overflow-x-auto"
        tabIndex={0}
        role="region"
        aria-label="Policy versions"
      >
        <table className="w-full min-w-[720px] text-left text-xs">
          <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
            <tr>
              <th scope="col" className="py-2.5 pr-4 font-normal">
                Version
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                State
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Commit
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Author
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Created
              </th>
              <th scope="col" className="py-2.5 pl-4 text-right font-normal">
                Actions
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border-subtle">
            {versions.map((v) => (
              <tr
                key={v.id}
                className="transition-colors hover:bg-surface-1/60"
              >
                <td className="py-3 pr-4 font-mono text-xs text-foreground">
                  v{v.version}
                </td>
                <td className="px-4 py-3">
                  <span className="inline-flex items-center gap-2 font-mono text-[11px]">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 rounded-full",
                        v.state === "active"
                          ? "bg-success"
                          : v.state === "shadow"
                            ? "bg-warn"
                            : "bg-muted-foreground/50",
                      )}
                    />
                    <span
                      className={cn(
                        v.state === "active"
                          ? "text-foreground"
                          : "text-muted-foreground",
                      )}
                    >
                      {v.state}
                    </span>
                  </span>
                </td>
                <td className="px-4 py-3 text-xs text-foreground">
                  {v.commit_message || "—"}
                </td>
                <td className="px-4 py-3 font-mono text-[11px] text-muted-foreground">
                  {v.created_by || "—"}
                </td>
                <td className="px-4 py-3 font-mono text-[11px] tabular-nums text-muted-foreground">
                  {new Date(v.created_at).toLocaleString()}
                </td>
                <td className="py-3 pl-4 text-right">
                  <div className="flex justify-end gap-1.5">
                    {activeVersion && activeVersion.id !== v.id ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        className="h-7 text-[11px]"
                        onClick={() => setDiffVersion(v)}
                      >
                        <GitCompare className="size-3" aria-hidden="true" />
                        Diff
                      </Button>
                    ) : null}
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="h-7 text-[11px]"
                      onClick={() => setPreviewVersion(v.version)}
                    >
                      <Eye className="size-3" aria-hidden="true" />
                      Preview
                    </Button>
                    {v.state === "draft" ? (
                      <>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          className="h-7 text-[11px]"
                          onClick={() => onPromoteShadow(v.version)}
                        >
                          Shadow
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          className="h-7 text-[11px]"
                          onClick={() => onActivate(v.version)}
                        >
                          Activate
                        </Button>
                      </>
                    ) : v.state === "shadow" ? (
                      <Button
                        type="button"
                        size="sm"
                        className="h-7 text-[11px]"
                        onClick={() => onActivate(v.version)}
                      >
                        Promote to active
                      </Button>
                    ) : null}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

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
    </div>
  );
}

function DiffDialog({
  active,
  candidate,
  onClose,
}: {
  active: OverlayVersion;
  candidate: OverlayVersion;
  onClose: () => void;
}) {
  const activeText = useMemo(
    () => stableStringify(active.snapshot),
    [active.snapshot],
  );
  const candidateText = useMemo(
    () => stableStringify(candidate.snapshot),
    [candidate.snapshot],
  );
  const rows = useMemo(
    () => diffLines(activeText.split("\n"), candidateText.split("\n")),
    [activeText, candidateText],
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">
            Diff — active (v{active.version}) → candidate (v{candidate.version})
          </DialogTitle>
          <DialogDescription className="text-xs">
            Lines present only in the active version show with{" "}
            <code className="font-mono">-</code>; lines added by v
            {candidate.version} show with <code className="font-mono">+</code>.
          </DialogDescription>
        </DialogHeader>
        <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface-1 p-3 font-mono text-[11px] leading-snug">
          {rows.map((r, i) => (
            <div
              key={i}
              className={
                r.kind === "add"
                  ? "bg-success/10 text-success"
                  : r.kind === "del"
                    ? "bg-destructive/10 text-destructive"
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
        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  );
}

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

type DiffRow = { kind: "same" | "add" | "del"; text: string };

function diffLines(oldLines: string[], newLines: string[]): DiffRow[] {
  const m = oldLines.length;
  const n = newLines.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () =>
    new Array(n + 1).fill(0),
  );
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
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">
            Preview impact — v{version}
          </DialogTitle>
          <DialogDescription className="text-xs">
            Paste one sample per line. Each line is scanned through both the
            current active profile and v{version}&apos;s effective profile;
            divergences are highlighted.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label htmlFor="preview-samples-textarea" className="sr-only">
            Sample inputs (one per line)
          </label>
          <textarea
            id="preview-samples-textarea"
            placeholder="ignore all previous instructions..."
            value={samplesText}
            onChange={(e) => setSamplesText(e.target.value)}
            rows={6}
            spellCheck={false}
            className="w-full rounded-md border border-border-subtle bg-surface-1 p-3 font-mono text-[11px] leading-relaxed text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          {preview.isError ? (
            <p className="text-xs text-destructive">
              {(preview.error as Error | undefined)?.message ??
                "Preview failed"}
            </p>
          ) : null}
          {result ? <PreviewResultsView result={result} /> : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={onClose}
          >
            Close
          </Button>
          <Button
            type="button"
            size="sm"
            className="h-8 text-xs"
            onClick={() => preview.mutate()}
            disabled={preview.isPending || !samplesText.trim()}
          >
            {preview.isPending ? "Running…" : "Run preview"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PreviewResultsView({ result }: { result: PreviewResult }) {
  const s = result.summary;
  return (
    <div className="space-y-2.5 border-t border-border-subtle pt-3">
      <div className="flex flex-wrap gap-3 font-mono text-[11px] tabular-nums text-muted-foreground">
        <span>Total: {s.total}</span>
        <span>Matching: {s.matching}</span>
        {s.would_block > 0 ? (
          <span className="text-foreground">Would block: {s.would_block}</span>
        ) : null}
        {s.would_allow > 0 ? (
          <span className="text-destructive">Would allow: {s.would_allow}</span>
        ) : null}
        {s.threshold_diff > 0 ? (
          <span>Different action: {s.threshold_diff}</span>
        ) : null}
      </div>
      <div className="divide-y divide-border-subtle border-y border-border-subtle">
        {result.results.map((r, i) => (
          <div key={i} className="space-y-1 py-2.5 text-[11px]">
            <div className="font-mono break-all text-foreground">
              {r.content}
            </div>
            <div className="flex flex-wrap items-center gap-2 text-muted-foreground">
              <span>
                active:{" "}
                <span className="font-mono text-foreground">
                  {r.active_action}
                </span>
              </span>
              <span>→</span>
              <span>
                candidate:{" "}
                <span className="font-mono text-foreground">
                  {r.candidate_action}
                </span>
              </span>
              <span
                className={cn(
                  "font-mono",
                  r.divergence === "would_allow"
                    ? "text-destructive"
                    : r.divergence
                      ? "text-warn"
                      : "text-muted-foreground",
                )}
              >
                ({r.divergence ? r.divergence.replace(/_/g, " ") : "matching"})
              </span>
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

  if (isLoading) {
    return (
      <div className="border-y border-border-subtle py-4">
        <SkeletonRows count={3} />
      </div>
    );
  }

  if (!events.length) {
    return (
      <div className="border-y border-border-subtle py-8 text-center text-xs text-muted-foreground">
        No shadow events recorded. Promote a draft to shadow, then send traffic
        to inspect divergences under the candidate policy.
      </div>
    );
  }

  return (
    <div className="border-y border-border-subtle">
      <div
        className="overflow-x-auto"
        tabIndex={0}
        role="region"
        aria-label="Shadow divergence events"
      >
        <table className="w-full min-w-[600px] text-left text-xs">
          <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
            <tr>
              <th scope="col" className="py-2.5 pr-4 font-normal">
                Divergence
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Active
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Shadow
              </th>
              <th scope="col" className="py-2.5 pl-4 text-right font-normal">
                When
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border-subtle">
            {events.map((e: ShadowEvent) => (
              <tr
                key={e.id}
                className="transition-colors hover:bg-surface-1/60"
              >
                <td className="py-3 pr-4 font-mono text-[11px]">
                  <span
                    className={cn(
                      e.divergence === "would_allow"
                        ? "text-destructive"
                        : e.divergence === "would_block"
                          ? "text-warn"
                          : "text-foreground",
                    )}
                  >
                    {e.divergence.replace(/_/g, " ")}
                  </span>
                </td>
                <td className="px-4 py-3 font-mono text-xs text-foreground">
                  {e.active_action}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-foreground">
                  {e.shadow_action}
                </td>
                <td className="py-3 pl-4 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                  {new Date(e.created_at).toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function AuditTable({ id }: { id: string }) {
  const { data: entries = [], isLoading } = useQuery({
    queryKey: overlayKeys.audit(id),
    queryFn: () => overlayApi.audit(id),
  });

  if (isLoading) {
    return (
      <div className="border-y border-border-subtle py-4">
        <SkeletonRows count={3} />
      </div>
    );
  }

  if (!entries.length) {
    return (
      <div className="border-y border-border-subtle py-8 text-center text-xs text-muted-foreground">
        No audit entries recorded for this policy.
      </div>
    );
  }

  return (
    <div className="border-y border-border-subtle">
      <div
        className="overflow-x-auto"
        tabIndex={0}
        role="region"
        aria-label="Policy audit history"
      >
        <table className="w-full min-w-[600px] text-left text-xs">
          <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
            <tr>
              <th scope="col" className="py-2.5 pr-4 font-normal">
                Event
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Actor
              </th>
              <th scope="col" className="px-4 py-2.5 font-normal">
                Reason
              </th>
              <th scope="col" className="py-2.5 pl-4 text-right font-normal">
                When
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border-subtle">
            {entries.map((a: AuditEntry) => (
              <tr
                key={a.id}
                className="transition-colors hover:bg-surface-1/60"
              >
                <td className="py-3 pr-4 font-mono text-[11px] text-foreground">
                  {a.event}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-foreground">
                  {a.actor || "—"}
                </td>
                <td className="px-4 py-3 text-xs text-muted-foreground">
                  {a.reason || "—"}
                </td>
                <td className="py-3 pl-4 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                  {new Date(a.created_at).toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
