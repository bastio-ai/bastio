import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Check, Copy, ExternalLink } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import type { ThreatEvent } from "@/api/client";
import { weightedThreatScore } from "@/lib/utils";

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

type Props = {
  threat: ThreatEvent | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function ThreatDetailSheet({ threat, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent>
        {threat ? <ThreatDetailBody threat={threat} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function ThreatDetailBody({ threat }: { threat: ThreatEvent }) {
  const details = (threat as ThreatEvent & { details?: Record<string, string> })
    .details;
  const userAgent = (threat as ThreatEvent & { user_agent?: string }).user_agent;
  const endUserID = (threat as ThreatEvent & { end_user_id?: string }).end_user_id;
  const ipAddress = (threat as ThreatEvent & { ip_address?: string }).ip_address;
  const threatSubtype = threat.threat_subtype;
  const source = threat.source;
  return (
    <>
      <SheetHeader>
        <div className="flex items-center gap-2">
          <Badge
            variant={severityVariant(threat.severity)}
            className="text-[10px] px-1.5 py-0"
          >
            {threat.severity}
          </Badge>
          <SheetTitle>{threat.threat_type}</SheetTitle>
          <Badge
            variant={threat.action_taken === "block" ? "destructive" : "outline"}
            className="text-[10px] px-1.5 py-0"
          >
            {threat.action_taken}
          </Badge>
        </div>
        <SheetDescription>
          Detected by{" "}
          <span className="font-mono text-foreground/80">
            {threat.detector_name}
          </span>{" "}
          ·{" "}
          <span className="tabular-nums">
            {new Date(threat.detected_at).toLocaleString()}
          </span>
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-4 overflow-y-auto pr-1">
        <ScoreRow
          confidence={threat.confidence}
          score={threat.score}
          weightedScore={threat.weighted_score}
        />

        <Section label="Matched pattern">
          <CopyableBlock text={threat.matched_pattern} />
        </Section>

        {threat.matched_content ? (
          <Section label="Matched content">
            <CopyableBlock text={threat.matched_content} multiline />
          </Section>
        ) : null}

        <Section label="Context">
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[11px]">
            {threatSubtype ? (
              <Row term="Subtype" value={threatSubtype} mono />
            ) : null}
            {source ? (
              <Row term="Source" value={source} mono />
            ) : null}
            <Row
              term="End user"
              value={
                endUserID ? (
                  <Link
                    to="/users/$id"
                    params={{ id: endUserID }}
                    className="font-mono text-foreground/80 hover:underline"
                  >
                    {endUserID}
                  </Link>
                ) : (
                  "—"
                )
              }
            />
            <Row term="IP" value={ipAddress || "—"} mono />
            <Row term="User-Agent" value={userAgent || "—"} mono truncate />
          </dl>
        </Section>

        {details && Object.keys(details).length > 0 ? (
          <Section label="Detector metadata">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-[11px]">
              {Object.entries(details).map(([k, v]) => (
                <Row key={k} term={k} value={v} mono truncate />
              ))}
            </dl>
          </Section>
        ) : null}
      </div>

      <SheetFooter>
        <Link
          to="/threats/$id"
          params={{ id: threat.id }}
          className={buttonVariants({ variant: "link", size: "sm" }) + " text-xs"}
        >
          <ExternalLink className="h-3 w-3" /> Open full view
        </Link>
        <Link
          to="/traces/$id"
          params={{ id: threat.trace_id }}
          className={buttonVariants({ variant: "outline", size: "sm" }) + " text-xs"}
        >
          Open related trace <ExternalLink className="h-3 w-3" />
        </Link>
      </SheetFooter>
    </>
  );
}

function ScoreRow({
  confidence,
  score,
  weightedScore,
}: {
  confidence: number;
  score: number;
  weightedScore?: number;
}) {
  // The engine compares score × confidence against the configured
  // threshold. Showing only the raw severity-based score made customers
  // think the threshold was higher than it really effectively was —
  // they saw 85%, set threshold = 70%, and were surprised when nothing
  // blocked because the weighted value was 68%. The weighted value is
  // the primary number; raw severity and confidence are the detail.
  const weighted = weightedThreatScore(score, confidence, weightedScore);
  return (
    <div className="flex items-center gap-4 rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-[11px]">
      <Metric
        label="Score (weighted)"
        value={`${(weighted * 100).toFixed(0)}%`}
        hint="Severity × Confidence — what the threshold actually compares against."
      />
      <div className="h-5 w-px bg-border" />
      <Metric
        label="Severity × Confidence"
        value={`${score.toFixed(2)} × ${confidence.toFixed(2)}`}
        hint="Raw severity score and detector confidence behind the weighted value."
      />
    </div>
  );
}

function Metric({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div title={hint}>
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground/70">
        {label}
      </div>
      <div className="font-mono text-sm tabular-nums text-foreground">
        {value}
      </div>
    </div>
  );
}

function Section({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        {label}
      </div>
      {children}
    </div>
  );
}

function Row({
  term,
  value,
  mono = false,
  truncate = false,
}: {
  term: string;
  value: React.ReactNode;
  mono?: boolean;
  truncate?: boolean;
}) {
  return (
    <>
      <dt className="text-muted-foreground">{term}</dt>
      <dd
        className={
          (mono ? "font-mono " : "") +
          (truncate ? "truncate " : "") +
          "text-foreground/90"
        }
      >
        {value}
      </dd>
    </>
  );
}

function CopyableBlock({
  text,
  multiline = false,
}: {
  text: string;
  multiline?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may be blocked in dev / iframe contexts; fail quietly.
    }
  };
  return (
    <div className="group relative">
      <pre
        className={
          (multiline
            ? "max-h-48 overflow-auto whitespace-pre-wrap break-words "
            : "overflow-x-auto whitespace-pre ") +
          "rounded-md border border-border/50 bg-muted/40 px-3 py-2 pr-10 font-mono text-[11px] leading-relaxed text-foreground/90"
        }
      >
        {text}
      </pre>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={onCopy}
        aria-label="Copy"
        className="absolute top-1.5 right-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
      >
        {copied ? (
          <Check className="h-3 w-3" />
        ) : (
          <Copy className="h-3 w-3" />
        )}
      </Button>
    </div>
  );
}
