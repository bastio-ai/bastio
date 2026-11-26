import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ChevronLeft, ExternalLink, ShieldPlus, ShieldX } from "lucide-react";

import { api } from "@/api/client";
import type { ThreatEvent } from "@/api/client";
import { overlayApi, overlayKeys } from "@/api/overlay";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AdminPageHeader,
  AdminPanel,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { AllowPatternAction } from "@/components/observe/allow-pattern-action";
import { ConnectedFindingExplanation } from "@/components/observe/finding-explanation-card";
import { SkeletonRows } from "@/components/skeleton";
import { cn, weightedThreatScore } from "@/lib/utils";

const severityDotClass = (severity: string) => {
  switch (severity) {
    case "critical":
    case "high":
      return "bg-destructive";
    case "medium":
      return "bg-warn";
    default:
      return "bg-muted-foreground/50";
  }
};

export function ThreatDetailPage() {
  const { id } = useParams({ from: "/threats/$id" });
  const { data, isLoading, isError } = useQuery({
    queryKey: ["threats", "detail", id],
    queryFn: () => api.threats.get(id),
  });

  if (isLoading) {
    return (
      <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
        <AdminPageHeader
          eyebrow="Security investigation"
          title="Threat"
          description="Loading threat event…"
          actions={<BackToThreats />}
        />
        <div className="border-y border-border-subtle">
          <SkeletonRows count={4} />
        </div>
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
        <AdminPageHeader
          eyebrow="Security investigation"
          title="Threat not found"
          description="This threat may have expired or belong to another tenant."
          actions={<BackToThreats />}
        />
        <div className="border-y border-border-subtle py-12 text-center">
          <ShieldX
            aria-hidden="true"
            className="mx-auto mb-2.5 h-5 w-5 text-muted-foreground"
          />
          <p className="text-[13px] font-medium text-foreground">
            Nothing to show
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            Return to the threats list to pick a different event.
          </p>
        </div>
      </div>
    );
  }

  const threat = data as ThreatEvent;
  const endUserID = (threat as ThreatEvent & { end_user_id?: string }).end_user_id;
  const ipAddress = (threat as ThreatEvent & { ip_address?: string }).ip_address;
  const userAgent = (threat as ThreatEvent & { user_agent?: string }).user_agent;
  const details = (threat as ThreatEvent & { details?: Record<string, string> })
    .details;
  const weightedScore = weightedThreatScore(
    threat.score,
    threat.confidence,
    threat.weighted_score,
  );

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Security investigation"
        title={<span className="font-mono">{threat.threat_type}</span>}
        description={`Detected by ${threat.detector_name} · ${new Date(
          threat.detected_at,
        ).toLocaleString()}`}
        bordered={false}
        className="mb-4"
        badge={
          <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className={cn("size-1.5 rounded-full", severityDotClass(threat.severity))}
            />
            <span className="capitalize">{threat.severity}</span>
          </span>
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <AllowPatternAction threat={threat} />
            <CapturePolicyAction threatID={threat.id} />
            <BackToThreats />
          </div>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Severity",
            value: threat.severity,
            detail: `Confidence ${(threat.confidence * 100).toFixed(0)}%`,
            tone:
              threat.severity === "critical" || threat.severity === "high"
                ? "danger"
                : "warning",
          },
          {
            label: "Action",
            value: threat.action_taken,
            detail:
              threat.action_taken === "block"
                ? "Request prevented"
                : "Event recorded",
            tone: threat.action_taken === "block" ? "danger" : "warning",
          },
          {
            label: "Weighted score",
            value: `${(weightedScore * 100).toFixed(0)}%`,
            detail: "Severity × confidence",
          },
          {
            label: "Detector",
            value: threat.detector_name,
            detail: threat.threat_subtype || "No subtype",
          },
        ]}
      />

      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_19rem]">
        <div className="min-w-0 space-y-6">
          <ConnectedFindingExplanation key={threat.id} finding={threat} />

          <AdminPanel
            title="Matched pattern"
            description="Rule or detector signature that matched this payload."
          >
            <pre className="overflow-x-auto whitespace-pre border border-border-subtle bg-surface-1/50 px-3.5 py-2.5 font-mono text-xs leading-relaxed text-foreground">
              {threat.matched_pattern}
            </pre>
          </AdminPanel>

          {threat.matched_content ? (
            <AdminPanel
              title="Matched content"
              description="Excerpt from the inspected gateway payload."
            >
              <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words border border-border-subtle bg-surface-1/50 px-3.5 py-2.5 font-mono text-xs leading-relaxed text-foreground">
                {threat.matched_content}
              </pre>
            </AdminPanel>
          ) : null}

          <AdminPanel
            title="Raw event"
            description="Full serialized threat record."
          >
            <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words border border-border-subtle bg-surface-1/30 px-3.5 py-2.5 font-mono text-[11px] leading-relaxed text-muted-foreground">
              {JSON.stringify(threat, null, 2)}
            </pre>
          </AdminPanel>
        </div>

        <aside className="space-y-6">
          <AdminPanel title="Outcome" contentClassName="py-0">
            <dl className="divide-y divide-border-subtle text-xs">
              <Row label="Action">
                <span className="inline-flex items-center gap-1.5 font-mono text-foreground">
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-1.5 rounded-full",
                      threat.action_taken === "block"
                        ? "bg-destructive"
                        : "bg-warn",
                    )}
                  />
                  {threat.action_taken}
                </span>
              </Row>
              <Row label="Score (weighted)">
                <span
                  className="font-mono tabular-nums"
                  title="Severity × Confidence — what the threshold actually compares against."
                >
                  {(weightedScore * 100).toFixed(0)}%
                </span>
              </Row>
              <Row label="Severity × Confidence">
                <span className="font-mono tabular-nums text-muted-foreground">
                  {threat.score.toFixed(2)} × {threat.confidence.toFixed(2)}
                </span>
              </Row>
            </dl>
          </AdminPanel>

          <AdminPanel title="Context" contentClassName="py-0">
            <dl className="divide-y divide-border-subtle text-xs">
              <Row label="End user">
                {endUserID ? (
                  <Link
                    to="/users/$id"
                    params={{ id: endUserID }}
                    className="font-mono text-foreground underline decoration-border-default underline-offset-4 hover:decoration-foreground"
                  >
                    {endUserID}
                  </Link>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </Row>
              <Row label="IP">
                <span className="font-mono">{ipAddress || "—"}</span>
              </Row>
              <Row label="User-Agent">
                <span className="font-mono text-[11px] break-all text-muted-foreground">
                  {userAgent || "—"}
                </span>
              </Row>
            </dl>
          </AdminPanel>

          {details && Object.keys(details).length > 0 ? (
            <AdminPanel title="Detector metadata" contentClassName="py-0">
              <dl className="divide-y divide-border-subtle text-[11px]">
                {Object.entries(details).map(([k, v]) => (
                  <Row key={k} label={k}>
                    <span className="font-mono break-all text-muted-foreground">
                      {v}
                    </span>
                  </Row>
                ))}
              </dl>
            </AdminPanel>
          ) : null}

          <div className="border-t border-border-subtle pt-4">
            <Link
              to="/traces/$id"
              params={{ id: threat.trace_id }}
              className={cn(
                buttonVariants({ variant: "outline", size: "sm" }),
                "w-full justify-between text-xs",
              )}
            >
              <span>Open related trace</span>
              <ExternalLink aria-hidden="true" className="h-3.5 w-3.5" />
            </Link>
          </div>
        </aside>
      </div>
    </div>
  );
}

function BackToThreats() {
  return (
    <Link
      to="/threats"
      className={cn(buttonVariants({ variant: "outline", size: "sm" }), "h-8 text-xs")}
    >
      <ChevronLeft aria-hidden="true" className="h-3.5 w-3.5" /> Back to threats
    </Link>
  );
}

// CapturePolicyAction is the "turn this judgement into a durable rule"
// shortcut. When no custom policies exist yet, a single button
// navigates to /overlays/new with the threat id in the search params
// so the new-policy page can pre-fill a pattern rule. When one or
// more policies exist, the button exposes a tiny picker: either
// create a brand-new policy or append a draft version to an existing
// one — both routes preserve from_threat so the prefill code path
// is the same.
function CapturePolicyAction({ threatID }: { threatID: string }) {
  const navigate = useNavigate();
  const overlaysQuery = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
  });
  const overlays = overlaysQuery.data ?? [];

  if (overlaysQuery.isLoading || overlays.length === 0) {
    return (
      <Button
        type="button"
        size="sm"
        className="h-8 text-xs"
        onClick={() =>
          navigate({
            to: "/overlays/new",
            search: { from_threat: threatID, template: undefined },
          })
        }
      >
        <ShieldPlus aria-hidden="true" className="h-3.5 w-3.5" /> Capture as custom policy
      </Button>
    );
  }

  return (
    <Select
      onValueChange={(value: string | null) => {
        if (!value) return;
        if (value === "__new__") {
          navigate({
            to: "/overlays/new",
            search: { from_threat: threatID, template: undefined },
          });
          return;
        }
        navigate({
          to: "/overlays/$id/versions/new",
          params: { id: value },
          search: { from_threat: threatID },
        });
      }}
    >
      <SelectTrigger aria-label="Capture as custom policy" className="h-8 w-56 text-xs">
        <div className="flex items-center gap-1.5">
          <ShieldPlus aria-hidden="true" className="h-3.5 w-3.5" />
          <SelectValue placeholder="Capture as custom policy…" />
        </div>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="__new__" className="text-xs">
          + New policy from this threat
        </SelectItem>
        {overlays.map((o) => (
          <SelectItem key={o.id} value={o.id} className="text-xs">
            Add to {o.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-3 py-2.5">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right text-foreground">{children}</dd>
    </div>
  );
}
