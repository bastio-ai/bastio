import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Check, ImageOff, LockKeyhole, Save, ShieldAlert, Sparkles } from "lucide-react";

import { SectionHeader } from "@/components/card";
import { FieldLabel, SecurityNotice } from "@/components/admin/admin-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SkeletonRows } from "@/components/skeleton";
import { cn } from "@/lib/utils";
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
  const [allowedModels, setAllowedModels] = useState<AllowedModelEntry[]>([]);
  const [personaName, setPersonaName] = useState("");
  const [personaPersonality, setPersonaPersonality] = useState("");
  const [personaTone, setPersonaTone] = useState("");
  const [disableImageAttachments, setDisableImageAttachments] = useState(false);

  useEffect(() => {
    if (!settings.data) return;
    setAllowedModels(settings.data.allowed_models ?? []);
    setPersonaName(settings.data.ai_persona_name ?? "");
    setPersonaPersonality(settings.data.ai_persona_personality ?? "");
    setPersonaTone(settings.data.ai_persona_tone ?? "");
    setDisableImageAttachments(settings.data.disable_image_attachments ?? false);
  }, [settings.data]);

  const currentState = useMemo(
    () => JSON.stringify({
      allowedModels,
      personaName: personaName.trim(),
      personaPersonality: personaPersonality.trim(),
      personaTone: personaTone.trim(),
      disableImageAttachments,
    }),
    [allowedModels, disableImageAttachments, personaName, personaPersonality, personaTone],
  );
  const savedState = useMemo(
    () => settings.data
      ? JSON.stringify({
          allowedModels: settings.data.allowed_models ?? [],
          personaName: (settings.data.ai_persona_name ?? "").trim(),
          personaPersonality: (settings.data.ai_persona_personality ?? "").trim(),
          personaTone: (settings.data.ai_persona_tone ?? "").trim(),
          disableImageAttachments: settings.data.disable_image_attachments ?? false,
        })
      : currentState,
    [currentState, settings.data],
  );
  const dirty = currentState !== savedState;

  const save = useMutation({
    mutationFn: () =>
      workspaceApi.patchSettings({
        allowed_models: allowedModels,
        ai_persona_name: personaName.trim() || null,
        ai_persona_personality: personaPersonality.trim() || null,
        ai_persona_tone: personaTone.trim() || null,
        disable_image_attachments: disableImageAttachments,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "settings"] }),
  });

  if (settings.isLoading) return <SkeletonRows count={5} />;

  const strictModelPolicy = allowedModels.length > 0;
  const personaConfigured = Boolean(personaName || personaPersonality || personaTone);

  return (
    <div>
      <SectionHeader
        title="Workspace-wide AI policy"
        description="These controls apply across assistants and the employee portal."
        action={
          <div className="flex items-center gap-2">
            {!dirty && save.isSuccess ? (
              <span className="inline-flex items-center gap-1 text-[10px] text-success"><Check className="h-3 w-3" /> Saved</span>
            ) : null}
            <Button size="sm" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
              <Save data-icon="inline-start" /> {save.isPending ? "Saving…" : "Save changes"}
            </Button>
          </div>
        }
      />

      <div className="mb-5 grid overflow-hidden rounded-xl border border-border/70 bg-card sm:grid-cols-2 sm:divide-x sm:divide-border/60 xl:grid-cols-4">
        <PolicyMetric label="AI identity" value={personaConfigured ? "Configured" : "Assistant defaults"} ready={personaConfigured} />
        <PolicyMetric label="Model policy" value={strictModelPolicy ? "Strict whitelist" : "Curated catalog"} ready={strictModelPolicy} />
        <PolicyMetric label="Models available" value={strictModelPolicy ? allowedModels.length : MODEL_CATALOG.length} ready />
        <PolicyMetric label="Image uploads" value={disableImageAttachments ? "Blocked" : "Allowed"} ready={disableImageAttachments} />
      </div>

      <section className="mt-5 overflow-hidden rounded-xl border border-border/70 bg-card">
        <div className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
              <ImageOff className="h-3.5 w-3.5" />
            </div>
            <div>
              <h2 className="text-[13px] font-semibold text-foreground">Image attachment policy</h2>
              <p className="mt-0.5 max-w-2xl text-[10px] leading-relaxed text-muted-foreground">
                Block images in employee chat when every attachment must pass text extraction and security inspection.
              </p>
            </div>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={disableImageAttachments}
            onClick={() => setDisableImageAttachments((current) => !current)}
            className={cn(
              "inline-flex h-8 shrink-0 items-center gap-2 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
              disableImageAttachments
                ? "border-success-border bg-success-bg text-success"
                : "border-border/70 bg-background text-muted-foreground hover:bg-muted/40",
            )}
          >
            <span className={cn("h-2 w-2 rounded-full", disableImageAttachments ? "bg-success" : "bg-muted-foreground/50")} />
            {disableImageAttachments ? "Images blocked" : "Images allowed"}
          </button>
        </div>
        {!disableImageAttachments ? (
          <div className="border-t border-border/60 px-4 py-3">
            <SecurityNotice title="Images bypass text-only inspection" tone="warning">
              Image uploads are rendered as visual context and are not text-extracted. Disable them for regulated or high-assurance workspaces.
            </SecurityNotice>
          </div>
        ) : null}
      </section>

      <div className="grid gap-5 xl:grid-cols-[minmax(320px,0.72fr)_minmax(0,1.28fr)]">
        <section className="rounded-xl border border-border/70 bg-card">
          <div className="border-b border-border/60 px-4 py-3.5">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground"><Sparkles className="h-3.5 w-3.5" /></div>
              <div>
                <h2 className="text-[13px] font-semibold text-foreground">Shared AI identity</h2>
                <p className="mt-0.5 text-[10px] text-muted-foreground">A consistent persona layered onto every assistant.</p>
              </div>
            </div>
          </div>
          <div className="space-y-4 p-4">
            <div>
              <FieldLabel optional>Name</FieldLabel>
              <Input value={personaName} onChange={(event) => setPersonaName(event.target.value)} placeholder="e.g. Bob" maxLength={120} />
              <p className="mt-1 text-[10px] text-muted-foreground">Shown as the shared AI identity in the portal.</p>
            </div>
            <div>
              <FieldLabel optional>Personality</FieldLabel>
              <Input value={personaPersonality} onChange={(event) => setPersonaPersonality(event.target.value)} placeholder="e.g. helpful, precise, concise" maxLength={500} />
            </div>
            <div>
              <FieldLabel optional>Tone of voice</FieldLabel>
              <Input value={personaTone} onChange={(event) => setPersonaTone(event.target.value)} placeholder="e.g. professional and direct" maxLength={200} />
            </div>
            <SecurityNotice title="Assistant instructions still apply" tone="info">
              Blank fields add no workspace-level persona. Each assistant’s own system prompt remains the source of its role and boundaries.
            </SecurityNotice>
          </div>
        </section>

        <section className="rounded-xl border border-border/70 bg-card">
          <div className="flex items-start justify-between gap-4 border-b border-border/60 px-4 py-3.5">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground"><LockKeyhole className="h-3.5 w-3.5" /></div>
              <div>
                <h2 className="text-[13px] font-semibold text-foreground">Model access policy</h2>
                <p className="mt-0.5 text-[10px] text-muted-foreground">Control the models employees can select at chat time.</p>
              </div>
            </div>
            <Badge variant={strictModelPolicy ? "success" : "warning"}>
              {strictModelPolicy ? "Restricted" : "Catalog default"}
            </Badge>
          </div>
          <div className="space-y-4 p-4">
            {!strictModelPolicy ? (
              <SecurityNotice title="All curated models are currently available" tone="warning">
                An empty whitelist intentionally exposes the full curated catalog. Select approved models below to enforce a strict allowlist in both the portal UI and chat send path.
              </SecurityNotice>
            ) : (
              <SecurityNotice title="Strict model whitelist enforced" tone="success">
                Only the selected provider and model pairs are available to employees. Server-side validation provides defense in depth beyond the picker UI.
              </SecurityNotice>
            )}
            <ModelWhitelistEditor value={allowedModels} onChange={setAllowedModels} />
          </div>
        </section>
      </div>

      {save.error ? (
        <SecurityNotice className="mt-5" title="Settings could not be saved" tone="warning">{(save.error as Error).message}</SecurityNotice>
      ) : null}
      {dirty ? (
        <div className="sticky bottom-3 z-10 mt-5 flex items-center justify-between gap-4 rounded-xl border border-border/80 bg-popover/95 px-4 py-3 shadow-lg backdrop-blur">
          <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
            <ShieldAlert className="h-3.5 w-3.5" /> Unsaved policy changes
          </div>
          <Button size="sm" disabled={save.isPending} onClick={() => save.mutate()}>
            <Save data-icon="inline-start" /> {save.isPending ? "Saving…" : "Save policy"}
          </Button>
        </div>
      ) : null}

      {ext.settingsExtra ? <div className="mt-5">{ext.settingsExtra}</div> : null}
    </div>
  );
}

function PolicyMetric({ label, value, ready }: { label: string; value: string | number; ready?: boolean }) {
  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      <div className={cn("flex h-8 w-8 items-center justify-center rounded-lg border", ready ? "border-success-border bg-success-bg text-success" : "border-border/60 bg-muted/30 text-muted-foreground")}>
        {ready ? <Check className="h-3.5 w-3.5" /> : <Bot className="h-3.5 w-3.5" />}
      </div>
      <div className="min-w-0">
        <p className="text-[10px] uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
        <p className="mt-0.5 truncate text-[12px] font-medium text-foreground">{value}</p>
      </div>
    </div>
  );
}

function ModelWhitelistEditor({ value, onChange }: { value: AllowedModelEntry[]; onChange: (next: AllowedModelEntry[]) => void }) {
  const allowedKey = (entry: AllowedModelEntry) => `${entry.provider}/${entry.model}`;
  const allowSet = new Set(value.map(allowedKey));
  const byProvider = MODEL_CATALOG.reduce<Record<string, typeof MODEL_CATALOG>>((result, model) => {
    (result[model.provider] ??= []).push(model);
    return result;
  }, {});

  const toggleModel = (provider: string, model: string) => {
    const key = `${provider}/${model}`;
    onChange(allowSet.has(key) ? value.filter((entry) => allowedKey(entry) !== key) : [...value, { provider, model }]);
  };
  const toggleProvider = (provider: string, models: typeof MODEL_CATALOG) => {
    const allOn = models.every((model) => allowSet.has(`${provider}/${model.model}`));
    const otherProviders = value.filter((entry) => entry.provider !== provider);
    onChange(allOn ? otherProviders : [...otherProviders, ...models.map((model) => ({ provider, model: model.model }))]);
  };

  return (
    <div className="space-y-3">
      {Object.entries(byProvider).map(([provider, models]) => {
        const selected = models.filter((model) => allowSet.has(`${provider}/${model.model}`));
        const allOn = selected.length === models.length;
        const someOn = selected.length > 0;
        return (
          <div key={provider} className="overflow-hidden rounded-lg border border-border/70">
            <label className="flex cursor-pointer items-center gap-3 border-b border-border/50 bg-muted/20 px-3 py-2.5">
              <input
                type="checkbox"
                checked={allOn}
                ref={(element) => { if (element) element.indeterminate = !allOn && someOn; }}
                onChange={() => toggleProvider(provider, models)}
              />
              <span className="text-[12px] font-medium capitalize text-foreground">{provider}</span>
              <span className="ml-auto font-mono text-[10px] text-muted-foreground">{selected.length}/{models.length} selected</span>
            </label>
            <div className="grid sm:grid-cols-2">
              {models.map((model, index) => {
                const selectedModel = allowSet.has(`${provider}/${model.model}`);
                return (
                  <label
                    key={`${provider}/${model.model}`}
                    className={cn(
                      "flex cursor-pointer items-center gap-2.5 px-3 py-2.5 transition-colors hover:bg-muted/30",
                      index > 0 && "border-t border-border/40 sm:border-t-0",
                      index % 2 === 1 && "sm:border-l sm:border-border/40",
                      index >= 2 && "sm:border-t sm:border-border/40",
                    )}
                  >
                    <input type="checkbox" checked={selectedModel} onChange={() => toggleModel(provider, model.model)} />
                    <div className="min-w-0">
                      <p className={cn("truncate text-[11px] font-medium", selectedModel ? "text-foreground" : "text-muted-foreground")}>{model.label}</p>
                      <p className="truncate font-mono text-[9px] text-muted-foreground">{model.model}</p>
                    </div>
                  </label>
                );
              })}
            </div>
          </div>
        );
      })}
      {value.length > 0 ? (
        <Button size="xs" variant="ghost" onClick={() => onChange([])}>Use the full curated catalog</Button>
      ) : null}
    </div>
  );
}
