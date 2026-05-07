import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi } from "./types";
import { MODEL_CATALOG, type AllowedModelEntry } from "./model-picker";
import { useWorkspaceExtension } from "./extension-context";

export function SettingsTab() {
  const ext = useWorkspaceExtension();
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: workspaceApi.getSettings,
  });

  // Seat limit / retention / billing mode / branding are cloud-only —
  // they're owned by the Stripe webhook (seat_limit), the Cloud admin
  // (retention/billing_mode), and the branded-chat feature (branding).
  // OSS doesn't expose them in this UI; the workspace_settings columns
  // still exist (set by migration 015) and PATCH ignores fields the
  // form doesn't send.
  const [allowedModels, setAllowedModels] = useState<AllowedModelEntry[]>([]);
  // AI persona — workspace-level overlay applied to every assistant's
  // system prompt at chat time. Empty string = "leave the assistant's
  // raw prompt alone". Three independent fields so admins can tune
  // identity, personality, and tone separately.
  const [personaName, setPersonaName] = useState("");
  const [personaPersonality, setPersonaPersonality] = useState("");
  const [personaTone, setPersonaTone] = useState("");

  useEffect(() => {
    if (settings.data) {
      setAllowedModels(settings.data.allowed_models ?? []);
      setPersonaName(settings.data.ai_persona_name ?? "");
      setPersonaPersonality(settings.data.ai_persona_personality ?? "");
      setPersonaTone(settings.data.ai_persona_tone ?? "");
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: () =>
      workspaceApi.patchSettings({
        allowed_models: allowedModels,
        // Empty string → null clears the field on the server side
        // (server reads nullable TEXT). Non-empty → set verbatim.
        ai_persona_name: personaName.trim() || null,
        ai_persona_personality: personaPersonality.trim() || null,
        ai_persona_tone: personaTone.trim() || null,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "settings"] }),
  });

  if (settings.isLoading) return <SkeletonRows count={4} />;

  return (
    <div className="space-y-6">
      <h3 className="text-sm font-semibold">AI persona</h3>
      <p className="text-xs text-muted-foreground -mt-4">
        Give your workspace's AI a name and personality. Applies to every assistant — employees see
        the same identity ("Have you asked Bob?") regardless of which assistant they pick. Leave
        blank to use each assistant's raw system prompt.
      </p>
      <Card>
        <CardContent className="space-y-3 p-4">
          <Field label="Name">
            <input
              type="text"
              value={personaName}
              onChange={(e) => setPersonaName(e.target.value)}
              placeholder="e.g. Bob"
              maxLength={120}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <Field label="Personality">
            <input
              type="text"
              value={personaPersonality}
              onChange={(e) => setPersonaPersonality(e.target.value)}
              placeholder="e.g. friendly, helpful, concise"
              maxLength={500}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <Field label="Tone of voice">
            <input
              type="text"
              value={personaTone}
              onChange={(e) => setPersonaTone(e.target.value)}
              placeholder="e.g. casual, professional, witty"
              maxLength={200}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <p className="text-xs text-muted-foreground">
            Saved together with the other settings via the button at the bottom of this page.
          </p>
        </CardContent>
      </Card>

      <h3 className="text-sm font-semibold">LLM models employees can pick</h3>

      <Card>
        <CardContent className="space-y-3 p-4">
          <ModelWhitelistEditor value={allowedModels} onChange={setAllowedModels} />
          {save.error && (
            <p className="text-sm text-destructive">{(save.error as Error).message}</p>
          )}
          <div className="flex justify-end">
            <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
              {save.isPending ? "Saving…" : "Save settings"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {ext.settingsExtra}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

// =============================================================================
// LLM model whitelist
// =============================================================================

// ModelWhitelistEditor lets the admin curate which (provider, model)
// pairs employees can pick from in the workspace chat. Empty list =
// all curated defaults available; non-empty = strict subset.
//
// Grouped by provider for ergonomics — admins typically think
// "OpenAI yes, Anthropic no" first, then per-model fine-tuning.
function ModelWhitelistEditor({
  value,
  onChange,
}: {
  value: AllowedModelEntry[];
  onChange: (next: AllowedModelEntry[]) => void;
}) {
  // Index value into a Set for O(1) membership checks while rendering.
  const allowedKey = (e: AllowedModelEntry) => `${e.provider}/${e.model}`;
  const allowSet = new Set(value.map(allowedKey));

  // Group catalog by provider for stacked sections.
  const byProvider = MODEL_CATALOG.reduce<Record<string, typeof MODEL_CATALOG>>(
    (acc, m) => {
      const list = acc[m.provider] ?? [];
      list.push(m);
      acc[m.provider] = list;
      return acc;
    },
    {},
  );

  const toggleModel = (provider: string, model: string) => {
    const key = `${provider}/${model}`;
    if (allowSet.has(key)) {
      onChange(value.filter((e) => allowedKey(e) !== key));
    } else {
      onChange([...value, { provider, model }]);
    }
  };

  const toggleProvider = (provider: string, models: typeof MODEL_CATALOG) => {
    const allOn = models.every((m) => allowSet.has(`${provider}/${m.model}`));
    if (allOn) {
      onChange(value.filter((e) => e.provider !== provider));
    } else {
      const others = value.filter((e) => e.provider !== provider);
      onChange([...others, ...models.map((m) => ({ provider, model: m.model }))]);
    }
  };

  const empty = value.length === 0;

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs font-medium text-muted-foreground">
          LLM models employees can pick
        </span>
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
          {empty ? "All curated defaults available" : `${value.length} model${value.length === 1 ? "" : "s"} allowed`}
        </span>
      </div>
      <p className="text-xs text-muted-foreground">
        Leave all unchecked to expose every curated default. Tick specific
        models to enforce a strict whitelist — both in the chat picker UI
        and as defense-in-depth on the chat send path.
      </p>
      <div className="space-y-3 rounded-md border border-border p-3">
        {Object.entries(byProvider).map(([provider, models]) => {
          const allOn = models.every((m) =>
            allowSet.has(`${provider}/${m.model}`),
          );
          const someOn = models.some((m) =>
            allowSet.has(`${provider}/${m.model}`),
          );
          return (
            <div key={provider} className="space-y-1.5">
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={allOn}
                  ref={(el) => {
                    if (el) el.indeterminate = !allOn && someOn;
                  }}
                  onChange={() => toggleProvider(provider, models)}
                />
                <span className="text-sm font-medium capitalize">{provider}</span>
                <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                  {someOn ? `${models.filter((m) => allowSet.has(`${provider}/${m.model}`)).length} / ${models.length}` : "off"}
                </span>
              </label>
              <div className="ml-6 grid grid-cols-1 gap-1 sm:grid-cols-2">
                {models.map((m) => {
                  const on = allowSet.has(`${provider}/${m.model}`);
                  return (
                    <label
                      key={`${provider}/${m.model}`}
                      className="flex items-center gap-2 text-xs"
                    >
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => toggleModel(provider, m.model)}
                      />
                      <span className={on ? "text-foreground" : "text-muted-foreground"}>
                        {m.label}
                      </span>
                      <span className="ml-auto font-mono text-[10px] text-muted-foreground">
                        {m.model}
                      </span>
                    </label>
                  );
                })}
              </div>
            </div>
          );
        })}
        {!empty && (
          <button
            type="button"
            onClick={() => onChange([])}
            className="text-xs text-cyan-500 hover:underline"
          >
            Clear whitelist (revert to all-defaults)
          </button>
        )}
      </div>
    </div>
  );
}

// Branded chat (slug + custom domains) was moved to bastio-cloud's
// cloud-dashboard/src/components/workspace/domains-tab.tsx — both
// features depend on hosted infrastructure (workspace.bastio.com
// routing + DNS verification) that doesn't exist in OSS deployments.
