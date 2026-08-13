import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ChevronRight,
  FileCode2,
  HeartPulse,
  Layers3,
  Plus,
  Search,
  ShieldCheck,
  Sparkles,
  Terminal,
} from "lucide-react";

import { overlayApi, overlayKeys, type Overlay } from "@/api/overlay";
import { AdminSummaryStrip, SecurityNotice } from "@/components/admin/admin-primitives";
import { EmptyState, PageHeader, SectionHeader } from "@/components/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { SkeletonRows } from "@/components/skeleton";
import { cn } from "@/lib/utils";

type PolicyView = "all" | "active" | "inactive";

const FEATURED_TEMPLATES = [
  {
    slug: "healthcare",
    name: "Healthcare PHI Shield",
    description: "PII tokenization and indirect-injection controls for protected health data.",
    icon: HeartPulse,
  },
  {
    slug: "fintech",
    name: "Financial Services Guardrail",
    description: "Account identifiers, payment data, secrets, and high-risk access patterns.",
    icon: ShieldCheck,
  },
  {
    slug: "code_assistant",
    name: "Code Execution Guard",
    description: "Destructive shell commands, path traversal, and code-injection controls.",
    icon: Terminal,
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
    <div className="space-y-5">
      <PageHeader
        title="Custom Policies"
        description="Add domain-specific controls on top of the gateway's managed security profile, then promote them deliberately into production."
        action={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => navigate({ to: "/overlay-templates" })}>
              <Sparkles className="size-3.5" aria-hidden />
              Templates
            </Button>
            <Button size="sm" onClick={() => createPolicy()}>
              <Plus className="size-3.5" aria-hidden />
              New policy
            </Button>
          </div>
        }
      />

      <AdminSummaryStrip
        items={[
          { label: "Policies", value: policies.length, detail: "Configured in this workspace" },
          { label: "Enforcing", value: activeCount, detail: "Active production versions", tone: "success" },
          { label: "Not enforcing", value: inactiveCount, detail: "Draft or inactive policies", tone: inactiveCount ? "warning" : "default" },
          { label: "Proxy scoped", value: scopedCount, detail: "Bound to a specific gateway" },
        ]}
      />

      <SecurityNotice title="Safe rollout is versioned by design" tone="success">
        Editing a policy does not silently change production enforcement. Create a version, preview it against test content, and activate it only after review.
      </SecurityNotice>

      <section>
        <SectionHeader
          title="Recommended baselines"
          description="Start with a maintained industry profile and adjust only the controls your application requires."
          action={
            <Button variant="ghost" size="sm" onClick={() => navigate({ to: "/overlay-templates" })}>
              View all templates
              <ChevronRight className="size-3.5" aria-hidden />
            </Button>
          }
        />
        <div className="grid gap-3 lg:grid-cols-3">
          {FEATURED_TEMPLATES.map((template) => {
            const Icon = template.icon;
            return (
              <button
                key={template.slug}
                type="button"
                onClick={() => createPolicy(template.slug)}
                className="group flex min-h-28 items-start gap-3 rounded-xl border border-border/70 bg-card p-4 text-left transition-colors hover:border-foreground/20 hover:bg-muted/20"
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/35 text-muted-foreground group-hover:text-foreground">
                  <Icon className="size-4" aria-hidden />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center justify-between gap-3 text-[13px] font-medium text-foreground">
                    {template.name}
                    <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                  </span>
                  <span className="mt-1.5 block text-[11px] leading-relaxed text-muted-foreground">
                    {template.description}
                  </span>
                </span>
              </button>
            );
          })}
        </div>
      </section>

      <section>
        <SectionHeader
          title="Policy inventory"
          description="Review deployment state, scope, and the last policy change."
        />
        <Card className="overflow-hidden border-border/70">
          <div className="flex flex-col gap-3 border-b border-border/60 p-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-1 rounded-lg border border-border/60 bg-muted/20 p-1">
              {(["all", "active", "inactive"] as const).map((item) => (
                <button
                  key={item}
                  type="button"
                  onClick={() => setView(item)}
                  className={cn(
                    "rounded-md px-3 py-1.5 text-[11px] font-medium capitalize transition-colors",
                    view === item ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {item}
                </button>
              ))}
            </div>
            <div className="relative w-full sm:w-72">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search policies"
                className="h-8 pl-8 text-xs"
              />
            </div>
          </div>
          <CardContent className="p-0">
            {overlays.isLoading ? (
              <SkeletonRows count={5} />
            ) : overlays.isError ? (
              <EmptyState
                icon={<Layers3 className="size-5" />}
                title="Policies could not be loaded"
                description="Refresh the page or verify that the gateway API is reachable."
              />
            ) : filteredPolicies.length === 0 ? (
              <EmptyState
                icon={<FileCode2 className="size-5" />}
                title={policies.length === 0 ? "No custom policies yet" : "No policies match this view"}
                description={
                  policies.length === 0
                    ? "Start from a maintained template or create a policy from a real threat event."
                    : "Change the status filter or search term to widen the result set."
                }
                action={policies.length === 0 ? <Button size="sm" onClick={() => createPolicy()}>Create first policy</Button> : undefined}
              />
            ) : (
              <PolicyTable
                policies={filteredPolicies}
                onOpen={(policy) => navigate({ to: "/overlays/$id", params: { id: policy.id } })}
              />
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function PolicyTable({ policies, onOpen }: { policies: Overlay[]; onOpen: (policy: Overlay) => void }) {
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead>Policy</TableHead>
          <TableHead>Scope</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Updated</TableHead>
          <TableHead className="w-12" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {policies.map((policy) => (
          <TableRow key={policy.id} className="group cursor-pointer" onClick={() => onOpen(policy)}>
            <TableCell>
              <div className="max-w-xl">
                <p className="text-[12px] font-medium text-foreground">{policy.name}</p>
                <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
                  {policy.description || "No policy description"}
                </p>
              </div>
            </TableCell>
            <TableCell className="font-mono text-[11px] text-muted-foreground">
              {policy.proxy_id ? "Single proxy" : "Workspace default"}
            </TableCell>
            <TableCell>
              {policy.active_version_id ? (
                <Badge variant="outline" className="border-success-border bg-success-bg text-success">Enforcing</Badge>
              ) : (
                <Badge variant="outline">Not enforcing</Badge>
              )}
            </TableCell>
            <TableCell className="font-mono text-[11px] text-muted-foreground">
              {formatUpdatedAt(policy.updated_at)}
            </TableCell>
            <TableCell>
              <ChevronRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" aria-hidden />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function formatUpdatedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
