import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { ArrowLeft, ChevronRight, Plus, ShieldX } from "lucide-react";

import { api } from "@/api/client";
import {
  overlayApi,
  overlayKeys,
  snapshotFromThreat,
  type OverlaySnapshot,
} from "@/api/overlay";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { AdminPageHeader, SecurityNotice } from "@/components/admin/admin-primitives";
import {
  SnapshotEditor,
  type SnapshotEditorView,
} from "@/components/snapshot-editor";

// OverlayVersionNewPage is the route-backed create-a-draft-version
// page. Replaces the inline NewVersionForm that was toggled by local
// state on the detail page — back button and refresh now behave.
//
// Optional ?from_threat=<id>: seeds the draft snapshot with a single
// pattern rule derived from a flagged threat. Used by the "Add to
// existing policy" shortcut on the new-policy page and by the Capture
// button on the threat detail page once it targets a specific policy.
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
    <>
      <div className="flex items-center gap-2 text-xs text-muted-foreground mb-2">
        <button
          className="inline-flex items-center hover:text-foreground"
          onClick={() => navigate({ to: "/overlays" })}
        >
          <ArrowLeft className="h-3 w-3" /> Custom Policies
        </button>
        <ChevronRight className="h-3 w-3" />
        <button
          className="hover:text-foreground font-mono"
          onClick={() => navigate({ to: "/overlays/$id", params: { id } })}
        >
          {overlay?.name ?? id.slice(0, 8)}
        </button>
        <ChevronRight className="h-3 w-3" />
        <span>New version</span>
      </div>

      <AdminPageHeader
        eyebrow="Policy versioning"
        title="New draft version"
        description={
          fromThreatID
            ? "Seeded from a flagged threat. Tighten the pattern, confirm the action, save as a draft — drafts don't affect traffic until you promote them."
            : "Append a draft version to this policy. Drafts don't affect traffic — promote to shadow to observe, or activate to enforce."
        }
        badge={<span className="rounded-full border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] font-medium text-muted-foreground">Not enforced</span>}
      />

      <SecurityNotice title="Safe rollout path" className="mb-4">
        This version is isolated from live traffic until you deliberately promote it to shadow or active enforcement.
      </SecurityNotice>

      {threat ? (
        <Card className="border-border/50 mt-4">
          <CardContent className="p-3 text-xs flex items-center gap-2">
            <ShieldX className="h-3 w-3 text-muted-foreground" />
            Captured from flagged{" "}
            <span className="font-mono font-semibold">
              {threat.detector_name}
            </span>{" "}
            threat ({threat.threat_type}, {threat.severity}) —{" "}
            <span className="font-mono">{threat.id.slice(0, 8)}</span>
          </CardContent>
        </Card>
      ) : null}

      <Card className="mt-4 border-border/70">
        <CardContent className="space-y-3 p-4">
          <Input
            placeholder="commit message"
            value={commit}
            onChange={(e) => setCommit(e.target.value)}
            className="h-8 text-xs"
          />
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
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() =>
                navigate({ to: "/overlays/$id", params: { id } })
              }
            >
              Cancel
            </Button>
            <Button
              size="sm"
              className="h-8 text-xs"
              onClick={() => create.mutate()}
              disabled={!!parseError || create.isPending}
            >
              <Plus className="h-3 w-3" /> Save draft
            </Button>
          </div>
          {create.isError ? (
            <p className="text-xs text-destructive">
              {(create.error as Error | undefined)?.message ??
                "Failed to create version"}
            </p>
          ) : null}
        </CardContent>
      </Card>
    </>
  );
}
