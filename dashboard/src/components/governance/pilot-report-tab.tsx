import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText, Sparkles, ArrowRight } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/card";
import { api } from "@/api/client";
import { formatNumber } from "@/lib/utils";

export function PilotReportTab() {
  const q = useQuery({
    queryKey: ["governance", "pilot-report"] as const,
    queryFn: () => api.governance.pilotReport(),
  });

  if (q.isLoading) {
    return (
      <Card>
        <CardContent className="p-12 text-center text-sm text-muted-foreground">
          Generating report…
        </CardContent>
      </Card>
    );
  }
  if (q.error) {
    return (
      <Card>
        <CardContent className="p-6 text-sm text-destructive">
          Failed to generate report: {(q.error as Error).message}
        </CardContent>
      </Card>
    );
  }
  const r = q.data;
  if (!r) return null;

  if (r.total_events === 0) {
    return (
      <Card>
        <CardContent>
          <EmptyState
            icon={<FileText className="h-6 w-6" />}
            title="Not enough data yet"
            description="The pilot report needs at least 14 days of governance events to produce a meaningful baseline. Once events start arriving, this view auto-fills."
          />
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-2 p-6">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-2xl font-semibold tracking-tight">
                Shadow AI Audit — {r.org_label}
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {r.window_days}-day window through{" "}
                {new Date(r.generated_at).toLocaleDateString()}
              </p>
            </div>
            <a
              href={`${api.governance.pilotReportPDFURL()}`}
              download
              className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              <Download className="h-4 w-4" />
              Download PDF
            </a>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4 p-6">
          <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            Executive summary
          </h3>
          <p className="text-sm leading-relaxed">
            Across the past {r.window_days} days, your employees attempted to share data with public
            AI tools <strong>{formatNumber(r.total_events)}</strong> times. <strong>{r.high_severity}</strong>{" "}
            of those events involved high-severity content (PII, secrets, or code). <strong>{r.unique_users}</strong>{" "}
            distinct users touched <strong>{r.unique_domains}</strong> different AI tools.
          </p>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <KpiBox label="Total events" value={r.total_events ?? 0} />
            <KpiBox label="High severity" value={r.high_severity ?? 0} tone="danger" />
            <KpiBox label="Blocked" value={r.blocked_count ?? 0} />
            <KpiBox label="Overridden" value={r.overridden_count ?? 0} tone="warn" />
          </div>
        </CardContent>
      </Card>

      <ActivateWorkspaceCTA recommendedSeats={r.unique_users ?? 1} />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Card>
          <CardContent className="p-6">
            <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Top tools
            </h3>
            <ul className="space-y-2">
              {(r.top_domains ?? []).map((d) => (
                <li key={d.domain} className="flex justify-between text-sm">
                  <span className="font-mono text-xs">{d.domain}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {formatNumber(d.count)}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-6">
            <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Top rules fired
            </h3>
            <ul className="space-y-2">
              {(r.top_rules ?? []).map((rule) => (
                <li key={rule.rule_id} className="flex justify-between text-sm">
                  <span className="font-mono text-xs">{rule.rule_id}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {formatNumber(rule.count)}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="p-6">
          <h3 className="mb-4 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            EU AI Act + GDPR coverage
          </h3>
          <div className="space-y-3">
            {(r.compliance_map ?? []).map((c) => (
              <div
                key={c.article}
                className="rounded-md border border-border bg-muted/30 p-4"
              >
                <Badge variant="outline" className="font-mono text-[10px] text-cyan-500">
                  {c.article}
                </Badge>
                <h4 className="mt-2 text-sm font-semibold">{c.title}</h4>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                  {c.coverage}
                </p>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

// ActivateWorkspaceCTA — the wedge close. Renders below the executive
// summary so the report data lands first; clicking the button opens
// Stripe Checkout with the audit-detected seat count pre-filled.
//
// `/billing/checkout` is a cloud-only endpoint. In OSS deployments
// (no auth + no Stripe) the request returns 503 and the inline error
// surfaces — operator sees clearly that this is a Cloud feature.
function ActivateWorkspaceCTA({ recommendedSeats }: { recommendedSeats: number | bigint }) {
  const initial = Math.max(1, Math.min(1000, Number(recommendedSeats) || 1));
  const [seats, setSeats] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onActivate = async () => {
    setError(null);
    setBusy(true);
    try {
      const r = await fetch("/billing/checkout", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ seats }),
      });
      if (!r.ok) {
        const t = await r.json().catch(() => ({ error: `checkout failed (${r.status})` }));
        throw new Error(t.error ?? "checkout failed");
      }
      const body = (await r.json()) as { url: string };
      window.location.href = body.url;
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <Card className="border-cyan-500/30 bg-gradient-to-br from-cyan-500/5 to-transparent">
      <CardContent className="space-y-4 p-6">
        <div className="flex items-start gap-4">
          <div className="rounded-md bg-cyan-500/10 p-2 text-cyan-500">
            <Sparkles className="h-5 w-5" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-semibold">Plug the leak — activate Workspace</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Redirect the {formatNumber(initial)} {initial === 1 ? "user" : "users"} touching public AI
              into a policy-enforced Bastio Workspace. Same chat surface, audited and EU-resident.
              Cancel anytime.
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted-foreground">Seats</span>
            <input
              type="number"
              min={1}
              max={1000}
              value={seats}
              onChange={(e) => setSeats(Math.max(1, Math.min(1000, Number(e.target.value) || 1)))}
              className="w-24 rounded-md border border-border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-cyan-500"
            />
          </label>
          <div className="text-xs text-muted-foreground">
            $25/seat/month
            <span className="ml-2 font-mono text-foreground">
              ${(seats * 25).toLocaleString()}/mo
            </span>
          </div>
          <Button
            onClick={onActivate}
            disabled={busy || seats < 1}
            className="ml-auto"
          >
            {busy ? "Opening Stripe…" : "Activate Workspace"}
            {!busy && <ArrowRight className="ml-2 h-4 w-4" />}
          </Button>
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}
        {!error && (
          <p className="text-xs text-muted-foreground">
            Recommended seats reflect the {formatNumber(initial)} active{" "}
            {initial === 1 ? "user" : "users"} detected during the audit window.
            Stripe handles the checkout; you'll return here once payment completes.
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function KpiBox({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "danger" | "warn";
}) {
  const color =
    tone === "danger"
      ? "text-destructive"
      : tone === "warn"
        ? "text-yellow-500"
        : "text-foreground";
  return (
    <div className="rounded-md border border-border p-4">
      <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className={`mt-1 text-2xl font-semibold tracking-tight ${color}`}>
        {formatNumber(value)}
      </div>
    </div>
  );
}
