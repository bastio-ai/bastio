import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ChevronRight,
  FileCode2,
  Layers3,
  Plus,
  Search,
  Sparkles,
} from "lucide-react";

import { overlayApi, overlayKeys, type Overlay } from "@/api/overlay";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  AdminUnderlineNav,
} from "@/components/admin/admin-primitives";
import { EmptyState } from "@/components/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SkeletonRows } from "@/components/skeleton";
import { cn } from "@/lib/utils";

type PolicyView = "all" | "active" | "inactive";

const FEATURED_TEMPLATES = [
  {
    slug: "healthcare",
    name: "Healthcare PHI Shield",
    description:
      "PII tokenization and indirect-injection controls for protected health data.",
  },
  {
    slug: "fintech",
    name: "Financial Services Guardrail",
    description:
      "Account identifiers, payment data, secrets, and high-risk access patterns.",
  },
  {
    slug: "code_assistant",
    name: "Code Execution Guard",
    description:
      "Destructive shell commands, path traversal, and code-injection controls.",
  },
] as const;

export function OverlaysPage() {
  const navigate = useNavigate();
  const [view, setView] = useState<PolicyView>("all");
  const [query, setQuery] = useState("");

  const overlays = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
  });

  const policies = overlays.data ?? [];
  const activeCount = policies.filter((policy) => policy.active_version_id).length;
  const inactiveCount = policies.length - activeCount;
  const scopedCount = policies.filter((policy) => policy.proxy_id).length;

  const filteredPolicies = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return policies.filter((policy) => {
      const matchesView =
        view === "all" ||
        (view === "active" && !!policy.active_version_id) ||
        (view === "inactive" && !policy.active_version_id);
      const matchesQuery =
        normalizedQuery.length === 0 ||
        policy.name.toLowerCase().includes(normalizedQuery) ||
        policy.description.toLowerCase().includes(normalizedQuery);
      return matchesView && matchesQuery;
    });
  }, [policies, query, view]);

  const createPolicy = (template?: string) =>
    navigate({
      to: "/overlays/new",
      search: { template, from_threat: undefined },
    });

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Security & guardrails"
        title="Custom policies"
        description="Add domain-specific controls on top of the gateway's managed security profile, then promote them deliberately into production."
        bordered={false}
        className="mb-4"
        actions={
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => navigate({ to: "/overlay-templates" })}
            >
              <Sparkles className="size-3.5" aria-hidden="true" />
              Templates
            </Button>
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              onClick={() => createPolicy()}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              New policy
            </Button>
          </>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Policies",
            value: policies.length,
            detail: "Configured in this workspace",
          },
          {
            label: "Enforcing",
            value: activeCount,
            detail: "Active production versions",
            tone: "success",
          },
          {
            label: "Not enforcing",
            value: inactiveCount,
            detail: "Draft or inactive policies",
            tone: inactiveCount ? "warning" : "default",
          },
          {
            label: "Proxy scoped",
            value: scopedCount,
            detail: "Bound to a specific gateway",
          },
        ]}
      />

      <div className="mb-6 border-l-2 border-success/60 pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">
          Safe rollout is versioned by design.
        </span>{" "}
        Editing a policy does not silently change production enforcement. Create a
        version, preview it against sample content, and activate it only after
        review.
      </div>

      <section className="mb-8">
        <div className="mb-3 flex flex-wrap items-baseline justify-between gap-3">
          <div>
            <h2 className="text-[13px] font-medium text-foreground">
              Recommended baselines
            </h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              Start with a maintained industry profile and adjust only the
              controls your application requires.
            </p>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => navigate({ to: "/overlay-templates" })}
          >
            View all templates
            <ChevronRight className="size-3.5" aria-hidden="true" />
          </Button>
        </div>

        <div className="divide-y divide-border-subtle border-y border-border-subtle lg:grid lg:grid-cols-3 lg:divide-x lg:divide-y-0">
          {FEATURED_TEMPLATES.map((template) => (
            <button
              key={template.slug}
              type="button"
              onClick={() => createPolicy(template.slug)}
              className="group flex w-full flex-col justify-between px-4 py-4 text-left transition-colors hover:bg-surface-1/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <div>
                <div className="flex items-center justify-between gap-3">
                  <span className="text-[13px] font-medium text-foreground">
                    {template.name}
                  </span>
                  <ChevronRight
                    className="size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground"
                    aria-hidden="true"
                  />
                </div>
                <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                  {template.description}
                </p>
              </div>
              <span className="mt-3 font-mono text-[11px] text-muted-foreground">
                template:{template.slug}
              </span>
            </button>
          ))}
        </div>
      </section>

      <section>
        <div className="mb-3">
          <h2 className="text-[13px] font-medium text-foreground">
            Policy inventory
          </h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Review deployment state, scope, and the last policy change.
          </p>
        </div>

        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <AdminUnderlineNav
            ariaLabel="Filter custom policies by state"
            value={view}
            onChange={setView}
            className="mb-0 flex-1"
            items={[
              { id: "all", label: "All", count: policies.length },
              { id: "active", label: "Enforcing", count: activeCount },
              { id: "inactive", label: "Not enforcing", count: inactiveCount },
            ]}
          />
          <search className="w-full sm:w-72">
            <label htmlFor="custom-policies-search" className="sr-only">
              Search policies
            </label>
            <div className="relative">
              <Search
                className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
                aria-hidden="true"
              />
              <Input
                id="custom-policies-search"
                type="search"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search policies…"
                className="h-8 pl-8 text-xs"
              />
            </div>
          </search>
        </div>

        <div className="border-y border-border-subtle">
          {overlays.isLoading ? (
            <div className="py-4">
              <SkeletonRows count={5} />
            </div>
          ) : overlays.isError ? (
            <EmptyState
              icon={<Layers3 className="size-5" />}
              title="Policies could not be loaded"
              description="Refresh the page or verify that the gateway API is reachable."
            />
          ) : filteredPolicies.length === 0 ? (
            <EmptyState
              icon={<FileCode2 className="size-5" />}
              title={
                policies.length === 0
                  ? "No custom policies yet"
                  : "No policies match this view"
              }
              description={
                policies.length === 0
                  ? "Start from a maintained template or create a policy from a flagged threat event."
                  : "Change the status filter or search term to widen the result set."
              }
              action={
                policies.length === 0 ? (
                  <Button
                    type="button"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => createPolicy()}
                  >
                    Create first policy
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <PolicyTable policies={filteredPolicies} />
          )}
        </div>
      </section>
    </div>
  );
}

function PolicyTable({ policies }: { policies: Overlay[] }) {
  return (
    <div
      className="overflow-x-auto"
      tabIndex={0}
      role="region"
      aria-label="Custom policies"
    >
      <table className="w-full min-w-[680px] text-left text-xs">
        <thead className="border-b border-border-subtle text-[11px] text-muted-foreground">
          <tr>
            <th
              scope="col"
              className="sticky top-0 bg-background py-2.5 pr-4 font-normal"
            >
              Policy
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 font-normal"
            >
              Scope
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background px-4 py-2.5 font-normal"
            >
              State
            </th>
            <th
              scope="col"
              className="sticky top-0 bg-background py-2.5 pl-4 text-right font-normal"
            >
              Updated
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {policies.map((policy) => {
            const enforcing = Boolean(policy.active_version_id);
            return (
              <tr
                key={policy.id}
                className="group transition-colors hover:bg-surface-1/60"
              >
                <td className="py-3 pr-4">
                  <Link
                    to="/overlays/$id"
                    params={{ id: policy.id }}
                    className="inline-flex max-w-xl flex-col focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span className="inline-flex items-center gap-1.5 font-mono text-xs font-medium text-foreground group-hover:underline">
                      {policy.name}
                      <ChevronRight
                        className="size-3 text-muted-foreground transition-transform group-hover:translate-x-0.5"
                        aria-hidden="true"
                      />
                    </span>
                    <span className="mt-0.5 truncate text-[11px] text-muted-foreground">
                      {policy.description || "No policy description"}
                    </span>
                  </Link>
                </td>
                <td className="px-4 py-3 font-mono text-[11px] text-muted-foreground">
                  {policy.proxy_id ? "Single proxy" : "Workspace default"}
                </td>
                <td className="px-4 py-3">
                  <span className="inline-flex items-center gap-2 text-xs">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 rounded-full",
                        enforcing ? "bg-success" : "bg-muted-foreground/50",
                      )}
                    />
                    <span
                      className={cn(
                        enforcing ? "text-foreground" : "text-muted-foreground",
                      )}
                    >
                      {enforcing ? "Enforcing" : "Not enforcing"}
                    </span>
                  </span>
                </td>
                <td className="py-3 pl-4 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                  {formatUpdatedAt(policy.updated_at)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function formatUpdatedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}
