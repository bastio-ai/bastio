import { useEffect, useId, useState, type ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { emptyFilters, parseTagFilter, TRACE_TYPES, type ObserveFilters } from "./trace-filters";

export type TraceFacets = { names: string[]; providers: string[]; models: string[]; environments: string[] };
const outcomes = [{ value: "ok", label: "Completed / allowed" }, { value: "error,blocked,rate_limited", label: "Failed or blocked" }, { value: "error", label: "Error" }, { value: "blocked", label: "Blocked" }, { value: "rate_limited", label: "Rate limited" }];
const securityOptions = [{ value: "threat", label: "Has security signals" }, { value: "clean", label: "No security signals" }];

export function TraceFilterPanel({ value, onApply, facets, loading, error }: {
  value: ObserveFilters;
  onApply: (next: ObserveFilters) => void;
  facets?: TraceFacets;
  loading?: boolean;
  error?: boolean;
}) {
  const [draft, setDraft] = useState(value);
  const [validation, setValidation] = useState("");
  const id = useId();
  useEffect(() => setDraft(value), [value]);
  const set = (field: keyof ObserveFilters, next: string) => setDraft((current) => ({ ...current, [field]: next }));
  const suggestions = (values: string[] = []) => Array.from(new Set(values.filter(Boolean))).sort();
  const text = (label: string, field: keyof ObserveFilters, placeholder: string, values?: string[]) => <Field label={label}>
    <Input value={draft[field]} onChange={(event) => set(field, event.target.value)} placeholder={placeholder} list={values ? `${id}-${field}` : undefined} className="text-xs" />
    {values ? <datalist id={`${id}-${field}`}>{suggestions(values).map((option) => <option key={option} value={option} />)}</datalist> : null}
  </Field>;
  return <form aria-label="Trace filters" onSubmit={(event) => {
    event.preventDefault();
    if (draft.from && draft.to && Date.parse(draft.from) > Date.parse(draft.to)) { setValidation("From must be earlier than To."); return; }
    if (draft.tags.trim() && parseTagFilter(draft.tags).length !== draft.tags.split(/[,\s]+/).filter(Boolean).length) { setValidation("Use key:value tags separated by commas or spaces."); return; }
    setValidation("");
    onApply(draft);
  }} className="max-h-[45vh] shrink-0 space-y-3 overflow-y-auto border-b border-border-subtle bg-surface-1 px-3 py-3">
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {text("Operation name contains", "traceName", "e.g. card_refresh", facets?.names ?? [])}
      <Field label="Operation type"><FilterSelect label="Operation type" value={draft.traceType} onChange={(next) => set("traceType", next)} options={TRACE_TYPES} all="All operation types" /></Field>
      {text("Provider", "provider", "Select or enter a provider", [...(facets?.providers ?? []), "browser-extension"])}
      {text("Model", "model", "Select or enter a model", facets?.models ?? [])}
    </div>
    <details>
      <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">More filters: outcome, security, user, release, tags and dates</summary>
      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Field label="Outcome"><FilterSelect label="Outcome" value={draft.status} onChange={(next) => set("status", next)} all="All outcomes" options={outcomes} /></Field>
        <Field label="Security"><FilterSelect label="Security" value={draft.security} onChange={(next) => set("security", next)} all="Any security" options={securityOptions} /></Field>
        {text("End user ID", "endUser", "Exact user ID")}
        {text("Release", "release", "e.g. v0.1.129")}
        {text("Tags", "tags", "key:value,other:value")}
        {text("Environment override", "environment", "Empty uses the header selection", facets?.environments ?? [])}
        <Field label="From (optional)"><Input type="datetime-local" value={localDateTime(draft.from)} onChange={(event) => set("from", event.target.value ? new Date(event.target.value).toISOString() : "")} className="text-xs" /></Field>
        <Field label="To (optional)"><Input type="datetime-local" value={localDateTime(draft.to)} onChange={(event) => set("to", event.target.value ? new Date(event.target.value).toISOString() : "")} className="text-xs" /></Field>
      </div>
    </details>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-[10px] text-muted-foreground">{loading ? "Loading suggestions…" : error ? "Suggestions unavailable; you can still enter filter values." : "Suggestions come from observed traffic. You can also enter values directly."}</p>
      <div className="flex gap-2"><Button type="button" variant="ghost" size="sm" onClick={() => { setDraft(emptyFilters); onApply(emptyFilters); }}>Clear filters</Button><Button type="submit" size="sm">Apply filters</Button></div>
    </div>
    {validation ? <p role="alert" className="text-xs text-destructive">{validation}</p> : null}
  </form>;
}

function localDateTime(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="block min-w-0 space-y-1"><span className="text-[10px] font-medium text-muted-foreground">{label}</span>{children}</label>;
}

function FilterSelect({ label, value, onChange, options, all }: { label: string; value: string; onChange: (value: string) => void; options: readonly { value: string; label: string }[]; all: string }) {
  return <Select value={value || "__all__"} onValueChange={(next) => onChange(!next || next === "__all__" ? "" : next)}><SelectTrigger aria-label={label} className="w-full cursor-pointer text-xs"><SelectValue>{value ? options.find((option) => option.value === value)?.label || value : all}</SelectValue></SelectTrigger><SelectContent><SelectItem value="__all__">{all}</SelectItem>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select>;
}

const labels: Partial<Record<keyof ObserveFilters, string>> = { traceName: "Name contains", traceType: "Type", provider: "Provider", model: "Model", status: "Outcome", security: "Security", endUser: "User", release: "Release", tags: "Tags", search: "Search", environment: "Environment", from: "From", to: "To" };
export function TraceFilterChips({ filters, onChange }: { filters: ObserveFilters; onChange: (next: ObserveFilters) => void }) {
  const active = Object.entries(labels).filter(([field]) => filters[field as keyof ObserveFilters]);
  if (!active.length) return null;
  return <div aria-label="Active trace filters" className="flex flex-wrap gap-1.5 border-b border-border-subtle px-3 py-2">
    {active.map(([field, label]) => <button key={field} type="button" onClick={() => onChange({ ...filters, [field]: "" })} aria-label={`Remove ${label} filter`} className="flex max-w-full cursor-pointer items-center gap-1 rounded-md border border-border-default bg-surface-1 px-2 py-1 text-[10px] hover:bg-surface-2"><span className="truncate">{label}: {field === "traceType" ? TRACE_TYPES.find((option) => option.value === filters.traceType)?.label : field === "status" ? outcomes.find((option) => option.value === filters.status)?.label || filters.status : field === "security" ? securityOptions.find((option) => option.value === filters.security)?.label : filters[field as keyof ObserveFilters]}</span><X className="size-3 shrink-0" /></button>)}
    <button type="button" onClick={() => onChange(emptyFilters)} className="cursor-pointer px-2 text-[10px] text-muted-foreground hover:text-foreground">Clear all</button>
  </div>;
}
