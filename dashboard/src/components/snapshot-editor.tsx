import { useMemo } from "react";
import { Plus, Trash2 } from "lucide-react";

import type { OverlaySnapshot, PatternRule } from "@/api/overlay";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// SnapshotEditor is the shared editor used by the "new overlay" and
// "new version" flows. It offers two views over the same underlying
// JSON string: a raw JSON textarea (power-user default) and a
// structured Form (dropdowns + pattern row editor for common edits).
//
// Design notes:
//   - Source of truth is the JSON string held by the parent. The
//     Form view parses the string on render and serialises back on
//     every edit so there's no dual-state drift to worry about.
//   - When the JSON is invalid, the Form tab shows a disabled banner
//     — you edit the JSON to fix it, then switch back.
//   - Form deliberately covers only detector overrides + additional
//     patterns. Access rules and plugin detectors are rarer to edit
//     and fit the JSON view fine (access rules also have the "not
//     yet enforced" caveat surfaced elsewhere).

export type SnapshotEditorView = "json" | "form";

type Props = {
  value: string;
  onChange: (next: string, parseError: string | null) => void;
  view: SnapshotEditorView;
  onViewChange: (next: SnapshotEditorView) => void;
  parseError: string | null;
  rows?: number;
};

type DetectorKey =
  | "injection"
  | "jailbreak"
  | "secrets"
  | "indirect_injection"
  | "output_exfil";

const detectorLabels: Record<DetectorKey, string> = {
  injection: "Injection",
  jailbreak: "Jailbreak",
  secrets: "Secrets",
  indirect_injection: "Indirect injection",
  output_exfil: "Output exfiltration",
};

const thresholdDetectors: DetectorKey[] = ["injection", "jailbreak"];

const strategyOptions = ["block", "warn", "log_only"];
const secretsStrategyOptions = ["block", "mask", "warn", "log_only"];
const piiActionOptions = ["block", "mask", "tokenize", "warn", "log_only"];
const patternTypes = ["regex", "keyword", "semantic"];
const patternActions = ["block", "warn", "log"];
const severityLevels = ["low", "medium", "high", "critical"];

export function SnapshotEditor({
  value,
  onChange,
  view,
  onViewChange,
  parseError,
  rows = 14,
}: Props) {
  const parsed = useMemo<OverlaySnapshot | null>(() => {
    try {
      return JSON.parse(value) as OverlaySnapshot;
    } catch {
      return null;
    }
  }, [value]);

  const updateSnapshot = (mutate: (s: OverlaySnapshot) => OverlaySnapshot) => {
    if (!parsed) return;
    const next = mutate({ ...parsed });
    const str = JSON.stringify(next, null, 2);
    onChange(str, null);
  };

  return (
    <div className="space-y-2">
      <div className="flex gap-1 border-b border-border/50 text-xs">
        <button
          type="button"
          onClick={() => onViewChange("json")}
          className={`px-3 py-1.5 border-b-2 -mb-px transition-colors ${
            view === "json"
              ? "border-foreground text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          JSON
        </button>
        <button
          type="button"
          onClick={() => onViewChange("form")}
          className={`px-3 py-1.5 border-b-2 -mb-px transition-colors ${
            view === "form"
              ? "border-foreground text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          Form
        </button>
      </div>

      {view === "json" ? (
        <textarea
          value={value}
          onChange={(e) => {
            const next = e.target.value;
            try {
              JSON.parse(next);
              onChange(next, null);
            } catch (err) {
              onChange(next, (err as Error).message);
            }
          }}
          rows={rows}
          spellCheck={false}
          className="w-full rounded border border-border/50 bg-muted/20 p-2 font-mono text-[11px] leading-relaxed focus:outline-none"
        />
      ) : !parsed ? (
        <div className="rounded border border-destructive/40 bg-destructive/5 p-3 text-xs">
          The snapshot JSON is invalid — switch to the JSON tab to fix it before editing via the form.
          {parseError ? (
            <div className="mt-1 font-mono text-[11px] text-destructive">{parseError}</div>
          ) : null}
        </div>
      ) : (
        <FormView snapshot={parsed} onUpdate={updateSnapshot} />
      )}
      <p className="text-[11px] text-muted-foreground">
        Note: <span className="font-mono">additional_access_rules</span> is accepted and stored, but runtime enforcement is not yet wired — those rules won't block traffic until a future release.
      </p>
    </div>
  );
}

function FormView({
  snapshot,
  onUpdate,
}: {
  snapshot: OverlaySnapshot;
  onUpdate: (mut: (s: OverlaySnapshot) => OverlaySnapshot) => void;
}) {
  return (
    <div className="space-y-3">
      <DetectorOverridesSection snapshot={snapshot} onUpdate={onUpdate} />
      <PatternsSection snapshot={snapshot} onUpdate={onUpdate} />
      <p className="text-[11px] text-muted-foreground">
        Access rules and plugin detectors can be edited via the JSON tab.
      </p>
    </div>
  );
}

function DetectorOverridesSection({
  snapshot,
  onUpdate,
}: {
  snapshot: OverlaySnapshot;
  onUpdate: (mut: (s: OverlaySnapshot) => OverlaySnapshot) => void;
}) {
  const overrides = snapshot.detector_overrides ?? {};

  const setDetector = (
    key: DetectorKey,
    patch: { threshold?: number | null; strategy?: string | null } | null,
  ) => {
    onUpdate((s) => {
      const next = { ...(s.detector_overrides ?? {}) };
      if (patch === null) {
        delete (next as Record<string, unknown>)[key];
      } else {
        const prev = (next as Record<string, { threshold?: number; strategy?: string } | undefined>)[key] ?? {};
        const merged: { threshold?: number; strategy?: string } = { ...prev };
        if (patch.threshold === null) delete merged.threshold;
        else if (patch.threshold !== undefined) merged.threshold = patch.threshold;
        if (patch.strategy === null) delete merged.strategy;
        else if (patch.strategy !== undefined) merged.strategy = patch.strategy;
        (next as Record<string, typeof merged>)[key] = merged;
      }
      s.detector_overrides = next;
      return s;
    });
  };

  const setPIIAction = (action: string | null) => {
    onUpdate((s) => {
      const next = { ...(s.detector_overrides ?? {}) };
      if (action === null) {
        delete (next as Record<string, unknown>).pii;
      } else {
        (next as Record<string, { action?: string }>).pii = { action };
      }
      s.detector_overrides = next;
      return s;
    });
  };

  return (
    <div className="space-y-2">
      <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
        Detector overrides
      </p>
      <div className="rounded border border-border/40 divide-y divide-border/30">
        {(Object.keys(detectorLabels) as DetectorKey[]).map((key) => {
          const cur = (overrides as Record<string, { threshold?: number; strategy?: string } | undefined>)[key];
          const enabled = !!cur;
          const opts = key === "secrets" ? secretsStrategyOptions : strategyOptions;
          return (
            <div key={key} className="p-2 space-y-2">
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => setDetector(key, e.target.checked ? {} : null)}
                  className="h-3 w-3"
                />
                <span className="font-medium">{detectorLabels[key]}</span>
                {enabled ? null : (
                  <span className="text-[10px] text-muted-foreground">(inheriting defaults)</span>
                )}
              </label>
              {enabled ? (
                <div className="grid grid-cols-2 gap-2 pl-5">
                  {thresholdDetectors.includes(key) ? (
                    <div className="space-y-1">
                      <p className="text-[10px] text-muted-foreground">Threshold (0–1)</p>
                      <Input
                        type="number"
                        min={0}
                        max={1}
                        step={0.05}
                        value={cur?.threshold ?? ""}
                        onChange={(e) => {
                          const v = e.target.value;
                          setDetector(key, { threshold: v === "" ? null : parseFloat(v) });
                        }}
                        className="h-7 text-xs"
                      />
                    </div>
                  ) : null}
                  <div className="space-y-1">
                    <p className="text-[10px] text-muted-foreground">Strategy</p>
                    <Select
                      value={cur?.strategy ?? ""}
                      onValueChange={(v) => setDetector(key, { strategy: v || null })}
                    >
                      <SelectTrigger className="h-7 text-xs">
                        <SelectValue placeholder="inherit" />
                      </SelectTrigger>
                      <SelectContent>
                        {opts.map((o) => (
                          <SelectItem key={o} value={o} className="text-xs">
                            {o}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              ) : null}
            </div>
          );
        })}
        {/* PII has a different shape — action only */}
        <div className="p-2 space-y-2">
          <label className="flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={!!overrides.pii}
              onChange={(e) => setPIIAction(e.target.checked ? "mask" : null)}
              className="h-3 w-3"
            />
            <span className="font-medium">PII</span>
            {overrides.pii ? null : (
              <span className="text-[10px] text-muted-foreground">(inheriting defaults)</span>
            )}
          </label>
          {overrides.pii ? (
            <div className="pl-5">
              <div className="space-y-1">
                <p className="text-[10px] text-muted-foreground">Action</p>
                <Select
                  value={overrides.pii.action ?? ""}
                  onValueChange={(v) => setPIIAction(v || null)}
                >
                  <SelectTrigger className="h-7 text-xs">
                    <SelectValue placeholder="inherit" />
                  </SelectTrigger>
                  <SelectContent>
                    {piiActionOptions.map((o) => (
                      <SelectItem key={o} value={o} className="text-xs">
                        {o}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function PatternsSection({
  snapshot,
  onUpdate,
}: {
  snapshot: OverlaySnapshot;
  onUpdate: (mut: (s: OverlaySnapshot) => OverlaySnapshot) => void;
}) {
  const patterns = snapshot.additional_patterns ?? [];

  const updateRow = (i: number, patch: Partial<PatternRule>) => {
    onUpdate((s) => {
      const list = [...(s.additional_patterns ?? [])];
      list[i] = { ...list[i], ...patch } as PatternRule;
      s.additional_patterns = list;
      return s;
    });
  };
  const removeRow = (i: number) => {
    onUpdate((s) => {
      const list = [...(s.additional_patterns ?? [])];
      list.splice(i, 1);
      s.additional_patterns = list.length ? list : undefined;
      return s;
    });
  };
  const addRow = () => {
    onUpdate((s) => {
      const list = [...(s.additional_patterns ?? [])];
      list.push({
        name: "",
        pattern_type: "regex",
        pattern: "",
        action: "warn",
        severity: "medium",
      });
      s.additional_patterns = list;
      return s;
    });
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          Additional patterns
        </p>
        <Button size="sm" variant="ghost" className="h-7 text-[11px]" onClick={addRow}>
          <Plus className="h-3 w-3" /> Add pattern
        </Button>
      </div>
      {patterns.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">No additional patterns.</p>
      ) : (
        <div className="rounded border border-border/40 divide-y divide-border/30">
          {patterns.map((p, i) => (
            <div key={i} className="p-2 grid grid-cols-[1fr_auto] gap-2">
              <div className="grid grid-cols-2 gap-2">
                <Input
                  placeholder="name"
                  value={p.name}
                  onChange={(e) => updateRow(i, { name: e.target.value })}
                  className="h-7 text-xs"
                />
                <Select
                  value={p.pattern_type}
                  onValueChange={(v) =>
                    updateRow(i, { pattern_type: v as PatternRule["pattern_type"] })
                  }
                >
                  <SelectTrigger className="h-7 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {patternTypes.map((t) => (
                      <SelectItem key={t} value={t} className="text-xs">
                        {t}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  placeholder="pattern (regex/keyword/semantic)"
                  value={p.pattern}
                  onChange={(e) => updateRow(i, { pattern: e.target.value })}
                  className="h-7 text-xs font-mono col-span-2"
                />
                <Select
                  value={p.action ?? ""}
                  onValueChange={(v) =>
                    updateRow(i, { action: v as PatternRule["action"] })
                  }
                >
                  <SelectTrigger className="h-7 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {patternActions.map((a) => (
                      <SelectItem key={a} value={a} className="text-xs">
                        {a}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={p.severity ?? ""}
                  onValueChange={(v) =>
                    updateRow(i, { severity: v as PatternRule["severity"] })
                  }
                >
                  <SelectTrigger className="h-7 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {severityLevels.map((s) => (
                      <SelectItem key={s} value={s} className="text-xs">
                        {s}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 self-start text-destructive"
                onClick={() => removeRow(i)}
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
