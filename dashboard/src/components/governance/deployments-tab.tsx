import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Laptop, AlertCircle, AlertTriangle } from "lucide-react";

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
import { EmptyState } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";
import { api } from "@/api/client";

export function DeploymentsTab() {
  const q = useQuery({
    queryKey: ["governance", "deployments"] as const,
    queryFn: () => api.governance.deployments(),
    refetchInterval: 30_000,
  });

  const rows = q.data?.deployments ?? [];
  const stale = (lastSeen: string): boolean =>
    Date.now() - new Date(lastSeen).getTime() > 15 * 60 * 1000;

  // Version-drift detection: how many distinct extension versions are running
  // across all heartbeats. Anything > 1 means the fleet is mid-rollout or
  // some browsers haven't refreshed their CRX. The dashboard surfaces this
  // up-front because mixed versions are the most common cause of "events
  // looked weird this week" support tickets.
  const versionBreakdown = useMemo(() => {
    const map = new Map<string, number>();
    for (const d of rows) {
      const v = d.extension_version || "unknown";
      map.set(v, (map.get(v) ?? 0) + 1);
    }
    return [...map.entries()].sort((a, b) => b[1] - a[1]);
  }, [rows]);
  const driftDetected = versionBreakdown.length > 1;

  return (
    <div className="space-y-4">
      {driftDetected && (
        <Card className="border-yellow-500/40 bg-yellow-500/5">
          <CardContent className="flex items-start gap-3 p-4">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-yellow-500" />
            <div className="flex-1 text-sm">
              <span className="font-semibold text-yellow-400">Extension version drift detected.</span>{" "}
              <span className="text-muted-foreground">
                Browsers are running multiple versions:{" "}
                {versionBreakdown.map(([v, n], i) => (
                  <span key={v} className="font-mono">
                    {v} ({n}){i < versionBreakdown.length - 1 ? ", " : ""}
                  </span>
                ))}
                . Confirm whether MDM rollout is in progress or stalled.
              </span>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          <div className="flex items-center justify-between border-b border-border p-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Extension deployment health
            </h3>
            <span className="text-xs text-muted-foreground">
              heartbeats every 5 min · stale after 15 min
            </span>
          </div>
        {q.isLoading ? (
          <div className="p-6">
            <SkeletonRows count={6} />
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<Laptop className="h-6 w-6" />}
            title="No deployments yet"
            description="When the extension is force-installed via MDM and successfully connects, each browser instance will appear here."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Status</TableHead>
                <TableHead>Install ID</TableHead>
                <TableHead>Browser</TableHead>
                <TableHead>Version</TableHead>
                <TableHead>Extension</TableHead>
                <TableHead>Last seen</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((d) => (
                <TableRow key={`${d.org_id}-${d.install_id}`}>
                  <TableCell>
                    {stale(d.last_seen_at) ? (
                      <Badge variant="destructive" className="gap-1">
                        <AlertCircle className="h-3 w-3" /> stale
                      </Badge>
                    ) : (
                      <Badge variant="default" className="bg-green-500/15 text-green-500">
                        active
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {d.install_id.slice(0, 16)}…
                  </TableCell>
                  <TableCell className="font-mono text-xs capitalize">
                    {d.browser}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {d.browser_version}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    v{d.extension_version}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {new Date(d.last_seen_at).toLocaleString()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        </CardContent>
      </Card>
    </div>
  );
}
