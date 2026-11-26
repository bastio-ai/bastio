import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, ChevronDown, ChevronRight, Sparkles } from "lucide-react";

import {
  overlayApi,
  overlayKeys,
  type OverlaySnapshot,
  type Template,
} from "@/api/overlay";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/card";
import {
  AdminPageHeader,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";

export function OverlayTemplatesPage() {
  const navigate = useNavigate();
  const [expandedSlug, setExpandedSlug] = useState<string | null>(null);

  const { data: templates = [], isLoading } = useQuery({
    queryKey: overlayKeys.templates(),
    queryFn: overlayApi.templates,
  });

  const summary = useMemo(
    () =>
      templates.reduce(
        (acc, template) => {
          acc.patterns += template.snapshot.additional_patterns?.length ?? 0;
          acc.accessRules +=
            template.snapshot.additional_access_rules?.length ?? 0;
          acc.overrides += Object.values(
            template.snapshot.detector_overrides ?? {},
          ).filter(Boolean).length;
          return acc;
        },
        { patterns: 0, accessRules: 0, overrides: 0 },
      ),
    [templates],
  );

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
        <span className="text-foreground">Templates</span>
      </nav>

      <AdminPageHeader
        eyebrow="Policy library"
        title="Policy templates"
        description="Start from a reviewed industry baseline, inspect every included rule, and create an editable draft. Templates never activate automatically."
        bordered={false}
        className="mb-4"
        badge={
          <span className="font-mono text-[11px] text-muted-foreground">
            read-only baselines
          </span>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Templates",
            value: templates.length,
            detail: "Available baselines",
          },
          {
            label: "Pattern rules",
            value: summary.patterns,
            detail: "Across the library",
          },
          {
            label: "Access rules",
            value: summary.accessRules,
            detail: "Network and identity constraints",
          },
          {
            label: "Detector overrides",
            value: summary.overrides,
            detail: "Threshold and action changes",
          },
        ]}
      />

      <div className="border-y border-border-subtle">
        {isLoading ? (
          <div className="py-4">
            <SkeletonRows count={4} />
          </div>
        ) : !templates.length ? (
          <EmptyState
            icon={<Sparkles className="size-5" />}
            title="No templates available"
            description="Built-in policy baselines have not been loaded for this workspace."
          />
        ) : (
          <div className="divide-y divide-border-subtle">
            {templates.map((t) => {
              const expanded = expandedSlug === t.slug;
              return (
                <article
                  key={t.id}
                  className="py-4 transition-colors"
                >
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-[13px] font-medium text-foreground">
                          {t.name}
                        </h2>
                        <span className="font-mono text-[11px] text-muted-foreground">
                          {t.slug}
                        </span>
                        {t.is_builtin ? (
                          <span className="rounded border border-border-subtle bg-surface-1 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                            builtin
                          </span>
                        ) : null}
                      </div>
                      <p className="mt-1 max-w-3xl text-xs leading-relaxed text-muted-foreground">
                        {t.description}
                      </p>
                      <div className="mt-2.5 flex flex-wrap items-center gap-4">
                        <TemplateSummary snapshot={t.snapshot} />
                        <button
                          type="button"
                          aria-expanded={expanded}
                          onClick={() =>
                            setExpandedSlug(expanded ? null : t.slug)
                          }
                          className="inline-flex cursor-pointer items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          {expanded ? (
                            <ChevronDown
                              className="size-3"
                              aria-hidden="true"
                            />
                          ) : (
                            <ChevronRight
                              className="size-3"
                              aria-hidden="true"
                            />
                          )}
                          {expanded ? "Hide rules" : "Preview rules"}
                        </button>
                      </div>
                    </div>

                    <div className="shrink-0">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-8 text-xs"
                        onClick={() =>
                          navigate({
                            to: "/overlays/new",
                            search: {
                              template: t.slug,
                              from_threat: undefined,
                            },
                          })
                        }
                      >
                        Use template
                      </Button>
                    </div>
                  </div>

                  {expanded ? (
                    <div className="mt-3">
                      <TemplateRules snapshot={t.snapshot} />
                    </div>
                  ) : null}
                </article>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function TemplateSummary({ snapshot }: { snapshot: Template["snapshot"] }) {
  const patternCount = snapshot.additional_patterns?.length ?? 0;
  const accessCount = snapshot.additional_access_rules?.length ?? 0;
  const overrideCount = Object.values(snapshot.detector_overrides ?? {}).filter(
    Boolean,
  ).length;
  return (
    <div className="flex flex-wrap items-center gap-3 font-mono text-[11px] tabular-nums text-muted-foreground">
      {patternCount > 0 ? <span>+{patternCount} patterns</span> : null}
      {accessCount > 0 ? <span>+{accessCount} access rules</span> : null}
      {overrideCount > 0 ? (
        <span>
          {overrideCount} override{overrideCount === 1 ? "" : "s"}
        </span>
      ) : null}
    </div>
  );
}

function TemplateRules({ snapshot }: { snapshot: OverlaySnapshot }) {
  const patterns = snapshot.additional_patterns ?? [];
  const access = snapshot.additional_access_rules ?? [];
  const overrides = snapshot.detector_overrides ?? {};
  const overrideEntries = Object.entries(overrides).filter(
    ([, v]) => v && Object.keys(v).length > 0,
  );

  return (
    <div className="space-y-3 border-l-2 border-border-subtle bg-surface-1/50 px-3.5 py-3 text-xs">
      {patterns.length > 0 ? (
        <div>
          <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">
            Additional patterns
          </p>
          <ul className="space-y-1.5">
            {patterns.map((p, i) => (
              <li key={i} className="font-mono text-[11px] break-all">
                <span className="mr-1.5 rounded border border-border-subtle bg-background px-1.5 py-0.5 text-[10px] text-muted-foreground">
                  {p.pattern_type}
                </span>
                <span className="text-foreground">{p.name}</span>
                <span className="text-muted-foreground"> → {p.action}</span>{" "}
                <span className="text-muted-foreground/70">({p.severity})</span>
                <div className="mt-0.5 pl-4 text-muted-foreground">
                  {p.pattern}
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {access.length > 0 ? (
        <div>
          <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">
            Additional access rules{" "}
            <span className="text-destructive/80">(not yet enforced)</span>
          </p>
          <ul className="space-y-1 font-mono text-[11px]">
            {access.map((r, i) => (
              <li key={i}>
                <span className="mr-1.5 rounded border border-border-subtle bg-background px-1.5 py-0.5 text-[10px] text-muted-foreground">
                  {r.rule_type}
                </span>
                <span>{r.value}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {overrideEntries.length > 0 ? (
        <div>
          <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">
            Detector overrides
          </p>
          <ul className="space-y-1 font-mono text-[11px]">
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
                  <span className="text-muted-foreground">
                    {" "}
                    → {parts.join(", ") || "—"}
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}

      {patterns.length === 0 &&
      access.length === 0 &&
      overrideEntries.length === 0 ? (
        <p className="text-muted-foreground">
          Empty snapshot — template has no rules yet.
        </p>
      ) : null}
    </div>
  );
}
