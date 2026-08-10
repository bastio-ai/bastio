import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  CheckCircle2,
  ChevronRight,
  Eye,
  FileCode,
  HeartPulse,
  Layers,
  Plus,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Terminal,
} from "lucide-react";

import { overlayApi, overlayKeys, type Overlay } from "@/api/overlay";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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

export function OverlaysPage() {
  const navigate = useNavigate();
  const [filter, setFilter] = useState<"all" | "active" | "shadow" | "draft">("all");

  const { data: overlays = [], isLoading } = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
  });

  const activeCount = overlays.filter((o) => o.active_version_id).length;
  const shadowCount = overlays.filter((o) => (o as any).shadow_version_id).length;
  const draftCount = overlays.filter((o) => !o.active_version_id && !(o as any).shadow_version_id).length;

  const filteredOverlays = overlays.filter((o) => {
    if (filter === "active") return !!o.active_version_id;
    if (filter === "shadow") return !!(o as any).shadow_version_id;
    if (filter === "draft") return !o.active_version_id && !(o as any).shadow_version_id;
    return true;
  });

  const featuredTemplates = [
    {
      slug: "healthcare",
      name: "Healthcare PHI Shield",
      description: "Auto-tokenize medical records, ICD codes, SSNs, and patient identifiers.",
      icon: HeartPulse,
      color: "text-emerald-500 bg-emerald-500/10 border-emerald-500/20",
    },
    {
      slug: "fintech",
      name: "Financial Compliance",
      description: "Block insider trading queries, credit card numbers, and banking credentials.",
      icon: ShieldCheck,
      color: "text-blue-500 bg-blue-500/10 border-blue-500/20",
    },
    {
      slug: "code_assistant",
      name: "Exec Command Guard",
      description: "Prevent destructive shell commands (rm -rf), SQL injection & path traversal.",
      icon: Terminal,
      color: "text-amber-500 bg-amber-500/10 border-amber-500/20",
    },
    {
      slug: "customer_support",
      name: "Customer Support Shield",
      description: "PII masking & raw exfiltration protection for support bots.",
      icon: ShieldAlert,
      color: "text-purple-500 bg-purple-500/10 border-purple-500/20",
    },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-end justify-between gap-4">
        <PageHeader
          title="Custom Policies"
          description="Layer domain-specific guardrails, custom regexes, and vertical compliance rules on top of core security profiles. Shadow-test on live traffic before activating."
        />
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs gap-1.5"
            onClick={() => navigate({ to: "/overlay-templates" })}
          >
            <Sparkles className="h-3.5 w-3.5 text-amber-500" /> Browse Templates
          </Button>
          <Button
            size="sm"
            className="h-8 text-xs gap-1.5"
            onClick={() =>
              navigate({
                to: "/overlays/new",
                search: { template: undefined, from_threat: undefined },
              })
            }
          >
            <Plus className="h-3.5 w-3.5" /> New Custom Policy
          </Button>
        </div>
      </div>

      {/* KPI Metrics */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Card
          className={`border-border/50 cursor-pointer transition-all hover:border-border ${
            filter === "all" ? "bg-accent/40 ring-1 ring-border" : ""
          }`}
          onClick={() => setFilter("all")}
        >
          <CardContent className="p-3.5">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                Total Policies
              </span>
              <Layers className="h-4 w-4 text-muted-foreground/60" />
            </div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-bold tracking-tight">{overlays.length}</span>
              <span className="text-[11px] text-muted-foreground">configured</span>
            </div>
          </CardContent>
        </Card>

        <Card
          className={`border-border/50 cursor-pointer transition-all hover:border-border ${
            filter === "active" ? "bg-emerald-500/5 ring-1 ring-emerald-500/30" : ""
          }`}
          onClick={() => setFilter("active")}
        >
          <CardContent className="p-3.5">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-medium uppercase tracking-wider text-emerald-600 dark:text-emerald-400">
                Active Production
              </span>
              <CheckCircle2 className="h-4 w-4 text-emerald-500" />
            </div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-bold tracking-tight text-emerald-600 dark:text-emerald-400">
                {activeCount}
              </span>
              <span className="text-[11px] text-muted-foreground">enforcing live</span>
            </div>
          </CardContent>
        </Card>

        <Card
          className={`border-border/50 cursor-pointer transition-all hover:border-border ${
            filter === "shadow" ? "bg-amber-500/5 ring-1 ring-amber-500/30" : ""
          }`}
          onClick={() => setFilter("shadow")}
        >
          <CardContent className="p-3.5">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-medium uppercase tracking-wider text-amber-600 dark:text-amber-400">
                Shadow Testing
              </span>
              <Eye className="h-4 w-4 text-amber-500" />
            </div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-bold tracking-tight text-amber-600 dark:text-amber-400">
                {shadowCount}
              </span>
              <span className="text-[11px] text-muted-foreground">background evaluation</span>
            </div>
          </CardContent>
        </Card>

        <Card
          className={`border-border/50 cursor-pointer transition-all hover:border-border ${
            filter === "draft" ? "bg-slate-500/5 ring-1 ring-slate-500/30" : ""
          }`}
          onClick={() => setFilter("draft")}
        >
          <CardContent className="p-3.5">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                Draft / Inactive
              </span>
              <FileCode className="h-4 w-4 text-muted-foreground/60" />
            </div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-bold tracking-tight">{draftCount}</span>
              <span className="text-[11px] text-muted-foreground">in staging</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Quick-Start Vertical Templates Showcase */}
      <Card className="border-border/50 bg-muted/10 overflow-hidden">
        <CardHeader className="p-4 pb-2 border-b border-border/40 flex flex-row items-center justify-between">
          <div className="space-y-0.5">
            <CardTitle className="text-xs font-semibold uppercase tracking-wider flex items-center gap-1.5">
              <Sparkles className="h-3.5 w-3.5 text-amber-500" /> Recommended Vertical Templates
            </CardTitle>
            <p className="text-[11px] text-muted-foreground">
              Deploy battle-tested industry security presets in 1 click. Customize rules before activating.
            </p>
          </div>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => navigate({ to: "/overlay-templates" })}
          >
            View all templates <ChevronRight className="h-3 w-3 ml-1" />
          </Button>
        </CardHeader>
        <CardContent className="p-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          {featuredTemplates.map((t) => {
            const IconComponent = t.icon;
            return (
              <div
                key={t.slug}
                className="group relative rounded-lg border border-border/50 bg-card p-3 space-y-2 hover:border-border transition-all hover:shadow-sm"
              >
                <div className="flex items-center justify-between">
                  <div className={`p-1.5 rounded-md border ${t.color}`}>
                    <IconComponent className="h-4 w-4" />
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="h-6 px-2 text-[10px] opacity-90 group-hover:opacity-100"
                    onClick={() =>
                      navigate({
                        to: "/overlays/new",
                        search: { template: t.slug, from_threat: undefined },
                      })
                    }
                  >
                    Use <ChevronRight className="h-2.5 w-2.5 ml-0.5" />
                  </Button>
                </div>
                <div>
                  <h4 className="text-xs font-medium group-hover:text-primary transition-colors">
                    {t.name}
                  </h4>
                  <p className="text-[11px] text-muted-foreground line-clamp-2 mt-0.5">
                    {t.description}
                  </p>
                </div>
              </div>
            );
          })}
        </CardContent>
      </Card>

      {/* Main Policy List Table */}
      <Card className="border-border/50 overflow-hidden">
        <CardHeader className="p-4 border-b border-border/40 flex flex-row items-center justify-between">
          <div className="flex items-center gap-2">
            <CardTitle className="text-xs font-semibold uppercase tracking-wider">
              Configured Custom Policies
            </CardTitle>
            {filter !== "all" && (
              <Badge variant="secondary" className="text-[10px] capitalize">
                Filtering: {filter}
              </Badge>
            )}
          </div>
          <span className="text-xs text-muted-foreground">
            Showing {filteredOverlays.length} of {overlays.length} policies
          </span>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={5} />
          ) : !filteredOverlays.length ? (
            <EmptyState
              icon={<Layers className="h-6 w-6" />}
              title={
                filter === "all"
                  ? "No custom policies configured"
                  : `No ${filter} custom policies found`
              }
              description="Add custom pattern rules, access restrictions, and detector overrides on top of your base security profile."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Policy Name
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Description
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Status & Version
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Last Modified
                  </TableHead>
                  <TableHead className="h-10 w-[80px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredOverlays.map((o: Overlay) => (
                  <TableRow
                    key={o.id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30 group"
                    onClick={() =>
                      navigate({ to: "/overlays/$id", params: { id: o.id } })
                    }
                  >
                    <TableCell className="font-mono text-xs font-medium group-hover:text-primary">
                      {o.name}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground max-w-[280px] truncate">
                      {o.description || "No description provided"}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        {o.active_version_id ? (
                          <Badge
                            variant="default"
                            className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-[10px] px-1.5 py-0"
                          >
                            <CheckCircle2 className="h-2.5 w-2.5 mr-1" /> Active
                          </Badge>
                        ) : (o as any).shadow_version_id ? (
                          <Badge
                            variant="secondary"
                            className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30 text-[10px] px-1.5 py-0"
                          >
                            <Eye className="h-2.5 w-2.5 mr-1" /> Shadow Mode
                          </Badge>
                        ) : (
                          <Badge
                            variant="outline"
                            className="text-muted-foreground text-[10px] px-1.5 py-0"
                          >
                            Draft
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground font-mono">
                      {new Date(o.updated_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 text-xs opacity-0 group-hover:opacity-100 transition-opacity"
                        onClick={(e) => {
                          e.stopPropagation();
                          navigate({ to: "/overlays/$id", params: { id: o.id } });
                        }}
                      >
                        Manage <ChevronRight className="h-3 w-3 ml-0.5" />
                      </Button>
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
