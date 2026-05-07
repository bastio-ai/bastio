import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ChevronLeft, ExternalLink, ShieldPlus, ShieldX } from "lucide-react";

import { api } from "@/api/client";
import type { ThreatEvent } from "@/api/client";
import { overlayApi, overlayKeys } from "@/api/overlay";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";

const severityVariant = (s: string) => {
  switch (s) {
    case "critical":
      return "destructive" as const;
    case "high":
      return "warning" as const;
    default:
      return "secondary" as const;
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
      <>
        <PageHeader title="Threat" description="Loading..." />
        <Card className="border-border/50">
          <CardContent className="p-0">
            <SkeletonRows count={4} />
          </CardContent>
        </Card>
      </>
    );
  }

  if (isError || !data) {
    return (
      <>
        <PageHeader
          title="Threat not found"
          description="This threat may have expired or belong to another tenant."
          action={<BackToThreats />}
        />
        <Card className="border-border/50">
          <CardContent className="p-0">
            <EmptyState
              icon={<ShieldX className="h-6 w-6" />}
              title="Nothing to show"
              description="Return to the threats list to pick a different event."
            />
          </CardContent>
        </Card>
      </>
    );
  }

  const threat = data as ThreatEvent;
  const endUserID = (threat as ThreatEvent & { end_user_id?: string }).end_user_id;
  const ipAddress = (threat as ThreatEvent & { ip_address?: string }).ip_address;
  const userAgent = (threat as ThreatEvent & { user_agent?: string }).user_agent;
  const details = (threat as ThreatEvent & { details?: Record<string, string> })
    .details;

  return (
    <>
      <PageHeader
        title={threat.threat_type}
        description={`Detected by ${threat.detector_name} · ${new Date(
          threat.detected_at,
        ).toLocaleString()}`}
        badge={
          <Badge
            variant={severityVariant(threat.severity)}
            className="text-[10px] px-1.5 py-0"
          >
            {threat.severity}
          </Badge>
        }
        action={
          <div className="flex items-center gap-2">
            <CapturePolicyAction threatID={threat.id} />
            <BackToThreats />
          </div>
        }
      />

      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="space-y-3">
          <Section title="Matched pattern">
            <pre className="overflow-x-auto whitespace-pre rounded-md border border-border/50 bg-muted/40 px-3 py-2 font-mono text-[12px] leading-relaxed text-foreground/90">
              {threat.matched_pattern}
            </pre>
          </Section>
          {threat.matched_content ? (
            <Section title="Matched content">
              <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border/50 bg-muted/40 px-3 py-2 font-mono text-[12px] leading-relaxed text-foreground/90">
                {threat.matched_content}
              </pre>
            </Section>
          ) : null}

          <Section title="Raw">
            <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border/50 bg-muted/20 px-3 py-2 font-mono text-[11px] leading-relaxed text-muted-foreground">
              {JSON.stringify(threat, null, 2)}
            </pre>
          </Section>
        </div>

        <aside className="space-y-3">
          <Section title="Outcome">
            <Card className="border-border/50">
              <CardContent className="space-y-2 p-3 text-[12px]">
                <Row label="Action">
                  <Badge
                    variant={
                      threat.action_taken === "block" ? "destructive" : "outline"
                    }
                    className="text-[10px] px-1.5 py-0"
                  >
                    {threat.action_taken}
                  </Badge>
                </Row>
                <Row label="Severity">
                  <span className="font-mono tabular-nums">
                    {(threat.score * 100).toFixed(0)}%
                  </span>
                </Row>
                <Row label="Confidence">
                  <span className="font-mono tabular-nums">
                    {(threat.confidence * 100).toFixed(0)}%
                  </span>
                </Row>
                <Row label="Triggers at">
                  <span
                    className="font-mono tabular-nums"
                    title="Severity × Confidence — what the threshold actually compares against."
                  >
                    {(threat.score * threat.confidence * 100).toFixed(0)}%
                  </span>
                </Row>
              </CardContent>
            </Card>
          </Section>

          <Section title="Context">
            <Card className="border-border/50">
              <CardContent className="space-y-2 p-3 text-[12px]">
                <Row label="End user">
                  {endUserID ? (
                    <Link
                      to="/users/$id"
                      params={{ id: endUserID }}
                      className="font-mono text-foreground/80 hover:underline"
                    >
                      {endUserID}
                    </Link>
                  ) : (
                    "—"
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
              </CardContent>
            </Card>
          </Section>

          {details && Object.keys(details).length > 0 ? (
            <Section title="Detector metadata">
              <Card className="border-border/50">
                <CardContent className="space-y-1 p-3 text-[11px]">
                  {Object.entries(details).map(([k, v]) => (
                    <Row key={k} label={k}>
                      <span className="font-mono break-all text-muted-foreground">
                        {v}
                      </span>
                    </Row>
                  ))}
                </CardContent>
              </Card>
            </Section>
          ) : null}

          <Link
            to="/traces/$id"
            params={{ id: threat.trace_id }}
            className={
              buttonVariants({ variant: "outline", size: "sm" }) +
              " w-full text-xs"
            }
          >
            Open related trace <ExternalLink className="h-3 w-3" />
          </Link>
        </aside>
      </div>
    </>
  );
}

function BackToThreats() {
  return (
    <Link
      to="/threats"
      className={buttonVariants({ variant: "outline", size: "sm" }) + " h-8 text-xs"}
    >
      <ChevronLeft className="h-3 w-3" /> Back to threats
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
        size="sm"
        className="h-8 text-xs"
        onClick={() =>
          navigate({
            to: "/overlays/new",
            search: { from_threat: threatID, template: undefined },
          })
        }
      >
        <ShieldPlus className="h-3 w-3" /> Capture as custom policy
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
      <SelectTrigger className="h-8 w-56 text-xs">
        <div className="flex items-center gap-1">
          <ShieldPlus className="h-3 w-3" />
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

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        {title}
      </div>
      {children}
    </div>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right text-foreground/90">{children}</span>
    </div>
  );
}
