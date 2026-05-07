import { Download, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export type ObserveFilters = {
  status: string;
  provider: string;
  model: string;
  endUser: string;
  search: string;
  security: string;
  from: string;
  to: string;
  environment: string;
  release: string;
  traceName: string;
  // Tag filter: space- or comma-separated `key:value` pairs.
  tags: string;
};

export const emptyFilters: ObserveFilters = {
  status: "",
  provider: "",
  model: "",
  endUser: "",
  search: "",
  security: "",
  from: "",
  to: "",
  environment: "",
  release: "",
  traceName: "",
  tags: "",
};

type Props = {
  value: ObserveFilters;
  onChange: (next: ObserveFilters) => void;
  onCSV?: () => void;
  showStatus?: boolean;
  showSecurity?: boolean;
  // Pre-seen environments from the current result set, so the env select
  // offers relevant options rather than requiring free-text guessing.
  environments?: string[];
};

// Shared filter bar for Traces and Sessions lists. Status and security
// filters can be hidden so the same component renders on surfaces where
// they don't apply.
export function FilterBar({
  value,
  onChange,
  onCSV,
  showStatus = true,
  showSecurity = true,
  environments = [],
}: Props) {
  const active = Object.values(value).some((v) => v !== "");
  const set = <K extends keyof ObserveFilters>(k: K, v: ObserveFilters[K]) =>
    onChange({ ...value, [k]: v });
  return (
    <Card className="border-border/50 mb-3">
      <CardContent className="space-y-2 p-3">
        <div className="flex flex-wrap items-center gap-2">
          {showStatus ? (
            <Select
              value={value.status || "all"}
              onValueChange={(v) => set("status", !v || v === "all" ? "" : v)}
            >
              <SelectTrigger className="h-8 w-36 text-xs">
                <SelectValue placeholder="All statuses" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
                <SelectItem value="ok">ok</SelectItem>
                <SelectItem value="error">error</SelectItem>
                <SelectItem value="blocked">blocked</SelectItem>
                <SelectItem value="rate_limited">rate_limited</SelectItem>
              </SelectContent>
            </Select>
          ) : null}
          {showSecurity ? (
            <Select
              value={value.security || "any"}
              onValueChange={(v) => set("security", !v || v === "any" ? "" : v)}
            >
              <SelectTrigger className="h-8 w-36 text-xs">
                <SelectValue placeholder="Security" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="any">Any security</SelectItem>
                <SelectItem value="clean">Clean</SelectItem>
                <SelectItem value="threat">Threat detected</SelectItem>
              </SelectContent>
            </Select>
          ) : null}
          <Select
            value={value.environment || "all"}
            onValueChange={(v) => set("environment", !v || v === "all" ? "" : v)}
          >
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue placeholder="Environment" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All environments</SelectItem>
              {environments
                .filter((e) => e)
                .map((e) => (
                  <SelectItem key={e} value={e}>
                    {e}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <Input
            placeholder="Provider"
            value={value.provider}
            onChange={(e) => set("provider", e.target.value)}
            className="h-8 w-32 text-xs"
          />
          <Input
            placeholder="Model"
            value={value.model}
            onChange={(e) => set("model", e.target.value)}
            className="h-8 w-32 text-xs"
          />
          <Input
            placeholder="End-user id"
            value={value.endUser}
            onChange={(e) => set("endUser", e.target.value)}
            className="h-8 w-36 text-xs"
          />
          <Input
            placeholder="Search..."
            value={value.search}
            onChange={(e) => set("search", e.target.value)}
            className="h-8 flex-1 min-w-[10rem] text-xs"
          />
          <Button
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => onChange(emptyFilters)}
            disabled={!active}
          >
            <X className="h-3 w-3" /> Clear
          </Button>
          {onCSV ? (
            <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onCSV}>
              <Download className="h-3 w-3" /> CSV
            </Button>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            placeholder="Trace name (e.g. guard_chat)"
            value={value.traceName}
            onChange={(e) => set("traceName", e.target.value)}
            className="h-8 w-52 text-xs"
          />
          <Input
            placeholder="Release (e.g. v1.4.0)"
            value={value.release}
            onChange={(e) => set("release", e.target.value)}
            className="h-8 w-40 text-xs"
          />
          <Input
            placeholder="Tags: feature:checkout,tier:pro"
            value={value.tags}
            onChange={(e) => set("tags", e.target.value)}
            className="h-8 flex-1 min-w-[12rem] text-xs"
          />
          <Input
            type="datetime-local"
            value={value.from}
            onChange={(e) => set("from", e.target.value)}
            className="h-8 w-44 text-xs"
            aria-label="From"
          />
          <Input
            type="datetime-local"
            value={value.to}
            onChange={(e) => set("to", e.target.value)}
            className="h-8 w-44 text-xs"
            aria-label="To"
          />
        </div>
      </CardContent>
    </Card>
  );
}

// parseTagFilter turns "feature:checkout,tier:pro" into the repeatable
// `tag` query-param array expected by the /v1/traces endpoint.
export function parseTagFilter(raw: string): string[] {
  if (!raw) return [];
  const out: string[] = [];
  for (const piece of raw.split(/[,\s]+/)) {
    const trimmed = piece.trim();
    if (trimmed && trimmed.includes(":")) out.push(trimmed);
  }
  return out;
}
