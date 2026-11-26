import { useQuery } from "@tanstack/react-query";
import { Download, FileCheck } from "lucide-react";
import { EmptyState } from "@/components/card";
import { Button } from "@/components/ui/button";
import {
  AdminPageHeader,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";

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
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Security & guardrails"
        title="Compliance & Audit"
        description="Dated Workspace settings, usage counts, and administrative audit events."
        bordered={false}
        className="mb-4"
        actions={
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={!data}
            onClick={() => window.location.assign("/v1/workspace/evidence")}
          >
            <Download aria-hidden="true" className="size-3.5" />
            Export evidence JSON
          </Button>
        }
      />

      <p className="mb-6 border-l-2 border-border-default pl-3 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">
          Operational evidence.
        </span>{" "}
        This report contains recorded Workspace configuration and audit events.
        It does not certify compliance with a regulation or security standard.
        Archived messages and gateway events are excluded from Workspace usage
        counts.
      </p>

      {report.isPending ? (
        <div className="border-y border-border-subtle py-4">
          <SkeletonRows count={4} />
        </div>
      ) : report.error ? (
        <div
          role="alert"
          className="border-y border-border-subtle py-8 text-xs text-muted-foreground"
        >
          <p className="font-medium text-foreground">
            Evidence report unavailable
          </p>
          <p className="mt-1">{report.error.message}</p>
        </div>
      ) : data ? (
        <>
          <AdminSummaryStrip
            items={[
              {
                label: "Model calls",
                value: data.counts.model_calls.toLocaleString(),
                detail: "Recorded in PostgreSQL",
              },
              {
                label: "Policy blocks",
                value: (
                  data.counts.policy_blocks + data.counts.output_blocks
                ).toLocaleString(),
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

          <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
            <h2 className="text-[13px] font-medium text-foreground">
              Administrative audit events
            </h2>
            <p className="font-mono text-[11px] tabular-nums text-muted-foreground">
              {new Date(data.period_start).toLocaleDateString()} –{" "}
              {new Date(data.period_end).toLocaleDateString()} · Generated{" "}
              {new Date(data.generated_at).toLocaleTimeString()}
            </p>
          </div>

          <div className="border-y border-border-subtle">
            {data.events.length === 0 ? (
              <EmptyState
                icon={<FileCheck className="size-5" />}
                title="No administrative events in this period"
                description="Workspace configuration changes, member invites, and policy updates will appear here."
              />
            ) : (
              <div
                className="overflow-x-auto"
                tabIndex={0}
                role="region"
                aria-label="Administrative audit events"
              >
                <table className="w-full min-w-[680px] text-left text-xs">
                  <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
                    <tr>
                      <th
                        scope="col"
                        className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
                      >
                        Time
                      </th>
                      <th
                        scope="col"
                        className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                      >
                        Actor
                      </th>
                      <th
                        scope="col"
                        className="sticky top-0 bg-background px-4 py-2.5 font-normal"
                      >
                        Action
                      </th>
                      <th
                        scope="col"
                        className="sticky top-0 bg-background py-2.5 pl-4 font-normal"
                      >
                        Resource
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {data.events.map((e) => (
                      <tr
                        key={e.id}
                        className="transition-colors hover:bg-surface-1/60"
                      >
                        <td className="whitespace-nowrap py-3 pr-4 font-mono tabular-nums text-muted-foreground">
                          {new Date(e.created_at).toLocaleString()}
                        </td>
                        <td className="px-4 py-3 text-foreground">
                          {e.actor_email}
                        </td>
                        <td className="px-4 py-3 font-mono text-foreground">
                          {e.action}
                        </td>
                        <td className="py-3 pl-4 text-muted-foreground">
                          {e.target_type}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          {data.events_truncated ? (
            <p className="mt-3 text-xs text-muted-foreground">
              Showing the latest 1,000 of {data.events_total.toLocaleString()}{" "}
              events. Use the evidence API date range to export smaller periods.
            </p>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
