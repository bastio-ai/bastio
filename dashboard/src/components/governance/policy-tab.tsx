import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save, Trash2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { api, type GovernanceCustomerPolicy } from "@/api/client";

type CustomerPolicy = GovernanceCustomerPolicy;

function NativeLabel({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <label className={`text-xs font-medium text-muted-foreground ${className ?? ""}`}>
      {children}
    </label>
  );
}

function Toggle({
  checked,
  onChange,
  ariaLabel,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  ariaLabel: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition ${
        checked ? "bg-cyan-500" : "bg-muted"
      }`}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white transition ${
          checked ? "translate-x-6" : "translate-x-1"
        }`}
      />
    </button>
  );
}

type ActionVal = "log" | "warn" | "block_redirect";
const ACTIONS: ActionVal[] = ["log", "warn", "block_redirect"];

export function PolicyTab() {
  const qc = useQueryClient();
  const policyQ = useQuery({
    queryKey: ["governance", "policy"] as const,
    queryFn: () => api.governance.policy.get(),
  });

  const [draft, setDraft] = useState<CustomerPolicy | null>(null);
  const [keywordInput, setKeywordInput] = useState("");

  useEffect(() => {
    if (policyQ.data) setDraft(policyQ.data);
  }, [policyQ.data]);

  const save = useMutation({
    mutationFn: async (body: CustomerPolicy) => api.governance.policy.update(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["governance", "policy"] }),
  });

  if (!draft) {
    return (
      <Card>
        <CardContent className="p-12 text-center text-sm text-muted-foreground">
          Loading policy…
        </CardContent>
      </Card>
    );
  }

  const update = (patch: Partial<CustomerPolicy>) =>
    setDraft((d) => (d ? { ...d, ...patch } : d));

  const addKeyword = () => {
    const k = keywordInput.trim();
    if (!k) return;
    if ((draft.CustomKeywords ?? []).includes(k)) return;
    update({ CustomKeywords: [...(draft.CustomKeywords ?? []), k] });
    setKeywordInput("");
  };

  const removeKeyword = (kw: string) =>
    update({ CustomKeywords: (draft.CustomKeywords ?? []).filter((k) => k !== kw) });

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-6 p-6">
          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Severity actions
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              What happens when the extension detects content at each severity tier.
            </p>
            <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-3">
              {(["Low", "Medium", "High"] as const).map((tier) => {
                const fieldKey = `Severity${tier}` as
                  | "SeverityLow"
                  | "SeverityMedium"
                  | "SeverityHigh";
                return (
                  <div key={tier} className="space-y-2">
                    <NativeLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                      {tier}
                    </NativeLabel>
                    <select
                      value={draft[fieldKey]}
                      onChange={(e) =>
                        update({ [fieldKey]: e.target.value as ActionVal } as Partial<CustomerPolicy>)
                      }
                      className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    >
                      {ACTIONS.map((a) => (
                        <option key={a} value={a}>
                          {a}
                        </option>
                      ))}
                    </select>
                  </div>
                );
              })}
            </div>
          </div>

          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Custom keywords
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Terms that promote a low-severity event (project codenames, internal labels).
            </p>
            <div className="mt-4 flex gap-2">
              <Input
                value={keywordInput}
                onChange={(e) => setKeywordInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    addKeyword();
                  }
                }}
                placeholder="Project Atlas"
                className="max-w-sm"
              />
              <Button size="sm" variant="outline" onClick={addKeyword}>
                Add
              </Button>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              {(draft.CustomKeywords ?? []).map((kw) => (
                <Badge
                  key={kw}
                  variant="outline"
                  className="cursor-pointer gap-1 font-mono text-xs"
                  onClick={() => removeKeyword(kw)}
                >
                  {kw} <Trash2 className="h-3 w-3" />
                </Badge>
              ))}
              {(draft.CustomKeywords ?? []).length === 0 && (
                <span className="text-xs italic text-muted-foreground">
                  No custom keywords yet.
                </span>
              )}
            </div>
          </div>

          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Redirect target
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Where the extension sends employees when their prompt is blocked. Point at LibreChat,
              Open WebUI, your internal AI tool, or future Bastio Workspace.
            </p>
            <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <NativeLabel>URL</NativeLabel>
                <Input
                  value={draft.RedirectTarget?.url ?? ""}
                  onChange={(e) =>
                    update({
                      RedirectTarget: {
                        url: e.target.value,
                        label: draft.RedirectTarget?.label ?? "",
                        open_in_new_tab: draft.RedirectTarget?.open_in_new_tab ?? true,
                      },
                    })
                  }
                  placeholder="https://librechat.acme.internal"
                />
              </div>
              <div className="space-y-2">
                <NativeLabel>Label</NativeLabel>
                <Input
                  value={draft.RedirectTarget?.label ?? ""}
                  onChange={(e) =>
                    update({
                      RedirectTarget: {
                        url: draft.RedirectTarget?.url ?? "",
                        label: e.target.value,
                        open_in_new_tab: draft.RedirectTarget?.open_in_new_tab ?? true,
                      },
                    })
                  }
                  placeholder="Acme LibreChat"
                />
              </div>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div className="flex items-center justify-between rounded-md border border-border p-4">
              <div>
                <span className="text-sm font-medium">Allow override</span>
                <p className="mt-1 text-xs text-muted-foreground">
                  Lets employees bypass a high-severity block with a justification.
                </p>
              </div>
              <Toggle
                checked={draft.OverrideEnabled ?? false}
                onChange={(v) => update({ OverrideEnabled: v })}
                ariaLabel="Allow override"
              />
            </div>
            <div className="flex items-center justify-between rounded-md border border-border p-4">
              <div>
                <span className="text-sm font-medium">Pseudonymize PII</span>
                <p className="mt-1 text-xs text-muted-foreground">
                  Hide employee identifiers in dashboard views (works-council friendly).
                </p>
              </div>
              <Toggle
                checked={draft.PseudonymizePII ?? false}
                onChange={(v) => update({ PseudonymizePII: v })}
                ariaLabel="Pseudonymize PII"
              />
            </div>
          </div>

          <div className="flex items-center justify-end gap-3 border-t border-border pt-4">
            {save.isError && (
              <span className="text-xs text-destructive">
                {(save.error as Error).message}
              </span>
            )}
            {save.isSuccess && (
              <span className="text-xs text-green-500">Saved.</span>
            )}
            <Button
              onClick={() => save.mutate(draft)}
              disabled={save.isPending}
              className="gap-2"
            >
              <Save className="h-4 w-4" />
              {save.isPending ? "Saving…" : "Save policy"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
