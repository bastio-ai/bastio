import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Activity } from "lucide-react";

import { gatewayBasePath, providerLabels } from "@/components/gateway-setup";
import { api } from "@/api/client";
import type { Trace } from "@/api/client";
import { EmptyState } from "@/components/card";
import {
  AdminPageHeader,
  AdminSummaryStrip,
} from "@/components/admin/admin-primitives";
import { TraceStatus } from "@/components/observe/trace-status";
import { SkeletonRows } from "@/components/skeleton";
import { cn, formatCost, formatDuration } from "@/lib/utils";

export function ProxyDetailPage() {
  const { id } = useParams({ from: "/proxies/$id" });

  const proxy = useQuery({
    queryKey: ["proxy", id],
    queryFn: () => api.proxies.get(id),
  });

  const traces = useQuery({
    queryKey: ["traces", { proxy_id: id, limit: 50 }],
    queryFn: () => api.traces.list({ proxy_id: id, limit: 50 }),
  });

  if (proxy.isLoading) {
    return (
      <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
        <SkeletonRows count={5} />
      </div>
    );
  }
  if (proxy.error || !proxy.data) {
    return (
      <div className="mx-auto max-w-[1180px] space-y-5 pb-10 pt-2 md:pt-4">
        <BackLink id={id} />
        <div className="border-y border-border-subtle">
          <EmptyState
            icon={<Activity className="size-5" />}
            title="Gateway not found"
            description="This gateway no longer exists, or belongs to another workspace."
          />
        </div>
      </div>
    );
  }

  const p = proxy.data;
  const nativeBasePath = gatewayBasePath(p);
  const rows = traces.data ?? [];

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="LLM gateway"
        title={p.name}
        bordered={false}
        className="mb-4"
        description={
          <span>
            Requests entering{" "}
            <code className="font-mono text-foreground">{nativeBasePath}</code>{" "}
            are routed to{" "}
            {providerLabels[p.target_provider] ?? p.target_provider} with the
            controls configured for this gateway.
          </span>
        }
        badge={
          <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 rounded-full",
                p.is_active ? "bg-success" : "bg-muted-foreground/40",
              )}
            />
            {p.is_active ? "Active" : "Disabled"}
          </span>
        }
        actions={<BackLink id={id} />}
      />

      <AdminSummaryStrip
        items={[
          { label: "SDK base path", value: nativeBasePath, detail: p.slug },
          {
            label: "Provider",
            value: providerLabels[p.target_provider] ?? p.target_provider,
            detail: "Upstream target",
          },
          {
            label: "Default model",
            value: p.target_model || "Request supplied",
            detail: p.target_model ? "Gateway default" : "No forced model",
          },
          {
            label: "Recent traces",
            value: rows.length,
            detail: `Created ${new Date(p.created_at).toLocaleDateString()}`,
            tone: rows.length ? "success" : "default",
          },
        ]}
      />

      <section aria-label="Recent gateway traces">
        <div className="mb-3 flex items-baseline justify-between gap-4">
          <h2 className="text-[13px] font-medium text-foreground">
            Recent traces
          </h2>
          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
            {rows.length} shown
          </span>
        </div>

        {traces.isLoading ? (
          <SkeletonRows count={4} />
        ) : rows.length === 0 ? (
          <div className="border-y border-border-subtle">
            <EmptyState
              icon={<Activity className="size-5" />}
              title="No requests through this gateway yet"
              description="Use the connection example in gateway setup, then return here to inspect the result."
            />
          </div>
        ) : (
          <div
            tabIndex={0}
            role="region"
            aria-label="Recent gateway traces"
            className="overflow-x-auto border-y border-border-subtle"
          >
            <table className="w-full min-w-[640px] border-collapse text-left text-[13px]">
              <thead>
                <tr className="border-b border-border-subtle text-[11px] text-muted-foreground">
                  <th scope="col" className="sticky top-0 bg-background py-2.5 pr-4 font-normal">
                    Status
                  </th>
                  <th scope="col" className="sticky top-0 bg-background px-3 py-2.5 font-normal">
                    Model
                  </th>
                  <th scope="col" className="sticky top-0 bg-background px-3 py-2.5 text-right font-normal">
                    Tokens
                  </th>
                  <th scope="col" className="sticky top-0 bg-background px-3 py-2.5 text-right font-normal">
                    Cost
                  </th>
                  <th scope="col" className="sticky top-0 bg-background px-3 py-2.5 text-right font-normal">
                    Latency
                  </th>
                  <th scope="col" className="sticky top-0 bg-background py-2.5 pl-3 text-right font-normal">
                    Time
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {rows.map((t: Trace) => (
                  <tr
                    key={t.id}
                    className="transition-colors hover:bg-surface-2/50"
                  >
                    <td className="py-2.5 pr-4">
                      <TraceStatus status={t.status} />
                    </td>
                    <td className="px-3 py-2.5 font-mono text-xs">
                      <Link
                        to="/traces/$id"
                        params={{ id: t.id }}
                        className="text-foreground underline-offset-4 hover:underline"
                      >
                        {t.model}
                      </Link>
                    </td>
                    <td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {(t.input_tokens + t.output_tokens).toLocaleString()}
                    </td>
                    <td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {formatCost(t.cost_cents)}
                    </td>
                    <td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {formatDuration(t.duration_ms)}
                    </td>
                    <td className="py-2.5 pl-3 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {new Date(t.started_at).toLocaleTimeString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function BackLink({ id }: { id?: string }) {
  return (
    <Link
      to="/proxies"
      search={{ gateway: id }}
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      <ArrowLeft className="size-3.5" aria-hidden="true" /> Back to gateways
    </Link>
  );
}
