import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
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
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AdminPageHeader,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";
import {
  SnapshotEditor,
  type SnapshotEditorView,
} from "@/components/snapshot-editor";

export function OverlayNewPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const search = useSearch({ from: "/overlays/new" });
  const templateSlug = search.template;
  const fromThreatID = search.from_threat;

  const resolvedSlug = templateSlug
    ? TEMPLATE_SLUG_ALIASES[templateSlug] || templateSlug
    : undefined;

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
    if (
      fromApi &&
      fromApi.snapshot &&
      Object.keys(fromApi.snapshot).length > 1
    ) {
      return fromApi;
    }
    return (
      BUILTIN_TEMPLATES[key] ??
      BUILTIN_TEMPLATES[templateSlug || ""] ??
      null
    );
  }, [templatesQuery.data, resolvedSlug, templateSlug]);

  const threatQuery = useQuery({
    queryKey: ["threats", "detail", fromThreatID],
    queryFn: () => api.threats.get(fromThreatID as string),
    enabled: !!fromThreatID,
  });
  const threat = threatQuery.data ?? null;

  const overlaysQuery = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
    enabled: !!fromThreatID,
  });
  const existingOverlays = overlaysQuery.data ?? [];

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [commit, setCommit] = useState("Initial version");
  const initialEmpty = useMemo(
    () => JSON.stringify(emptySnapshot(), null, 2),
    [],
  );
  const [snapshotJSON, setSnapshotJSON] = useState(initialEmpty);
  const [parseError, setParseError] = useState<string | null>(null);
  const [view, setView] = useState<SnapshotEditorView>(
    fromThreatID ? "form" : "json",
  );
  const [didPrefill, setDidPrefill] = useState(false);

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
        <span className="text-foreground">New policy</span>
      </nav>

      <AdminPageHeader
        eyebrow="Policy authoring"
        title="New custom policy"
        description={
          fromThreatID
            ? "Seeded from a flagged threat. Tighten the pattern, confirm the action, then save as a draft — nothing is activated automatically."
            : templateSlug
              ? "Seeded from a built-in template. Review the rules, adjust what you need, then save the policy as a draft."
              : "Define extra patterns, access rules, and detector overrides on top of your core security profile. Starts as a draft — nothing is activated automatically."
        }
        badge={
          <span className="font-mono text-[11px] text-muted-foreground">
            draft only
          </span>
        }
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => navigate({ to: "/overlay-templates" })}
          >
            <Sparkles className="size-3.5" aria-hidden="true" />
            Browse templates
          </Button>
        }
      />

      <div className="mb-6 border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">
          No live traffic changes.
        </span>{" "}
        Creating this policy stores version 1 as a draft. Review it, promote it
        to shadow, and activate only after the observed matches are verified.
      </div>

      {(templateSlug && templatesQuery.isLoading) ||
      (fromThreatID && threatQuery.isLoading) ? (
        <div className="mb-6">
          <SkeletonRows count={3} />
        </div>
      ) : null}

      {templateSlug && !templatesQuery.isLoading && !template ? (
        <div className="mb-6 border-l-2 border-destructive pl-3.5 text-xs text-muted-foreground">
          Template <span className="font-mono text-foreground">{templateSlug}</span>{" "}
          not found. You can still create a policy from scratch below.
        </div>
      ) : null}

      {fromThreatID && !threatQuery.isLoading && !threat ? (
        <div className="mb-6 border-l-2 border-destructive pl-3.5 text-xs text-muted-foreground">
          Threat{" "}
          <span className="font-mono text-foreground">
            {fromThreatID.slice(0, 8)}
          </span>{" "}
          not found. You can still create a policy from scratch below.
        </div>
      ) : null}

      {threat ? (
        <div className="mb-6 space-y-2 border-y border-border-subtle py-3 text-xs">
          <div className="flex items-center gap-2 text-muted-foreground">
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
          {existingOverlays.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2 border-t border-border-subtle pt-2.5 text-muted-foreground">
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
                <SelectTrigger
                  aria-label="Add rule to existing policy"
                  className="h-8 w-52 text-xs"
                >
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
        </div>
      ) : null}

      {template ? (
        <div className="mb-6 flex items-center gap-2 border-y border-border-subtle py-3 text-xs text-muted-foreground">
          <Sparkles className="size-3.5 shrink-0" aria-hidden="true" />
          <span>
            Seeded from template{" "}
            <span className="font-mono font-medium text-foreground">
              {template.slug}
            </span>{" "}
            — {template.name}
          </span>
        </div>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!name || parseError || create.isPending) return;
          create.mutate();
        }}
        className="space-y-6 border-t border-border-subtle pt-5"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <FieldLabel htmlFor="policy-name">Policy name</FieldLabel>
            <Input
              id="policy-name"
              placeholder="strict-pii or consumer-chat"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-8 font-mono text-xs"
            />
          </div>
          <div>
            <FieldLabel htmlFor="policy-description" optional>
              Description
            </FieldLabel>
            <Input
              id="policy-description"
              placeholder="What traffic or compliance rule this policy covers"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="h-8 text-xs"
            />
          </div>
        </div>

        <div>
          <FieldLabel htmlFor="policy-commit">Version commit note</FieldLabel>
          <Input
            id="policy-commit"
            placeholder="Initial version"
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
              "Failed to create policy"}
          </p>
        ) : null}

        <div className="flex justify-end gap-2 border-t border-border-subtle pt-4">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => navigate({ to: "/overlays" })}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            className="h-8 text-xs"
            disabled={!name || !!parseError || create.isPending}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            Create policy
          </Button>
        </div>
      </form>
    </div>
  );
}
