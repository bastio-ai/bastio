import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { ArrowLeft, ChevronRight, Plus, ShieldX } from "lucide-react";

import { api } from "@/api/client";
import {
  overlayApi,
  overlayKeys,
  snapshotFromThreat,
  type OverlaySnapshot,
} from "@/api/overlay";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  AdminPageHeader,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import {
  SnapshotEditor,
  type SnapshotEditorView,
} from "@/components/snapshot-editor";

export function OverlayVersionNewPage() {
  const { id } = useParams({ from: "/overlays/$id/versions/new" });
  const search = useSearch({ from: "/overlays/$id/versions/new" });
  const fromThreatID = search.from_threat;
  const navigate = useNavigate();
  const qc = useQueryClient();

  const overlayQuery = useQuery({
    queryKey: overlayKeys.detail(id),
    queryFn: () => overlayApi.get(id),
  });

  const threatQuery = useQuery({
    queryKey: ["threats", "detail", fromThreatID],
    queryFn: () => api.threats.get(fromThreatID as string),
    enabled: !!fromThreatID,
  });
  const threat = threatQuery.data ?? null;

  const [commit, setCommit] = useState("");
  const initial = useMemo(
    () => JSON.stringify({ schema_version: 1 }, null, 2),
    [],
  );
  const [snapshotJSON, setSnapshotJSON] = useState(initial);
  const [parseError, setParseError] = useState<string | null>(null);
  const [view, setView] = useState<SnapshotEditorView>(
    fromThreatID ? "form" : "json",
  );
  const [didPrefill, setDidPrefill] = useState(false);

  useEffect(() => {
    if (didPrefill || !threat) return;
    setCommit(`Captured from threat ${threat.id.slice(0, 8)}`);
    setSnapshotJSON(JSON.stringify(snapshotFromThreat(threat), null, 2));
    setDidPrefill(true);
  }, [threat, didPrefill]);

  const create = useMutation({
    mutationFn: async () => {
      let snapshot: OverlaySnapshot;
      try {
        snapshot = JSON.parse(snapshotJSON) as OverlaySnapshot;
      } catch (e) {
        throw new Error(`Invalid JSON: ${(e as Error).message}`);
      }
      return overlayApi.createVersion(id, {
        snapshot,
        commit_message: commit,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: overlayKeys.versions(id) });
      navigate({ to: "/overlays/$id", params: { id } });
    },
  });

  const overlay = overlayQuery.data?.overlay;

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
        <Link
          to="/overlays/$id"
          params={{ id }}
          className="font-mono hover:text-foreground"
        >
          {overlay?.name ?? id.slice(0, 8)}
        </Link>
        <ChevronRight className="size-3" aria-hidden="true" />
        <span className="text-foreground">New version</span>
      </nav>

      <AdminPageHeader
        eyebrow="Policy versioning"
        title="New draft version"
        description={
          fromThreatID
            ? "Seeded from a flagged threat. Tighten the pattern, confirm the action, and save as a draft — drafts do not affect traffic until you promote them."
            : "Append a draft version to this policy. Drafts do not affect traffic — promote to shadow to observe, or activate to enforce."
        }
        badge={
          <span className="font-mono text-[11px] text-muted-foreground">
            not enforced
          </span>
        }
      />

      <div className="mb-6 border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">Safe rollout path.</span>{" "}
        This version is isolated from live traffic until you deliberately
        promote it to shadow or active enforcement.
      </div>

      {threat ? (
        <div className="mb-6 flex items-center gap-2 border-y border-border-subtle py-3 text-xs text-muted-foreground">
          <ShieldX className="size-3.5 shrink-0" aria-hidden="true" />
          <span>
            Captured from flagged{" "}
            <span className="font-mono font-medium text-foreground">
              {threat.detector_name}
            </span>{" "}
            threat ({threat.threat_type}, {threat.severity}) —{" "}
            <span className="font-mono">{threat.id.slice(0, 8)}</span>
          </span>
        </div>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (parseError || create.isPending) return;
          create.mutate();
        }}
        className="space-y-6 border-t border-border-subtle pt-5"
      >
        <div>
          <FieldLabel htmlFor="version-commit" optional>
            Commit message
          </FieldLabel>
          <Input
            id="version-commit"
            placeholder="Describe what changed in this version"
            value={commit}
            onChange={(e) => setCommit(e.target.value)}
            className="h-8 text-xs"
          />
        </div>

        <SnapshotEditor
          value={snapshotJSON}
          onChange={(next, err) => {
            setSnapshotJSON(next);
            setParseError(err);
          }}
          view={view}
          onViewChange={setView}
          parseError={parseError}
        />

        {parseError ? (
          <p className="text-xs text-destructive">
            JSON parse error: {parseError}
          </p>
        ) : null}

        {create.isError ? (
          <p className="text-xs text-destructive">
            {(create.error as Error | undefined)?.message ??
              "Failed to create version"}
          </p>
        ) : null}

        <div className="flex justify-end gap-2 border-t border-border-subtle pt-4">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => navigate({ to: "/overlays/$id", params: { id } })}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            className="h-8 text-xs"
            disabled={!!parseError || create.isPending}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            Save draft
          </Button>
        </div>
      </form>
    </div>
  );
}
