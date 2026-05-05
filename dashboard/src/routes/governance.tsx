import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe, Shield, Activity, AlertTriangle, ArrowRight, Key } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";
import { KpiCard } from "@/components/observe/kpi-card";
import { formatNumber } from "@/lib/utils";

import { api } from "@/api/client";
import {
  ruleDisplay,
  sevTone,
  actionTone,
  formatUserID,
} from "@/components/governance/types";
import { DeploymentsTab } from "@/components/governance/deployments-tab";
import { OverridesTab } from "@/components/governance/overrides-tab";
import { PolicyTab } from "@/components/governance/policy-tab";
import { WebhooksTab } from "@/components/governance/webhooks-tab";
import { DomainsTab } from "@/components/governance/domains-tab";
import { InstallationsTab } from "@/components/governance/installations-tab";
import { PilotReportTab } from "@/components/governance/pilot-report-tab";

type Tab =
  | "overview"
  | "deployments"
  | "overrides"
  | "policy"
  | "webhooks"
  | "domains"
  | "installations"
  | "pilot-report";

const TABS: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "deployments", label: "Deployments" },
  { id: "overrides", label: "Overrides" },
  { id: "policy", label: "Policy" },
  { id: "webhooks", label: "Webhooks" },
  { id: "domains", label: "Domains" },
  { id: "installations", label: "Installations" },
  { id: "pilot-report", label: "Pilot report" },
];

export function GovernancePage() {
  const [tab, setTab] = useState<Tab>("overview");

  return (
    <div className="space-y-6 p-6">
      <PageHeader
        title="Governance"
        description="Shadow AI usage and policy enforcement across employee browsers."
      />

      <div className="flex gap-1 overflow-x-auto border-b border-border">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`whitespace-nowrap border-b-2 px-4 py-2 text-sm transition ${
              tab === t.id
                ? "border-cyan-500 text-cyan-500"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div>
        {tab === "overview" && <OverviewTab onSwitchTab={setTab} />}
        {tab === "deployments" && <DeploymentsTab />}
        {tab === "overrides" && <OverridesTab />}
        {tab === "policy" && <PolicyTab />}
        {tab === "webhooks" && <WebhooksTab />}
        {tab === "domains" && <DomainsTab />}
        {tab === "installations" && <InstallationsTab />}
        {tab === "pilot-report" && <PilotReportTab />}
      </div>
    </div>
  );
}

function OverviewTab({ onSwitchTab }: { onSwitchTab: (t: Tab) => void }) {
  const [windowDays, setWindowDays] = useState<number>(7);
  const [severityFilter, setSeverityFilter] = useState<string>("");
  const [domainFilter, setDomainFilter] = useState<string>("");

  const overviewQ = useQuery({
    queryKey: ["governance", "overview", windowDays] as const,
    queryFn: () => api.governance.overview({ window_days: windowDays }),
    refetchInterval: 30_000,
  });

  const installsQ = useQuery({
    queryKey: ["governance", "installations"] as const,
    queryFn: () => api.governance.installations.list(),
    staleTime: 60_000,
  });

  const deploymentsQ = useQuery({
    queryKey: ["governance", "deployments"] as const,
    queryFn: () => api.governance.deployments(),
    staleTime: 30_000,
  });

  const eventsQ = useQuery({
    queryKey: ["governance", "events", severityFilter, domainFilter] as const,
    queryFn: () =>
      api.governance.events({
        limit: 50,
        ...(severityFilter ? { severity: severityFilter as "low" | "medium" | "high" } : {}),
        ...(domainFilter ? { source_domain: domainFilter } : {}),
      }),
    refetchInterval: 30_000,
  });

  const policyQ = useQuery({
    queryKey: ["governance", "policy"] as const,
    queryFn: () => api.governance.policy.get(),
    staleTime: 60_000,
  });
  const pseudonymize = policyQ.data?.PseudonymizePII ?? false;

  const overview = overviewQ.data;
  const events = eventsQ.data?.events ?? [];

  // Per-domain drilldown — computed client-side from the currently-loaded
  // events when the user clicks a domain in Top Tools. No extra fetch.
  const drilldown = useMemo(() => {
    if (!domainFilter || events.length === 0) return null;
    const sevCounts: Record<string, number> = { high: 0, medium: 0, low: 0 };
    const ruleCounts = new Map<string, number>();
    const userCounts = new Map<string, number>();
    for (const ev of events) {
      sevCounts[ev.severity] = (sevCounts[ev.severity] ?? 0) + 1;
      for (const rid of ev.rule_ids ?? []) {
        ruleCounts.set(rid, (ruleCounts.get(rid) ?? 0) + 1);
      }
      const uid = ev.user_id ?? "";
      if (uid) userCounts.set(uid, (userCounts.get(uid) ?? 0) + 1);
    }
    const sortDesc = <K,>(m: Map<K, number>) =>
      [...m.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5);
    return {
      sevCounts,
      topRules: sortDesc(ruleCounts),
      topUsers: sortDesc(userCounts),
      total: events.length,
    };
  }, [domainFilter, events]);

  const blocked = overview?.by_action?.blocked ?? 0;
  const warned = overview?.by_action?.warned ?? 0;
  const redirected = overview?.by_action?.redirected ?? 0;
  const overridden = overview?.by_action?.overridden ?? 0;

  const riskScore = useMemo(() => {
    if (!overview || overview.total_events === 0) return 0;
    const numerator = blocked * 3 + warned * 1 + overridden * 2 + redirected * 1;
    const denom = overview.total_events * 3;
    return Math.round((numerator / denom) * 100);
  }, [overview, blocked, warned, redirected, overridden]);

  const installCount = installsQ.data?.installations?.length ?? 0;
  const deploymentCount = deploymentsQ.data?.deployments?.length ?? 0;
  const totalEvents = overview?.total_events ?? 0;
  const firstRunStage: "no-install" | "no-deploy" | "no-events" | "live" = !installsQ.isLoading
    ? installCount === 0
      ? "no-install"
      : deploymentCount === 0
        ? "no-deploy"
        : totalEvents === 0
          ? "no-events"
          : "live"
    : "live";

  return (
    <div className="space-y-6">
      {firstRunStage === "no-install" && (
        <Card className="border-cyan-500/40 bg-cyan-500/5">
          <CardContent className="flex flex-col gap-3 p-6 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="text-sm font-semibold text-cyan-400">Step 1: Get the extension into employee browsers</div>
              <p className="mt-1 text-sm text-muted-foreground">
                Generate an MDM bundle, then push it to your managed Chrome / Edge fleet via Chrome Enterprise, Intune, or Jamf.
              </p>
            </div>
            <button
              type="button"
              onClick={() => onSwitchTab("installations")}
              className="inline-flex items-center gap-2 rounded-md bg-cyan-500 px-4 py-2 text-sm font-medium text-cyan-950 hover:bg-cyan-400"
            >
              <Key className="h-4 w-4" />
              Generate MDM bundle
              <ArrowRight className="h-4 w-4" />
            </button>
          </CardContent>
        </Card>
      )}
      {firstRunStage === "no-deploy" && (
        <Card className="border-yellow-500/40 bg-yellow-500/5">
          <CardContent className="flex flex-col gap-3 p-6 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="text-sm font-semibold text-yellow-400">Step 2: Waiting for the first MDM push</div>
              <p className="mt-1 text-sm text-muted-foreground">
                You have an installation registered, but no browsers have checked in yet. Confirm with IT that the bundle deployed; first heartbeats arrive within ~5 minutes of install.
              </p>
            </div>
            <button
              type="button"
              onClick={() => onSwitchTab("deployments")}
              className="inline-flex items-center gap-2 rounded-md border border-yellow-500/40 bg-transparent px-4 py-2 text-sm text-yellow-400 hover:bg-yellow-500/10"
            >
              View deployment health
              <ArrowRight className="h-4 w-4" />
            </button>
          </CardContent>
        </Card>
      )}
      {firstRunStage === "no-events" && (
        <Card className="border-border bg-muted/30">
          <CardContent className="p-6">
            <div className="text-sm font-semibold">Browsers connected — waiting for first events</div>
            <p className="mt-1 text-sm text-muted-foreground">
              {deploymentCount} {deploymentCount === 1 ? "browser is" : "browsers are"} reporting heartbeats. Events appear here as soon as employees use a tracked AI tool.
            </p>
          </CardContent>
        </Card>
      )}

      <div className="flex items-center justify-between">
        <div className="text-sm text-muted-foreground">
          Last updated {new Date().toLocaleTimeString()} · auto-refreshes every 30s
        </div>
        <div className="flex items-center gap-2">
          {[7, 30, 90].map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setWindowDays(d)}
              className={`rounded-md border px-3 py-1 text-xs font-medium transition ${
                windowDays === d
                  ? "border-cyan-500 bg-cyan-500/10 text-cyan-400"
                  : "border-border bg-transparent text-muted-foreground hover:bg-muted"
              }`}
            >
              {d}d
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          icon={<Activity className="h-4 w-4" />}
          label={`Events (${windowDays}d)`}
          value={overview ? formatNumber(overview.total_events) : "—"}
        />
        <KpiCard
          icon={<Shield className="h-4 w-4" />}
          label="Risk score"
          value={overview ? `${riskScore}/100` : "—"}
          tone={riskScore > 60 ? "danger" : "default"}
        />
        <KpiCard
          icon={<Globe className="h-4 w-4" />}
          label="Unique users"
          value={overview ? formatNumber(overview.unique_users ?? 0) : "—"}
        />
        <KpiCard
          icon={<AlertTriangle className="h-4 w-4" />}
          label="Tools touched"
          value={overview ? formatNumber(overview.unique_domains ?? 0) : "—"}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Card>
          <CardContent className="p-6">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                Top tools
              </h3>
              <span className="text-xs text-muted-foreground">last {windowDays}d</span>
            </div>
            {overviewQ.isLoading ? (
              <SkeletonRows count={5} />
            ) : overview && (overview.top_domains?.length ?? 0) > 0 ? (
              <ul className="space-y-2">
                {(overview.top_domains ?? []).map((d) => (
                  <li
                    key={d.domain}
                    className="flex items-center justify-between text-sm"
                  >
                    <button
                      type="button"
                      onClick={() =>
                        setDomainFilter(domainFilter === d.domain ? "" : d.domain)
                      }
                      className={`font-mono text-xs ${
                        domainFilter === d.domain
                          ? "text-cyan-400"
                          : "text-foreground hover:text-cyan-400"
                      }`}
                    >
                      {d.domain}
                    </button>
                    <span className="font-mono text-xs text-muted-foreground">
                      {formatNumber(d.count)}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyState
                icon={<Globe className="h-6 w-6" />}
                title="No events yet"
                description="Once the Governance extension is deployed and IT pushes managed config via MDM, events will start appearing here."
              />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                Top rules fired
              </h3>
              <span className="text-xs text-muted-foreground">last {windowDays}d</span>
            </div>
            {overviewQ.isLoading ? (
              <SkeletonRows count={5} />
            ) : overview && (overview.top_rules?.length ?? 0) > 0 ? (
              <ul className="space-y-2">
                {(overview.top_rules ?? []).map((r) => (
                  <li
                    key={r.rule_id}
                    className="flex items-center justify-between text-sm"
                  >
                    <span className="font-mono text-xs text-foreground">
                      {ruleDisplay(r.rule_id)}
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {formatNumber(r.count)}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyState
                icon={<Shield className="h-6 w-6" />}
                title="No rules fired"
                description="When the extension intercepts sensitive content, the rule that fired will be listed here."
              />
            )}
          </CardContent>
        </Card>
      </div>

      {drilldown && (
        <Card className="border-cyan-500/30 bg-cyan-500/5">
          <CardContent className="p-6">
            <div className="mb-4 flex items-center justify-between">
              <div>
                <span className="text-xs uppercase tracking-wide text-muted-foreground">Drilldown</span>
                <h3 className="mt-1 font-mono text-base font-semibold">{domainFilter}</h3>
              </div>
              <button
                type="button"
                onClick={() => setDomainFilter("")}
                className="text-xs text-muted-foreground hover:text-foreground"
              >
                Clear filter ×
              </button>
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">
                  Severity ({drilldown.total} recent events)
                </div>
                <div className="space-y-1.5">
                  {(["high", "medium", "low"] as const).map((s) => (
                    <div key={s} className="flex items-center justify-between text-sm">
                      <Badge variant={sevTone(s)} className="font-mono text-[10px]">
                        {s}
                      </Badge>
                      <span className="font-mono text-xs text-muted-foreground">
                        {drilldown.sevCounts[s] ?? 0}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
              <div>
                <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">
                  Top rules
                </div>
                <ul className="space-y-1.5">
                  {drilldown.topRules.map(([rule, count]) => (
                    <li key={rule} className="flex items-center justify-between text-sm">
                      <span className="font-mono text-xs">{ruleDisplay(rule)}</span>
                      <span className="font-mono text-xs text-muted-foreground">{count}</span>
                    </li>
                  ))}
                  {drilldown.topRules.length === 0 && (
                    <li className="text-xs italic text-muted-foreground">no rules fired</li>
                  )}
                </ul>
              </div>
              <div>
                <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">
                  Top users
                </div>
                <ul className="space-y-1.5">
                  {drilldown.topUsers.map(([uid, count]) => (
                    <li key={uid} className="flex items-center justify-between text-sm">
                      <span className="font-mono text-xs">{formatUserID(uid, pseudonymize)}</span>
                      <span className="font-mono text-xs text-muted-foreground">{count}</span>
                    </li>
                  ))}
                  {drilldown.topUsers.length === 0 && (
                    <li className="text-xs italic text-muted-foreground">no users yet</li>
                  )}
                </ul>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          <div className="flex items-center justify-between border-b border-border p-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Recent events
            </h3>
            <div className="flex items-center gap-2">
              <select
                value={severityFilter}
                onChange={(e) => setSeverityFilter(e.target.value)}
                className="rounded border border-border bg-background px-2 py-1 text-xs"
              >
                <option value="">All severities</option>
                <option value="high">High</option>
                <option value="medium">Medium</option>
                <option value="low">Low</option>
              </select>
              {domainFilter && (
                <button
                  type="button"
                  onClick={() => setDomainFilter("")}
                  className="rounded border border-cyan-500/40 bg-cyan-500/10 px-2 py-1 text-xs text-cyan-400"
                >
                  domain: {domainFilter} ×
                </button>
              )}
            </div>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Tool</TableHead>
                <TableHead>Rules</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Action</TableHead>
                <TableHead className="text-right">Chars</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {eventsQ.isLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className="p-6">
                    <SkeletonRows count={6} />
                  </TableCell>
                </TableRow>
              ) : events.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="p-12 text-center text-sm text-muted-foreground"
                  >
                    No events match the current filters.
                  </TableCell>
                </TableRow>
              ) : (
                events.map((ev) => (
                  <TableRow key={ev.event_id}>
                    <TableCell className="font-mono text-xs">
                      {new Date(ev.occurred_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatUserID(ev.user_id, pseudonymize)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {ev.source_domain}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(ev.rule_ids ?? []).slice(0, 3).map((r) => (
                          <Badge
                            key={r}
                            variant="outline"
                            className="font-mono text-[10px]"
                          >
                            {ruleDisplay(r)}
                          </Badge>
                        ))}
                        {(ev.rule_ids?.length ?? 0) > 3 && (
                          <span className="text-xs text-muted-foreground">
                            +{(ev.rule_ids?.length ?? 0) - 3}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={sevTone(ev.severity)}>{ev.severity}</Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={actionTone(ev.action)}>{ev.action}</Badge>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">
                      {formatNumber(ev.char_count_intercepted ?? 0)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
