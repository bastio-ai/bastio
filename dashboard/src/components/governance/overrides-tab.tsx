import { useQuery } from "@tanstack/react-query";
import { ShieldAlert } from "lucide-react";

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
import { ruleDisplay, sevTone, formatUserID } from "./types";

export function OverridesTab() {
  const q = useQuery({
    queryKey: ["governance", "overrides"] as const,
    queryFn: () => api.governance.overrides({ limit: 200 }),
    refetchInterval: 30_000,
  });

  const policyQ = useQuery({
    queryKey: ["governance", "policy"] as const,
    queryFn: () => api.governance.policy.get(),
    staleTime: 60_000,
  });
  const pseudonymize = policyQ.data?.PseudonymizePII ?? false;

  const rows = q.data?.overrides ?? [];

  return (
    <Card>
      <CardContent className="p-0">
        <div className="flex items-center justify-between border-b border-border p-4">
          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Override audit log
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Every time an employee bypasses a policy block with justification.
            </p>
          </div>
          <Badge variant="outline">
            {q.data?.count ?? 0} overrides
          </Badge>
        </div>
        {q.isLoading ? (
          <div className="p-6">
            <SkeletonRows count={6} />
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<ShieldAlert className="h-6 w-6" />}
            title="No overrides yet"
            description="When an employee uses 'Send anyway (logged)' on a high-severity block, the event lands here with their justification."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Tool</TableHead>
                <TableHead>Rules</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Justification</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((ev) => (
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
                      {(ev.rule_ids ?? []).slice(0, 2).map((r) => (
                        <Badge
                          key={r}
                          variant="outline"
                          className="font-mono text-[10px]"
                        >
                          {ruleDisplay(r)}
                        </Badge>
                      ))}
                      {(ev.rule_ids?.length ?? 0) > 2 && (
                        <span className="text-xs text-muted-foreground">
                          +{(ev.rule_ids?.length ?? 0) - 2}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={sevTone(ev.severity)}>{ev.severity}</Badge>
                  </TableCell>
                  <TableCell className="max-w-md text-xs italic text-muted-foreground">
                    {ev.override_justification || <span className="not-italic">—</span>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
