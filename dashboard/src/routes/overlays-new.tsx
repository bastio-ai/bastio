import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowLeft, ChevronRight, Plus, ShieldX, Sparkles } from "lucide-react";

import { api } from "@/api/client";
import {
  BUILTIN_TEMPLATES,
  TEMPLATE_SLUG_ALIASES,
  emptySnapshot,
  overlayApi,
  overlayKeys,
  snapshotFromThreat,
  suggestedPolicyNameFromThreat,
  type OverlaySnapshot,
} from "@/api/overlay";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AdminPageHeader, SecurityNotice } from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";
import {
  SnapshotEditor,
  type SnapshotEditorView,
} from "@/components/snapshot-editor";

// OverlayNewPage is the route-backed create-a-custom-policy page.
// Replaces the inline dialog that lived in overlays.tsx. Two reasons
// the URL-backed approach matters: the browser back button returns
// to the list instead of escaping the app; a refresh preserves the
// user's place.
//
// Optional search params:
//   ?template=<slug>      → prefill from a built-in template, source=template:<slug>
//   ?from_threat=<id>     → prefill a single pattern rule captured
//                           from a flagged threat. When other overlays
//                           exist, the page offers an "add to existing"
//                           shortcut that navigates to the version-new
//                           route preserving from_threat.
export function OverlayNewPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const search = useSearch({ from: "/overlays/new" });
  const templateSlug = search.template;
  const fromThreatID = search.from_threat;

  const resolvedSlug = templateSlug
    ? TEMPLATE_SLUG_ALIASES[templateSlug] || templateSlug
    : undefined;

  // Templates list is cheap — one query, cached by TanStack Query.
  // We only use it when a template slug is in the URL, but requesting
  // it unconditionally lets us show the selected template's name
  // without a second round-trip.
  const templatesQuery = useQuery({
    queryKey: overlayKeys.templates(),
    queryFn: overlayApi.templates,
    enabled: !!templateSlug,
  });

  const template = useMemo(() => {
    if (!resolvedSlug && !templateSlug) return null;
    const key = resolvedSlug || templateSlug || "";
    const fromApi = templatesQuery.data?.find(
      (t) => t.slug === key || t.slug === templateSlug,
    );
    if (fromApi && fromApi.snapshot && Object.keys(fromApi.snapshot).length > 1) {
      return fromApi;
    }
    return (
      BUILTIN_TEMPLATES[key] ??
      BUILTIN_TEMPLATES[templateSlug || ""] ??
      null
    );
  }, [templatesQuery.data, resolvedSlug, templateSlug]);

  // Threat source prefill. Fetched only when from_threat is set.
  const threatQuery = useQuery({
    queryKey: ["threats", "detail", fromThreatID],
    queryFn: () => api.threats.get(fromThreatID as string),
    enabled: !!fromThreatID,
  });
  const threat = threatQuery.data ?? null;

  // List of existing overlays — queried only when we came from a
  // threat, so the page can offer "add to existing policy instead"
  // instead of always forcing a brand-new policy.
  const overlaysQuery = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
    enabled: !!fromThreatID,
  });
  const existingOverlays = overlaysQuery.data ?? [];

  // Form state. Initial JSON is the empty snapshot; updated once the
  // template or threat loads (if one was requested).
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [commit, setCommit] = useState("Initial version");
  const initialEmpty = useMemo(
    () => JSON.stringify(emptySnapshot(), null, 2),
    [],
  );
  const [snapshotJSON, setSnapshotJSON] = useState(initialEmpty);
  const [parseError, setParseError] = useState<string | null>(null);
  // When the user arrives from a threat, default to the Form view so
  // the captured pattern rule is visible without reading JSON.
  const [view, setView] = useState<SnapshotEditorView>(
    fromThreatID ? "form" : "json",
  );
  const [didPrefill, setDidPrefill] = useState(false);

  // Apply the prefill exactly once per navigation so the user can
  // still clear the form afterwards without it resetting. Threat
  // prefill takes precedence over template if somehow both are set.
  useEffect(() => {
    if (didPrefill) return;
    if (threat) {
      setName((prev) => prev || suggestedPolicyNameFromThreat(threat));
      setDescription(
        (prev) =>
          prev ||
          `Captured from flagged ${threat.detector_name} threat on ${new Date(
            threat.detected_at,
          ).toLocaleDateString()}`,
      );
      setCommit(`Captured from threat ${threat.id.slice(0, 8)}`);
      setSnapshotJSON(JSON.stringify(snapshotFromThreat(threat), null, 2));
      setDidPrefill(true);
      return;
    }
    if (template) {
      setName((prev) => prev || template.slug);
      setDescription((prev) => prev || template.description);
      setCommit(`From template: ${template.slug}`);
      setSnapshotJSON(JSON.stringify(template.snapshot, null, 2));
      setDidPrefill(true);
    }
  }, [threat, template, didPrefill]);

  const create = useMutation({
    mutationFn: async () => {
      let snapshot: OverlaySnapshot;
      try {
        snapshot = JSON.parse(snapshotJSON) as OverlaySnapshot;
      } catch (e) {
        throw new Error(`Invalid JSON: ${(e as Error).message}`);
      }
      return overlayApi.create({
        name,
        description,
        snapshot,
        commit_message: commit,
        // Provenance: record where this overlay came from so audit
        // captures it. Threat capture trumps template when both set.
        source: fromThreatID
          ? `threat:${fromThreatID}`
          : templateSlug
            ? `template:${templateSlug}`
            : "manual",
      });
    },
    onSuccess: (result) => {
      qc.invalidateQueries({ queryKey: overlayKeys.list() });
      navigate({ to: "/overlays/$id", params: { id: result.overlay.id } });
    },
  });

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
        <span>New policy</span>
      </div>

      <AdminPageHeader
        eyebrow="Policy authoring"
        title="New custom policy"
        description={
          fromThreatID
            ? "Seeded from a flagged threat. Tighten the pattern, confirm the action, then save as a draft — nothing is activated automatically."
            : templateSlug
              ? "Seeded from a built-in template. Review the rules, tweak anything you like, then create the policy as a draft."
              : "Define extra patterns, access rules, and detector overrides on top of your core security profile. Starts as a draft — nothing is activated automatically."
        }
        badge={<span className="rounded-full border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] font-medium text-muted-foreground">Draft only</span>}
        actions={<Button variant="outline" size="sm" className="h-8 text-xs" onClick={() => navigate({ to: "/overlay-templates" })}><Sparkles className="size-3.5" /> Browse templates</Button>}
      />

      <SecurityNotice title="No live traffic changes" className="mb-4">
        Creating this policy stores version 1 as a draft. Review it, promote it to shadow, and activate only after the observed matches are correct.
      </SecurityNotice>

      {(templateSlug && templatesQuery.isLoading) ||
      (fromThreatID && threatQuery.isLoading) ? (
        <div className="mt-4">
          <SkeletonRows count={3} />
        </div>
      ) : null}

      {templateSlug && !templatesQuery.isLoading && !template ? (
        <Card className="border-destructive/40 bg-destructive/5 mt-4">
          <CardContent className="p-3 text-xs">
            Template <span className="font-mono">{templateSlug}</span> not found.
            You can still create a policy from scratch below.
          </CardContent>
        </Card>
      ) : null}

      {fromThreatID && !threatQuery.isLoading && !threat ? (
        <Card className="border-destructive/40 bg-destructive/5 mt-4">
          <CardContent className="p-3 text-xs">
            Threat <span className="font-mono">{fromThreatID.slice(0, 8)}</span>{" "}
            not found. You can still create a policy from scratch below.
          </CardContent>
        </Card>
      ) : null}

      {threat ? (
        <Card className="border-border/50 mt-4">
          <CardContent className="p-3 text-xs space-y-2">
            <div className="flex items-center gap-2">
              <ShieldX className="h-3 w-3 text-muted-foreground" />
              <span>
                Captured from flagged{" "}
                <span className="font-mono font-semibold">
                  {threat.detector_name}
                </span>{" "}
                threat ({threat.threat_type}, {threat.severity}) —{" "}
                <span className="font-mono">{threat.id.slice(0, 8)}</span>
              </span>
            </div>
            {existingOverlays.length > 0 ? (
              <div className="flex flex-wrap items-center gap-2 border-t border-border/40 pt-2 text-muted-foreground">
                <span>Or add this rule to an existing policy:</span>
                <Select
                  onValueChange={(id: string | null) => {
                    if (!id) return;
                    navigate({
                      to: "/overlays/$id/versions/new",
                      params: { id },
                      search: { from_threat: threat.id },
                    });
                  }}
                >
                  <SelectTrigger className="h-7 w-48 text-xs">
                    <SelectValue placeholder="Pick a policy…" />
                  </SelectTrigger>
                  <SelectContent>
                    {existingOverlays.map((o) => (
                      <SelectItem key={o.id} value={o.id} className="text-xs">
                        {o.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {template ? (
        <Card className="border-border/50 mt-4">
          <CardContent className="p-3 text-xs flex items-center gap-2">
            <Sparkles className="h-3 w-3 text-muted-foreground" />
            Seeded from template{" "}
            <span className="font-mono font-semibold">{template.slug}</span>
            <span className="text-muted-foreground"> — {template.name}</span>
          </CardContent>
        </Card>
      ) : null}

      <Card className="mt-4 border-border/70">
        <CardContent className="space-y-3 p-4">
          <div className="grid grid-cols-2 gap-2">
            <Input
              placeholder="name (e.g. strict-pii, consumer-chat)"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-8 text-xs"
            />
            <Input
              placeholder="description (optional)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="h-8 text-xs"
            />
          </div>
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
              onClick={() => navigate({ to: "/overlays" })}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              className="h-8 text-xs"
              onClick={() => create.mutate()}
              disabled={!name || !!parseError || create.isPending}
            >
              <Plus className="h-3 w-3" /> Create
            </Button>
          </div>
          {create.isError ? (
            <p className="text-xs text-destructive">
              {(create.error as Error | undefined)?.message ??
                "Failed to create policy"}
            </p>
          ) : null}
        </CardContent>
      </Card>
    </>
  );
}
