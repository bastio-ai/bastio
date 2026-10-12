import { useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { PageHeader } from "@/components/card";
import { Button } from "@/components/ui/button";
import {
  AdminSummaryStrip,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
type Report = {
  generated_at: string;
  period_start: string;
  period_end: string;
  scope: string;
  certification: string;
  settings: { store_conversation_content: boolean };
  counts: {
    messages: number;
    model_calls: number;
    policy_blocks: number;
    output_blocks: number;
    tokens: number;
    estimated_spend_cents: number;
  };
  events_total: number;
  events_truncated: boolean;
  events: {
    id: string;
    action: string;
    actor_email: string;
    target_type: string;
    created_at: string;
  }[];
};
export function CompliancePage() {
  const report = useQuery({
    queryKey: ["workspace", "evidence"],
    queryFn: async (): Promise<Report> => {
      const r = await fetch("/v1/workspace/evidence", {
        credentials: "include",
      });
      if (!r.ok)
        throw new Error("Workspace evidence unavailable or access denied");
      return r.json();
    },
  });
  const data = report.data;
  return (
    <div className="space-y-5">
      <PageHeader
        title="Compliance & Audit"
        description="Dated Workspace settings, usage and administrative events."
        action={
          <Button
            size="sm"
            disabled={!data}
            onClick={() => window.location.assign("/v1/workspace/evidence")}
          >
            <Download className="size-3.5" />
            Export evidence JSON
          </Button>
        }
      />
      <SecurityNotice title="Operational evidence" tone="info">
        This report contains real Workspace records. It does not certify
        compliance with a regulation or security standard. Archived messages and
        gateway events are excluded from its usage counts.
      </SecurityNotice>
      {report.error && (
        <p role="alert" className="text-sm text-destructive">
          {report.error.message}
        </p>
      )}
      {data && (
        <>
          <AdminSummaryStrip
            items={[
              {
                label: "Model calls",
                value: data.counts.model_calls,
                detail: "Recorded in PostgreSQL",
              },
              {
                label: "Policy blocks",
                value: data.counts.policy_blocks + data.counts.output_blocks,
                detail: "Input and output decisions",
              },
              {
                label: "Estimated spend",
                value: `$${(data.counts.estimated_spend_cents / 100).toFixed(2)}`,
                detail: "Transactional model cost ledger",
              },
              {
                label: "Content storage",
                value: data.settings.store_conversation_content
                  ? "Enabled"
                  : "Disabled",
                detail: "Current setting, not historical policy",
              },
            ]}
          />
          <p className="text-xs text-muted-foreground">
            {new Date(data.period_start).toLocaleString()} –{" "}
            {new Date(data.period_end).toLocaleString()} · Generated{" "}
            {new Date(data.generated_at).toLocaleString()}
          </p>
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-left text-xs">
              <thead className="bg-muted">
                <tr>
                  <th className="p-3">Time</th>
                  <th>Actor</th>
                  <th>Action</th>
                  <th>Resource</th>
                </tr>
              </thead>
              <tbody>
                {data.events.map((e) => (
                  <tr key={e.id} className="border-t border-border">
                    <td className="p-3 font-mono">
                      {new Date(e.created_at).toLocaleString()}
                    </td>
                    <td>{e.actor_email}</td>
                    <td>{e.action}</td>
                    <td>{e.target_type}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {data.events.length === 0 && (
              <p className="p-4 text-xs text-muted-foreground">
                No administrative events in this period.
              </p>
            )}
          </div>
          {data.events_truncated && (
            <p className="text-xs text-muted-foreground">
              Showing the latest 1,000 of {data.events_total} events. Use the
              evidence API date range to export smaller periods.
            </p>
          )}
        </>
      )}
    </div>
  );
}
