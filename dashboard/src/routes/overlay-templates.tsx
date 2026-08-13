import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowLeft, ChevronDown, ChevronRight, Sparkles } from "lucide-react";

import {
  overlayApi,
  overlayKeys,
  type OverlaySnapshot,
  type Template,
} from "@/api/overlay";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/card";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";

export function OverlayTemplatesPage() {
  const navigate = useNavigate();
  const [expandedSlug, setExpandedSlug] = useState<string | null>(null);

  const { data: templates = [], isLoading } = useQuery({
    queryKey: overlayKeys.templates(),
    queryFn: overlayApi.templates,
  });
  const summary = useMemo(() => templates.reduce((acc, template) => {
    acc.patterns += template.snapshot.additional_patterns?.length ?? 0;
    acc.accessRules += template.snapshot.additional_access_rules?.length ?? 0;
    acc.overrides += Object.values(template.snapshot.detector_overrides ?? {}).filter(Boolean).length;
    return acc;
  }, { patterns: 0, accessRules: 0, overrides: 0 }), [templates]);

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
        <span>Templates</span>
      </div>

      <AdminPageHeader
        eyebrow="Policy library"
        title="Policy templates"
        description="Start from a reviewed industry baseline, inspect every included rule, and create an editable draft. Templates never activate automatically."
        badge={<Badge variant="outline" className="font-mono text-[10px]">read-only baselines</Badge>}
      />

      <AdminSummaryStrip items={[
        { label: "Templates", value: templates.length, detail: "Available baselines" },
        { label: "Pattern rules", value: summary.patterns, detail: "Across the library" },
        { label: "Access rules", value: summary.accessRules, detail: "Network and identity constraints" },
        { label: "Detector overrides", value: summary.overrides, detail: "Threshold and action changes" },
      ]} />

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {isLoading ? (
          <SkeletonRows count={4} />
        ) : !templates.length ? (
          <EmptyState
            icon={<Sparkles className="h-6 w-6" />}
            title="No templates available"
            description="Built-in templates are seeded by migration 011. Check the DB if this looks wrong."
          />
        ) : (
          templates.map((t) => {
            const expanded = expandedSlug === t.slug;
            return (
              <Card key={t.id} className="border-border/60 transition-colors hover:border-border">
                <CardContent className="space-y-3 p-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <h3 className="font-mono text-sm font-bold tracking-tight">{t.slug}</h3>
                        {t.is_builtin ? (
                          <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                            builtin
                          </Badge>
                        ) : null}
                      </div>
                      <p className="text-[11px] text-muted-foreground">{t.name}</p>
                    </div>
                    <Button
                      size="sm"
                      className="h-8 text-xs shrink-0"
                      onClick={() =>
                        navigate({
                          to: "/overlays/new",
                          search: { template: t.slug, from_threat: undefined },
                        })
                      }
                    >
                      Use template
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">{t.description}</p>
                  <TemplateSummary snapshot={t.snapshot} />
                  <button
                    type="button"
                    onClick={() => setExpandedSlug(expanded ? null : t.slug)}
                    className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
                  >
                    {expanded ? (
                      <ChevronDown className="h-3 w-3" />
                    ) : (
                      <ChevronRight className="h-3 w-3" />
                    )}
                    {expanded ? "Hide rules" : "Preview rules"}
                  </button>
                  {expanded ? <TemplateRules snapshot={t.snapshot} /> : null}
                </CardContent>
              </Card>
            );
          })
        )}
      </div>
    </>
  );
}

function TemplateSummary({ snapshot }: { snapshot: Template["snapshot"] }) {
  const patternCount = snapshot.additional_patterns?.length ?? 0;
  const accessCount = snapshot.additional_access_rules?.length ?? 0;
  const overrideCount = Object.values(snapshot.detector_overrides ?? {}).filter(
    Boolean,
  ).length;
  return (
    <div className="flex flex-wrap gap-1">
      {patternCount > 0 ? (
        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
          +{patternCount} patterns
        </Badge>
      ) : null}
      {accessCount > 0 ? (
        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
          +{accessCount} access rules
        </Badge>
      ) : null}
      {overrideCount > 0 ? (
        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
          {overrideCount} override{overrideCount === 1 ? "" : "s"}
        </Badge>
      ) : null}
    </div>
  );
}

// TemplateRules renders the snapshot content as a readable list:
// patterns, access rules, and detector overrides in plain English
// (no raw JSON). Editors use the full JSON textarea on the new-policy
// page.
function TemplateRules({ snapshot }: { snapshot: OverlaySnapshot }) {
  const patterns = snapshot.additional_patterns ?? [];
  const access = snapshot.additional_access_rules ?? [];
  const overrides = snapshot.detector_overrides ?? {};
  const overrideEntries = Object.entries(overrides).filter(
    ([, v]) => v && Object.keys(v).length > 0,
  );

  return (
    <div className="rounded border border-border/40 bg-muted/20 p-2 space-y-2 text-[11px]">
      {patterns.length > 0 ? (
        <div>
          <p className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] mb-1">
            Additional patterns
          </p>
          <ul className="space-y-1">
            {patterns.map((p, i) => (
              <li key={i} className="font-mono break-all">
                <Badge variant="outline" className="text-[10px] px-1 py-0 mr-1">
                  {p.pattern_type}
                </Badge>
                <span className="text-foreground">{p.name}</span>
                <span className="text-muted-foreground"> → {p.action}</span>{" "}
                <span className="text-muted-foreground/70">({p.severity})</span>
                <div className="pl-4 text-muted-foreground/80">{p.pattern}</div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {access.length > 0 ? (
        <div>
          <p className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] mb-1">
            Additional access rules{" "}
            <span className="normal-case font-normal tracking-normal text-destructive/80">
              (not yet enforced)
            </span>
          </p>
          <ul className="space-y-1 font-mono">
            {access.map((r, i) => (
              <li key={i}>
                <Badge variant="outline" className="text-[10px] px-1 py-0 mr-1">
                  {r.rule_type}
                </Badge>
                {r.value}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {overrideEntries.length > 0 ? (
        <div>
          <p className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] mb-1">
            Detector overrides
          </p>
          <ul className="space-y-1 font-mono">
            {overrideEntries.map(([detector, v]) => {
              const tuple = v as Record<string, unknown>;
              const parts: string[] = [];
              if ("threshold" in tuple && tuple.threshold !== undefined) {
                parts.push(`threshold=${String(tuple.threshold)}`);
              }
              if ("strategy" in tuple && tuple.strategy !== undefined) {
                parts.push(`strategy=${String(tuple.strategy)}`);
              }
              if ("action" in tuple && tuple.action !== undefined) {
                parts.push(`action=${String(tuple.action)}`);
              }
              return (
                <li key={detector}>
                  <span className="text-foreground">{detector}</span>
                  <span className="text-muted-foreground"> → {parts.join(", ") || "—"}</span>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}
      {patterns.length === 0 && access.length === 0 && overrideEntries.length === 0 ? (
        <p className="text-muted-foreground">(Empty snapshot — template has no rules yet.)</p>
      ) : null}
    </div>
  );
}
