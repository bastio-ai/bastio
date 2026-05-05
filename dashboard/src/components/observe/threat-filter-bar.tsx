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

export type ThreatFilters = {
  severity: string;
  threatType: string;
  threatSubtype: string;
  detectorName: string;
  actionTaken: string;
  endUser: string;
  ipAddress: string;
  search: string;
  from: string;
  to: string;
};

export const emptyThreatFilters: ThreatFilters = {
  severity: "",
  threatType: "",
  threatSubtype: "",
  detectorName: "",
  actionTaken: "",
  endUser: "",
  ipAddress: "",
  search: "",
  from: "",
  to: "",
};

// Severity enum mirrors the ClickHouse values; keep in sync with the
// detector output in internal/security/detection.
export const SEVERITIES = ["critical", "high", "medium", "low"] as const;

// Threat types mirror the detector registry. "custom_pattern" is the
// fallback for user-defined patterns; keep the list ordered by how often
// operators triage each category.
export const THREAT_TYPES = [
  "jailbreak",
  "injection",
  "pii",
  "bot",
  "rate_anomaly",
  "custom_pattern",
] as const;

export const ACTIONS = ["block", "redact", "warn", "pass"] as const;

type Props = {
  value: ThreatFilters;
  onChange: (next: ThreatFilters) => void;
  onCSV?: () => void;
  // Detector names seen in the current result set so the free-text input
  // can be swapped for a datalist of known values. Optional because the
  // first render has no results yet.
  detectors?: string[];
  // Ref so the parent (ThreatsPage) can focus the search input with "/".
  searchInputRef?: React.RefObject<HTMLInputElement | null>;
};

export function ThreatFilterBar({
  value,
  onChange,
  onCSV,
  detectors = [],
  searchInputRef,
}: Props) {
  const active = Object.values(value).some((v) => v !== "");
  const set = <K extends keyof ThreatFilters>(k: K, v: ThreatFilters[K]) =>
    onChange({ ...value, [k]: v });
  return (
    <Card className="border-border/50 mb-3">
      <CardContent className="space-y-2 p-3">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={value.severity || "all"}
            onValueChange={(v) => set("severity", !v || v === "all" ? "" : v)}
          >
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue placeholder="Severity" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Any severity</SelectItem>
              {SEVERITIES.map((s) => (
                <SelectItem key={s} value={s}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={value.threatType || "all"}
            onValueChange={(v) =>
              set("threatType", !v || v === "all" ? "" : v)
            }
          >
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue placeholder="Threat type" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Any type</SelectItem>
              {THREAT_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={value.actionTaken || "all"}
            onValueChange={(v) =>
              set("actionTaken", !v || v === "all" ? "" : v)
            }
          >
            <SelectTrigger className="h-8 w-32 text-xs">
              <SelectValue placeholder="Action" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Any action</SelectItem>
              {ACTIONS.map((a) => (
                <SelectItem key={a} value={a}>
                  {a}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            ref={searchInputRef}
            placeholder="Search pattern or content..."
            value={value.search}
            onChange={(e) => set("search", e.target.value)}
            className="h-8 flex-1 min-w-[14rem] text-xs"
          />
          <Button
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => onChange(emptyThreatFilters)}
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
            placeholder="Subtype (e.g. persona.dan)"
            value={value.threatSubtype}
            onChange={(e) => set("threatSubtype", e.target.value)}
            className="h-8 w-48 text-xs"
          />
          <Input
            placeholder="Detector name"
            value={value.detectorName}
            onChange={(e) => set("detectorName", e.target.value)}
            className="h-8 w-44 text-xs"
            list="threat-detectors"
          />
          {detectors.length > 0 ? (
            <datalist id="threat-detectors">
              {detectors.map((d) => (
                <option key={d} value={d} />
              ))}
            </datalist>
          ) : null}
          <Input
            placeholder="End-user id"
            value={value.endUser}
            onChange={(e) => set("endUser", e.target.value)}
            className="h-8 w-40 text-xs"
          />
          <Input
            placeholder="IP address"
            value={value.ipAddress}
            onChange={(e) => set("ipAddress", e.target.value)}
            className="h-8 w-40 text-xs"
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
