import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CheckCircle2,
  ChevronDown,
  Download,
  FileCheck2,
  Search,
  ShieldCheck,
} from "lucide-react";

import { AdminSummaryStrip, SecurityNotice } from "@/components/admin/admin-primitives";
import { EmptyState, PageHeader, SectionHeader } from "@/components/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

interface ComplianceControl {
  standard: string;
  article: string;
  control_name: string;
  status: string;
  evidence: string;
}

interface ComplianceReport {
  generated_at: string;
  platform: string;
  version: string;
  summary: {
    total_requests_scanned: number;
    threats_blocked: number;
    pii_redactions: number;
    cost_savings_usd: number;
    cache_hit_ratio: number;
  };
  compliance_controls: ComplianceControl[];
}

export function CompliancePage() {
  const [downloading, setDownloading] = useState(false);
  const [query, setQuery] = useState("");
  const [standard, setStandard] = useState("all");

  const report = useQuery<ComplianceReport>({
    queryKey: ["compliance-report"],
    queryFn: async () => {
      const response = await fetch("/v1/audit/export");
      if (!response.ok) throw new Error("Failed to load compliance report");
      return response.json();
    },
  });

  const controls = report.data?.compliance_controls ?? [];
  const standards = useMemo(
    () => Array.from(new Set(controls.map((control) => control.standard))).sort(),
    [controls],
  );
  const filteredControls = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return controls.filter((control) => {
      const matchesStandard = standard === "all" || control.standard === standard;
      const matchesQuery =
        normalizedQuery.length === 0 ||
        control.control_name.toLowerCase().includes(normalizedQuery) ||
        control.article.toLowerCase().includes(normalizedQuery) ||
        control.evidence.toLowerCase().includes(normalizedQuery);
      return matchesStandard && matchesQuery;
    });
  }, [controls, query, standard]);

  const verifiedCount = controls.filter((control) => control.status.toLowerCase() === "compliant").length;
  const summary = report.data?.summary;
  const evidenceAvailable = Boolean(summary && controls.length > 0);

  const handleDownload = () => {
    setDownloading(true);
    window.location.assign("/v1/audit/export?format=json");
    window.setTimeout(() => setDownloading(false), 1500);
  };

  return (
    <div className="space-y-5">
      <PageHeader
        title="Compliance & Audit"
        description="Review control mappings and export traceable evidence from the gateway's recorded security activity."
        action={
          <Button onClick={handleDownload} disabled={downloading || report.isError || !evidenceAvailable} size="sm">
            <Download className="size-3.5" aria-hidden />
            {downloading ? "Preparing export…" : "Export JSON report"}
          </Button>
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Requests audited",
            value: formatMetric(summary?.total_requests_scanned),
            detail: "Included in the current report",
          },
          {
            label: "Threats blocked",
            value: formatMetric(summary?.threats_blocked),
            detail: "Prevented before provider forwarding",
            tone: "success",
          },
          {
            label: "PII transformations",
            value: formatMetric(summary?.pii_redactions),
            detail: "Masked or tokenized values",
          },
          {
            label: "Controls verified",
            value: controls.length ? `${verifiedCount}/${controls.length}` : "—",
            detail: "Backed by exported evidence",
            tone: verifiedCount === controls.length && controls.length ? "success" : "default",
          },
        ]}
      />

      {report.isError || (!report.isPending && !evidenceAvailable) ? (
        <SecurityNotice title={report.isError ? "Evidence service is unavailable" : "No audit evidence has been generated"} tone="warning">
          The page is not displaying cached or invented compliance claims. Generate auditable gateway activity and restore the export pipeline before downloading a report.
        </SecurityNotice>
      ) : (
        <SecurityNotice title="Evidence reflects the active deployment" tone="info">
          This report is generated from recorded gateway activity. A compliant mapping supports an audit; it does not by itself certify the organization or replace an independent assessment.
        </SecurityNotice>
      )}

      <Card className="border-border/70">
        <CardContent className="flex flex-col gap-4 p-4 md:flex-row md:items-center md:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
              <ShieldCheck className="size-4" aria-hidden />
            </span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-[13px] font-medium text-foreground">Evidence provenance</h2>
                <Badge variant="outline">{evidenceAvailable ? report.data?.platform || "Current gateway" : "Evidence pending"}</Badge>
              </div>
              <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                {evidenceAvailable ? `Generated ${formatDateTime(report.data?.generated_at)}` : "No report metadata is available yet"}
                {report.data?.version ? ` · gateway ${report.data.version}` : ""}
              </p>
            </div>
          </div>
          <div className="font-mono text-[11px] text-muted-foreground">
            JSON · machine-readable · point-in-time
          </div>
        </CardContent>
      </Card>

      <section>
        <SectionHeader
          title="Control mapping"
          description="Each row links a requirement to the operational evidence produced by this deployment."
          action={<span className="font-mono text-[11px] text-muted-foreground">{filteredControls.length} shown</span>}
        />
        <Card className="overflow-hidden border-border/70">
          <div className="flex flex-col gap-3 border-b border-border/60 p-3 sm:flex-row">
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search controls, articles, or evidence"
                className="h-8 pl-8 text-xs"
              />
            </div>
            <label className="relative sm:w-60">
              <span className="sr-only">Filter by standard</span>
              <select
                value={standard}
                onChange={(event) => setStandard(event.target.value)}
                className="h-8 w-full appearance-none rounded-md border border-border bg-background px-3 pr-8 text-xs text-foreground outline-none focus:ring-1 focus:ring-ring"
              >
                <option value="all">All standards</option>
                {standards.map((item) => <option key={item} value={item}>{item}</option>)}
              </select>
              <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
            </label>
          </div>

          {report.isPending ? (
            <div className="space-y-0 divide-y divide-border/50">
              {[0, 1, 2, 3].map((item) => <div key={item} className="h-24 animate-pulse bg-muted/10" />)}
            </div>
          ) : filteredControls.length === 0 ? (
            <EmptyState
              icon={<FileCheck2 className="size-5" />}
              title={controls.length ? "No controls match this view" : "No control evidence available"}
              description={controls.length ? "Change the search or standard filter." : "Generate gateway activity and restore the audit export service to populate this matrix."}
            />
          ) : (
            <div className="divide-y divide-border/60">
              {filteredControls.map((control) => (
                <ControlRow key={`${control.standard}-${control.article}-${control.control_name}`} control={control} />
              ))}
            </div>
          )}
        </Card>
      </section>
    </div>
  );
}

function ControlRow({ control }: { control: ComplianceControl }) {
  const compliant = control.status.toLowerCase() === "compliant";
  return (
    <article className="grid gap-3 p-4 lg:grid-cols-[220px_minmax(0,1fr)_120px] lg:items-start">
      <div>
        <div className="flex items-center gap-2">
          <CheckCircle2 className={cn("size-3.5", compliant ? "text-success" : "text-muted-foreground")} aria-hidden />
          <Badge variant="secondary">{control.standard}</Badge>
        </div>
        <p className="mt-2 font-mono text-[11px] text-muted-foreground">{control.article}</p>
      </div>
      <div className="min-w-0">
        <h3 className="text-[12px] font-medium text-foreground">{control.control_name}</h3>
        <p className="mt-1.5 text-[11px] leading-relaxed text-muted-foreground">{control.evidence}</p>
      </div>
      <Badge
        variant="outline"
        className={cn("justify-self-start lg:justify-self-end", compliant && "border-success-border bg-success-bg text-success")}
      >
        {control.status}
      </Badge>
    </article>
  );
}

function formatMetric(value: number | undefined): string {
  return typeof value === "number" ? value.toLocaleString() : "—";
}

function formatDateTime(value: string | undefined): string {
  if (!value) return "when the report is requested";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
