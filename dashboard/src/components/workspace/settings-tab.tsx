import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import {
  Check,
  ImageOff,
  LockKeyhole,
  Save,
  ShieldAlert,
  Sparkles,
} from "lucide-react";

import { SectionHeader } from "@/components/card";
import {
  FieldLabel,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SkeletonRows } from "@/components/skeleton";
import { cn } from "@/lib/utils";
import { workspaceApi, type Settings } from "./types";
import { MODEL_CATALOG, type AllowedModelEntry } from "./model-picker";
import { useWorkspaceExtension } from "./extension-context";

import type { SettingsSection } from "./navigation";

export function SettingsTab({
  section = "general",
  onSectionChange,
}: {
  section?: SettingsSection;
  onSectionChange?: (section: SettingsSection) => void;
}) {
  const ext = useWorkspaceExtension();
  const [cloudDirty, setCloudDirty] = useState(false);
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: workspaceApi.getSettings,
  });
  const sectionsNav = useRef<HTMLElement>(null);

  useEffect(() => {
    const nav = sectionsNav.current;
    const active = nav?.querySelector('[aria-current="page"]');
    if (!nav || !active) return;
    // Keep a deep-linked section visible on narrow screens without moving the page.
    const revealActive = () => {
      const navBounds = nav.getBoundingClientRect();
      const activeBounds = active.getBoundingClientRect();
      if (activeBounds.left < navBounds.left) {
        nav.scrollBy({ left: activeBounds.left - navBounds.left });
      } else if (activeBounds.right > navBounds.right) {
        nav.scrollBy({ left: activeBounds.right - navBounds.right });
      }
    };
    revealActive();
    const observer = new ResizeObserver(revealActive);
    observer.observe(nav);
    return () => observer.disconnect();
  }, [section, settings.isSuccess]);

  const previousSettings = useRef<Settings | undefined>(undefined);
  const [storeContent, setStoreContent] = useState(true);
  const [spendCap, setSpendCap] = useState(0);
  const [allowedModels, setAllowedModels] = useState<AllowedModelEntry[]>([]);
  const [personaName, setPersonaName] = useState("");
  const [personaPersonality, setPersonaPersonality] = useState("");
  const [personaTone, setPersonaTone] = useState("");
  const [disableImageAttachments, setDisableImageAttachments] = useState(false);

  useEffect(() => {
    const next = settings.data;
    if (!next) return;
    const previous = previousSettings.current?.customer_id === next.customer_id
      ? previousSettings.current : undefined;
    // Refresh untouched fields while preserving edits, even if another admin saves.
    setStoreContent((current) =>
      !previous || current === (previous.store_conversation_content ?? true)
        ? (next.store_conversation_content ?? true)
        : current,
    );
    setSpendCap((current) =>
      !previous || current === (previous.spend_cap_cents ?? 0)
        ? (next.spend_cap_cents ?? 0)
        : current,
    );
    setAllowedModels((current) =>
      !previous ||
      JSON.stringify(current) === JSON.stringify(previous.allowed_models ?? [])
        ? (next.allowed_models ?? [])
        : current,
    );
    setPersonaName((current) =>
      !previous || current.trim() === (previous.ai_persona_name ?? "").trim()
        ? (next.ai_persona_name ?? "")
        : current,
    );
    setPersonaPersonality((current) =>
      !previous ||
      current.trim() === (previous.ai_persona_personality ?? "").trim()
        ? (next.ai_persona_personality ?? "")
        : current,
    );
    setPersonaTone((current) =>
      !previous || current.trim() === (previous.ai_persona_tone ?? "").trim()
        ? (next.ai_persona_tone ?? "")
        : current,
    );
    setDisableImageAttachments((current) =>
      !previous || current === (previous.disable_image_attachments ?? false)
        ? (next.disable_image_attachments ?? false)
        : current,
    );
    previousSettings.current = next;
  }, [settings.data]);

  const currentState = useMemo(
    () =>
      JSON.stringify({
        storeContent,
        spendCap,
        allowedModels,
        personaName: personaName.trim(),
        personaPersonality: personaPersonality.trim(),
        personaTone: personaTone.trim(),
        disableImageAttachments,
      }),
    [
      storeContent,
      spendCap,
      allowedModels,
      disableImageAttachments,
      personaName,
      personaPersonality,
      personaTone,
    ],
  );
  const savedState = useMemo(
    () =>
      settings.data
        ? JSON.stringify({
            storeContent: settings.data.store_conversation_content ?? true,
            spendCap: settings.data.spend_cap_cents ?? 0,
            allowedModels: settings.data.allowed_models ?? [],
            personaName: (settings.data.ai_persona_name ?? "").trim(),
            personaPersonality: (
              settings.data.ai_persona_personality ?? ""
            ).trim(),
            personaTone: (settings.data.ai_persona_tone ?? "").trim(),
            disableImageAttachments:
              settings.data.disable_image_attachments ?? false,
          })
        : currentState,
    [currentState, settings.data],
  );
  const dirty = currentState !== savedState;
  const blocker = useBlocker({
    shouldBlockFn: ({ next }) =>
      (dirty || cloudDirty) &&
      (next.pathname !== "/workspace" ||
        (next.search as { view?: string }).view !== "settings"),
    enableBeforeUnload: dirty || cloudDirty,
    withResolver: true,
  });

  const save = useMutation({
    mutationFn: () => {
      const patch = {
        store_conversation_content: storeContent,
        spend_cap_cents: spendCap,
        allowed_models: allowedModels,
        ai_persona_name: personaName.trim() || null,
        ai_persona_personality: personaPersonality.trim() || null,
        ai_persona_tone: personaTone.trim() || null,
        disable_image_attachments: disableImageAttachments,
      };
      // Save only edited fields: another settings section may update this record.
      const defaults = {
        ...settings.data,
        store_conversation_content:
          settings.data?.store_conversation_content ?? true,
        spend_cap_cents: settings.data?.spend_cap_cents ?? 0,
        allowed_models: settings.data?.allowed_models ?? [],
        disable_image_attachments:
          settings.data?.disable_image_attachments ?? false,
      };
      return workspaceApi.patchSettings(
        Object.fromEntries(
          Object.entries(patch).filter(
            ([key, value]) =>
              JSON.stringify(value) !==
              JSON.stringify(defaults[key as keyof typeof defaults] ?? null),
          ),
        ),
      );
    },
    onSuccess: async () => {
      const fresh = await workspaceApi.getSettings();
      qc.setQueryData(["workspace", "settings"], fresh);
    },
  });

  if (settings.isLoading) return <SkeletonRows count={5} />;
  if (settings.error)
    return (
      <SecurityNotice title="Settings could not be loaded" tone="warning">
        <p>{settings.error.message}</p>
        <Button
          variant="outline"
          className="mt-3"
          onClick={() => settings.refetch()}
        >
          Retry
        </Button>
      </SecurityNotice>
    );

  const strictModelPolicy = allowedModels.length > 0;

  return (
    <div>
      <nav
        ref={sectionsNav}
        aria-label="Settings sections"
        className="mb-5 flex gap-1 overflow-x-auto border-b border-border"
      >
        {(
          [
            ["general", "General", "Workspace name and AI identity"],
            ["models", "Models & AI", "Models employees can use"],
            [
              "privacy",
              "Privacy & data",
              "Conversation storage and attachments",
            ],
            [
              "usage",
              "Spending & billing",
              "Usage limits and provider charges",
            ],
          ] as const
        ).map(([id, label, description]) => (
          <button
            type="button"
            key={id}
            aria-current={section === id ? "page" : undefined}
            title={description}
            onClick={() => onSectionChange?.(id)}
            className={cn(
              "relative min-h-11 shrink-0 cursor-pointer whitespace-nowrap rounded-t-lg px-4 py-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:rounded-full after:transition-colors",
              section === id
                ? "bg-muted text-foreground after:bg-foreground"
                : "text-muted-foreground hover:bg-muted hover:text-foreground after:bg-transparent hover:after:bg-border",
            )}
          >
            {label}
          </button>
        ))}
      </nav>
      {ext.settingsSection ? (
        <div className="mb-5">
          {ext.settingsSection(section, setCloudDirty)}
        </div>
      ) : null}
      <SectionHeader
        className="flex-wrap gap-3 [&>div:first-child]:basis-full sm:flex-nowrap sm:[&>div:first-child]:basis-auto"
        title={
          {
            general: "Shared AI identity",
            models: "Approved models",
            privacy: "Conversation privacy",
            usage: "Workspace spending limit",
          }[section]
        }
        description="Changes apply across the employee workspace. Save before leaving this page."
        action={
          <div className="flex items-center gap-2">
            {!dirty && save.isSuccess ? (
              <span className="inline-flex items-center gap-1 text-xs text-success">
                <Check className="h-3 w-3" /> Saved
              </span>
            ) : null}
            <Button
              size="sm"
              disabled={!dirty || save.isPending}
              onClick={() => save.mutate()}
            >
              <Save data-icon="inline-start" />{" "}
              {save.isPending ? "Saving…" : "Save AI settings"}
            </Button>
          </div>
        }
      />

      {section === "privacy" && (
        <section className="rounded-xl border border-border bg-card p-5">
          <h2 className="text-[13px] font-bold tracking-tight">
            Conversation storage
          </h2>
          <label className="mt-3 flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={!storeContent}
              onChange={(e) => setStoreContent(!e.target.checked)}
            />{" "}
            Do not store conversation content
          </label>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            Temporary chats keep their content in the open browser tab. Leaving
            chat, refreshing or closing it loses the conversation text. Bastio
            retains usage, cost and security-decision metadata, without prompt
            text, attachments or security snippets.
          </p>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            This applies to new writes. Previously saved content and archives
            remain. Knowledge documents are stored separately. Model providers
            apply their own retention policies. Conversation content is not
            collected for training.
          </p>
        </section>
      )}
      {section === "usage" && (
        <section className="rounded-xl border border-border bg-card p-5">
          <h2 className="text-[13px] font-bold tracking-tight">
            Monthly Workspace spending limit
          </h2>
          <label className="mt-2 flex items-center gap-2 text-xs">
            USD{" "}
            <Input
              aria-label="Monthly spending limit in USD"
              type="number"
              min="0"
              step="0.01"
              value={spendCap / 100}
              onChange={(e) =>
                setSpendCap(
                  Math.max(0, Math.round(Number(e.target.value) * 100)),
                )
              }
              className="max-w-32"
            />
          </label>
          <p className="mt-2 text-xs text-muted-foreground">
            Zero removes the limit. Estimated model costs count across
            employees; near-limit requests reserve their maximum allowance
            before sending. Responses allow up to 8,192 output tokens. Resets
            each calendar month in UTC.
          </p>
        </section>
      )}
      {section === "privacy" && (
        <section className="mt-5 overflow-hidden rounded-xl border border-border/70 bg-card">
          <div className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex min-w-0 items-start gap-3">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
                <ImageOff className="h-3.5 w-3.5" />
              </div>
              <div>
                <h2 className="text-[13px] font-semibold text-foreground">
                  Image attachment policy
                </h2>
                <p className="mt-0.5 max-w-2xl text-xs leading-relaxed text-muted-foreground">
                  Block images in employee chat when every attachment must pass
                  text extraction and security inspection.
                </p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={disableImageAttachments}
              onClick={() => setDisableImageAttachments((current) => !current)}
              className={cn(
                "inline-flex h-8 shrink-0 items-center gap-2 rounded-lg border px-2.5 text-xs font-medium transition-colors",
                disableImageAttachments
                  ? "border-success-border bg-success-bg text-success"
                  : "border-border/70 bg-background text-muted-foreground hover:bg-muted/40",
              )}
            >
              <span
                className={cn(
                  "h-2 w-2 rounded-full",
                  disableImageAttachments
                    ? "bg-success"
                    : "bg-muted-foreground/50",
                )}
              />
              {disableImageAttachments ? "Images blocked" : "Images allowed"}
            </button>
          </div>
          {!disableImageAttachments ? (
            <div className="border-t border-border/60 px-4 py-3">
              <SecurityNotice
                title="Images bypass text-only inspection"
                tone="warning"
              >
                Image uploads are rendered as visual context and are not
                text-extracted. Disable them for regulated or high-assurance
                workspaces.
              </SecurityNotice>
            </div>
          ) : null}
        </section>
      )}

      <div className="space-y-5">
        {section === "general" && (
          <section className="rounded-xl border border-border/70 bg-card">
            <div className="border-b border-border/60 px-4 py-3.5">
              <div className="flex items-center gap-2">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
                  <Sparkles className="h-3.5 w-3.5" />
                </div>
                <div>
                  <h2 className="text-[13px] font-semibold text-foreground">
                    Shared AI identity
                  </h2>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    A consistent persona layered onto every assistant.
                  </p>
                </div>
              </div>
            </div>
            <div className="space-y-4 p-4">
              <div>
                <FieldLabel optional htmlFor="persona-name">
                  Name
                </FieldLabel>
                <Input
                  id="persona-name"
                  value={personaName}
                  onChange={(event) => setPersonaName(event.target.value)}
                  placeholder="e.g. Bob"
                  maxLength={120}
                />
                <p className="mt-1 text-xs text-muted-foreground">
                  Shown as the shared AI identity in the portal.
                </p>
              </div>
              <div>
                <FieldLabel optional htmlFor="persona-personality">
                  Personality
                </FieldLabel>
                <Input
                  id="persona-personality"
                  value={personaPersonality}
                  onChange={(event) =>
                    setPersonaPersonality(event.target.value)
                  }
                  placeholder="e.g. helpful, precise, concise"
                  maxLength={500}
                />
              </div>
              <div>
                <FieldLabel optional htmlFor="persona-tone">
                  Tone of voice
                </FieldLabel>
                <Input
                  id="persona-tone"
                  value={personaTone}
                  onChange={(event) => setPersonaTone(event.target.value)}
                  placeholder="e.g. professional and direct"
                  maxLength={200}
                />
              </div>
              <SecurityNotice
                title="Assistant instructions still apply"
                tone="info"
              >
                Blank fields add no workspace-level persona. Each assistant’s
                own system prompt remains the source of its role and boundaries.
              </SecurityNotice>
            </div>
          </section>
        )}

        {section === "models" && (
          <section className="rounded-xl border border-border/70 bg-card">
            <div className="flex items-start justify-between gap-4 border-b border-border/60 px-4 py-3.5">
              <div className="flex items-center gap-2">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
                  <LockKeyhole className="h-3.5 w-3.5" />
                </div>
                <div>
                  <h2 className="text-[13px] font-semibold text-foreground">
                    Choose models
                  </h2>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Control the models employees can select at chat time.
                  </p>
                </div>
              </div>
              <Badge variant={strictModelPolicy ? "success" : "warning"}>
                {strictModelPolicy ? "Restricted" : "Catalog default"}
              </Badge>
            </div>
            <div className="space-y-4 p-4">
              {!strictModelPolicy ? (
                <SecurityNotice
                  title="All curated models are currently available"
                  tone="warning"
                >
                  With no models selected, employees can use the full curated
                  catalog. Select approved models below to enforce a strict
                  allowlist in both the portal UI and chat send path.
                </SecurityNotice>
              ) : (
                <SecurityNotice
                  title="Only approved models are available"
                  tone="success"
                >
                  Only the selected provider and model pairs are available to
                  employees. Bastio checks this policy on every chat request.
                </SecurityNotice>
              )}
              <ModelWhitelistEditor
                value={allowedModels}
                onChange={setAllowedModels}
              />
            </div>
          </section>
        )}
      </div>

      {save.error ? (
        <SecurityNotice
          className="mt-5"
          title="Settings could not be saved"
          tone="warning"
        >
          {(save.error as Error).message}
        </SecurityNotice>
      ) : null}
      {dirty ? (
        <div className="sticky bottom-3 z-10 mt-5 flex items-center justify-between gap-4 rounded-xl border border-border/80 bg-popover/95 px-4 py-3 shadow-lg backdrop-blur">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <ShieldAlert className="h-3.5 w-3.5" /> Unsaved policy changes
          </div>
          <Button
            size="sm"
            disabled={save.isPending}
            onClick={() => save.mutate()}
          >
            <Save data-icon="inline-start" />{" "}
            {save.isPending ? "Saving…" : "Save policy"}
          </Button>
        </div>
      ) : null}

      {!ext.settingsSection && ext.settingsExtra ? (
        <div className="mt-5">{ext.settingsExtra}</div>
      ) : null}
      <Dialog
        open={blocker.status === "blocked"}
        onOpenChange={(open) => {
          if (!open) blocker.reset?.();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Leave without saving?</DialogTitle>
            <DialogDescription>
              You have unsaved workspace settings. Keep editing to save them, or
              leave to discard your changes.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => blocker.reset?.()}>
              Keep editing
            </Button>
            <Button onClick={() => blocker.proceed?.()}>
              Discard and leave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ModelWhitelistEditor({
  value,
  onChange,
}: {
  value: AllowedModelEntry[];
  onChange: (next: AllowedModelEntry[]) => void;
}) {
  const [search, setSearch] = useState("");
  const allowedKey = (entry: AllowedModelEntry) =>
    `${entry.provider}/${entry.model}`;
  const allowSet = new Set(value.map(allowedKey));
  const byProvider = MODEL_CATALOG.reduce<Record<string, typeof MODEL_CATALOG>>(
    (result, model) => {
      (result[model.provider] ??= []).push(model);
      return result;
    },
    {},
  );

  const toggleModel = (provider: string, model: string) => {
    const key = `${provider}/${model}`;
    onChange(
      allowSet.has(key)
        ? value.filter((entry) => allowedKey(entry) !== key)
        : [...value, { provider, model }],
    );
  };
  const toggleProvider = (provider: string, models: typeof MODEL_CATALOG) => {
    const allOn = models.every((model) =>
      allowSet.has(`${provider}/${model.model}`),
    );
    const otherProviders = value.filter((entry) => entry.provider !== provider);
    onChange(
      allOn
        ? otherProviders
        : [
            ...otherProviders,
            ...models.map((model) => ({ provider, model: model.model })),
          ],
    );
  };

  return (
    <div className="space-y-3">
      <Input
        aria-label="Search models"
        placeholder="Search models or providers…"
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      <p className="text-xs text-muted-foreground">
        {value.length
          ? `${value.length} approved models selected`
          : "All curated models allowed"}
        . Expand a provider to choose models.
      </p>
      {Object.entries(byProvider).map(([provider, models]) => {
        const visible = models.filter((model) =>
          `${provider} ${model.label} ${model.model}`
            .toLowerCase()
            .includes(search.toLowerCase()),
        );
        if (!visible.length) return null;
        const selected = models.filter((model) =>
          allowSet.has(`${provider}/${model.model}`),
        );
        const allOn = selected.length === models.length;
        const someOn = selected.length > 0;
        return (
          <details
            key={provider}
            open={search.trim() ? true : undefined}
            className="overflow-hidden rounded-lg border border-border/70"
          >
            <summary className="flex min-h-11 cursor-pointer items-center justify-between gap-3 bg-muted/20 px-3 py-3 text-sm font-medium capitalize">
              {provider}
              <span className="text-xs font-normal normal-case text-muted-foreground">
                {selected.length}/{models.length} selected · expand
              </span>
            </summary>
            <label className="flex cursor-pointer items-center gap-3 border-b border-border/50 bg-muted/20 px-3 py-2.5">
              <input
                type="checkbox"
                checked={allOn}
                ref={(element) => {
                  if (element) element.indeterminate = !allOn && someOn;
                }}
                onChange={() => toggleProvider(provider, models)}
              />
              <span className="text-[12px] font-medium capitalize text-foreground">
                Select all {provider} models
              </span>
              <span className="ml-auto font-mono text-xs text-muted-foreground">
                {selected.length}/{models.length} selected
              </span>
            </label>
            <div className="grid sm:grid-cols-2">
              {visible.map((model, index) => {
                const selectedModel = allowSet.has(
                  `${provider}/${model.model}`,
                );
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
                    <input
                      type="checkbox"
                      checked={selectedModel}
                      onChange={() => toggleModel(provider, model.model)}
                    />
                    <div className="min-w-0">
                      <p
                        className={cn(
                          "truncate text-xs font-medium",
                          selectedModel
                            ? "text-foreground"
                            : "text-muted-foreground",
                        )}
                      >
                        {model.label}
                      </p>
                      <p className="truncate font-mono text-[9px] text-muted-foreground">
                        {model.model}
                      </p>
                    </div>
                  </label>
                );
              })}
            </div>
          </details>
        );
      })}
      {search &&
        !MODEL_CATALOG.some((model) =>
          `${model.provider} ${model.label} ${model.model}`
            .toLowerCase()
            .includes(search.toLowerCase()),
        ) && (
          <p className="py-4 text-sm text-muted-foreground">
            No models match “{search}”.
          </p>
        )}
      {value.length > 0 ? (
        <Button size="xs" variant="ghost" onClick={() => onChange([])}>
          Use the full curated catalog
        </Button>
      ) : null}
    </div>
  );
}
