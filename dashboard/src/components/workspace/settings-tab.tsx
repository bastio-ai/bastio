import { MCPHistorySettings } from "./mcp-history-settings";
import { ModelConnections } from "./model-connections";
import { workspaceModelChoices } from "./workspace-models";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { Check, Save } from "lucide-react";

import {
  AdminPanel,
  FieldLabel,
} from "@/components/admin/admin-primitives";
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
    const previous =
      previousSettings.current?.customer_id === next.customer_id
        ? previousSettings.current
        : undefined;
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
      qc.invalidateQueries({ queryKey: ["workspace", "mcp-history-policy"] });
    },
  });

  if (settings.isLoading) return <SkeletonRows count={5} />;
  if (settings.error)
    return (
      <div
        role="alert"
        className="space-y-3 border-l-2 border-warn pl-3.5 text-xs text-muted-foreground"
      >
        <p className="font-medium text-foreground">
          Settings could not be loaded
        </p>
        <p>{settings.error.message}</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          onClick={() => void settings.refetch()}
        >
          Retry
        </Button>
      </div>
    );

  const strictModelPolicy = allowedModels.length > 0;
  const saveAction = (
    <div className="flex items-center gap-2.5">
      {!dirty && save.isSuccess ? (
        <span
          role="status"
          className="inline-flex items-center gap-1.5 text-xs text-success"
        >
          <Check className="size-3.5" aria-hidden="true" /> Saved
        </span>
      ) : null}
      <Button
        type="button"
        size="sm"
        className="h-8 text-xs"
        disabled={!dirty || save.isPending}
        onClick={() => save.mutate()}
      >
        <Save data-icon="inline-start" aria-hidden="true" />{" "}
        {save.isPending ? "Saving…" : "Save AI settings"}
      </Button>
    </div>
  );

  return (
    <div>
      <nav
        ref={sectionsNav}
        aria-label="Settings sections"
        className="mb-6 flex gap-5 overflow-x-auto border-b border-border-subtle"
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
        ).map(([id, label, description]) => {
          const active = section === id;
          return (
            <button
              type="button"
              key={id}
              aria-current={active ? "page" : undefined}
              title={description}
              onClick={() => onSectionChange?.(id)}
              className={cn(
                "inline-flex shrink-0 cursor-pointer items-center gap-1.5 whitespace-nowrap border-b-2 px-0.5 pb-3 text-[12px] transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                active
                  ? "border-foreground/70 text-foreground"
                  : "border-transparent text-muted-foreground",
              )}
            >
              {label}
            </button>
          );
        })}
      </nav>

      {ext.settingsSection ? (
        <div className="mb-6">
          {ext.settingsSection(section, setCloudDirty)}
        </div>
      ) : null}

      {section === "general" && (
        <AdminPanel
          title="Shared AI identity"
          description="A consistent persona layered onto every assistant. Changes apply across the employee workspace."
          action={saveAction}
        >
          <div className="space-y-4 border-b border-border-subtle pb-4">
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
                onChange={(event) => setPersonaPersonality(event.target.value)}
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
            <div className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
              <span className="font-medium text-foreground">
                Assistant instructions still apply.
              </span>{" "}
              Blank fields add no workspace-level persona. Each assistant’s own
              system prompt remains the source of its role and boundaries.
            </div>
          </div>
        </AdminPanel>
      )}

      {section === "models" && (
        <div className="space-y-6">
          <ModelConnections />
          <AdminPanel
            title="Approved models"
            description="Control the models employees can select at chat time. Changes apply across the employee workspace."
            action={
              <div className="flex flex-wrap items-center gap-3">
                <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      strictModelPolicy ? "bg-success" : "bg-warn",
                    )}
                  />
                  {strictModelPolicy ? "Restricted" : "Catalog default"}
                </span>
                {saveAction}
              </div>
            }
          >
            <div className="space-y-4 border-b border-border-subtle pb-4">
              {!strictModelPolicy ? (
                <div className="border-l-2 border-warn pl-3.5 text-xs leading-relaxed text-muted-foreground">
                  <p className="font-medium text-foreground">
                    Connected provider models are currently allowed
                  </p>
                  <p className="mt-0.5">
                    With no models selected, employees can choose models
                    reported by connected providers, or suggestions where
                    discovery is unavailable. Select approved models below to
                    enforce a strict allowlist in both the portal UI and chat
                    send path.
                  </p>
                </div>
              ) : (
                <div className="border-l-2 border-success/60 pl-3.5 text-xs leading-relaxed text-muted-foreground">
                  <p className="font-medium text-foreground">
                    Only approved models are available
                  </p>
                  <p className="mt-0.5">
                    Only the selected provider and model pairs are available to
                    employees. Bastio checks this policy on every chat request.
                  </p>
                </div>
              )}
              <ModelWhitelistEditor
                value={allowedModels}
                onChange={setAllowedModels}
              />
            </div>
          </AdminPanel>
        </div>
      )}

      {section === "privacy" && (
        <div className="space-y-6">
          <AdminPanel
            title="Conversation privacy"
            description="Changes apply across the employee workspace. Save before leaving this page."
            action={saveAction}
            contentClassName="py-0"
          >
            <div className="divide-y divide-border-subtle border-b border-border-subtle">
              <div className="py-4">
                <h3 className="text-[13px] font-medium text-foreground">
                  Conversation storage
                </h3>
                <label
                  htmlFor="do-not-store-content"
                  className="mt-3 flex cursor-pointer items-center gap-2.5 text-xs text-foreground"
                >
                  <input
                    id="do-not-store-content"
                    type="checkbox"
                    className="accent-foreground"
                    checked={!storeContent}
                    onChange={(e) => setStoreContent(!e.target.checked)}
                  />
                  <span>Do not store conversation content</span>
                </label>
                <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                  Temporary chats keep their content in the open browser tab.
                  Leaving chat, refreshing or closing it loses the conversation
                  text. Bastio retains usage, cost and security-decision
                  metadata, without prompt text, attachments or security
                  snippets.
                </p>
                <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                  This applies to new conversation writes. Enabling it also
                  deletes retained MCP payload history and disables new MCP
                  capture. Previously saved conversation content and archives
                  remain. Knowledge documents are stored separately. Model
                  providers apply their own retention policies. Conversation
                  content is not collected for training.
                </p>
              </div>

              <div className="py-4">
                <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0">
                    <h3 className="text-[13px] font-medium text-foreground">
                      Image attachment policy
                    </h3>
                    <p className="mt-1 max-w-2xl text-xs leading-relaxed text-muted-foreground">
                      Block images in employee chat when every attachment must
                      pass text extraction and security inspection.
                    </p>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={disableImageAttachments}
                    onClick={() =>
                      setDisableImageAttachments((current) => !current)
                    }
                    className={cn(
                      "inline-flex h-8 shrink-0 cursor-pointer items-center gap-2 rounded-md border px-2.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                      disableImageAttachments
                        ? "border-border-subtle bg-surface-1/60 text-foreground"
                        : "border-border-subtle bg-background text-muted-foreground hover:bg-surface-1/50 hover:text-foreground",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 rounded-full",
                        disableImageAttachments
                          ? "bg-success"
                          : "bg-muted-foreground/50",
                      )}
                    />
                    {disableImageAttachments
                      ? "Images blocked"
                      : "Images allowed"}
                  </button>
                </div>
                {!disableImageAttachments ? (
                  <div className="mt-3 border-l-2 border-warn pl-3.5 text-xs leading-relaxed text-muted-foreground">
                    <span className="font-medium text-foreground">
                      Images bypass text-only inspection.
                    </span>{" "}
                    Image uploads are rendered as visual context and are not
                    text-extracted. Disable them for regulated or high-assurance
                    workspaces.
                  </div>
                ) : null}
              </div>
            </div>
          </AdminPanel>

          <MCPHistorySettings />
        </div>
      )}

      {section === "usage" && (
        <AdminPanel
          title="Workspace spending limit"
          description="Changes apply across the employee workspace. Save before leaving this page."
          action={saveAction}
        >
          <div className="space-y-3 border-b border-border-subtle pb-4">
            <FieldLabel htmlFor="workspace-spend-cap">
              Monthly Workspace spending limit
            </FieldLabel>
            <div className="flex items-center gap-2">
              <span className="font-mono text-xs text-muted-foreground">
                USD
              </span>
              <Input
                id="workspace-spend-cap"
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
                className="max-w-36 font-mono tabular-nums"
              />
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">
              Zero removes the limit. Estimated model costs count across
              employees; near-limit requests reserve their maximum allowance
              before sending. Responses allow up to 8,192 output tokens. Resets
              each calendar month in UTC.
            </p>
          </div>
        </AdminPanel>
      )}

      {save.error ? (
        <div
          role="alert"
          className="mt-5 border-l-2 border-destructive pl-3.5 text-xs text-destructive"
        >
          <span className="font-medium">Settings could not be saved:</span>{" "}
          {(save.error as Error).message}
        </div>
      ) : null}

      {dirty ? (
        <div className="sticky bottom-3 z-10 mt-6 flex items-center justify-between gap-4 border-y border-border-subtle bg-background/95 py-3 backdrop-blur">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span
              aria-hidden="true"
              className="size-1.5 shrink-0 rounded-full bg-warn"
            />
            <span>Unsaved policy changes</span>
          </div>
          <Button
            type="button"
            size="sm"
            className="h-8 text-xs"
            disabled={save.isPending}
            onClick={() => save.mutate()}
          >
            <Save data-icon="inline-start" aria-hidden="true" />{" "}
            {save.isPending ? "Saving…" : "Save policy"}
          </Button>
        </div>
      ) : null}

      {!ext.settingsSection && ext.settingsExtra ? (
        <div className="mt-6">{ext.settingsExtra}</div>
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
            <Button
              type="button"
              variant="outline"
              onClick={() => blocker.reset?.()}
            >
              Keep editing
            </Button>
            <Button type="button" onClick={() => blocker.proceed?.()}>
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
  const editorId = useId();
  const [search, setSearch] = useState("");
  const inventory = useQuery({
    queryKey: ["workspace", "models", true],
    queryFn: () => workspaceApi.modelInventory(true),
  });
  const catalog = workspaceModelChoices(inventory.data);
  const allowedKey = (entry: AllowedModelEntry) =>
    `${entry.provider}/${entry.model}`;
  const allowSet = new Set(value.map(allowedKey));
  const byProvider = catalog.reduce<Record<string, typeof MODEL_CATALOG>>(
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

  const hasVisibleProviders = Object.entries(byProvider).some(
    ([provider, models]) =>
      models.some((model) =>
        `${provider} ${model.label} ${model.model}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ),
  );

  return (
    <div className="space-y-3">
      <search aria-label="Filter models">
        <label htmlFor={`${editorId}-search`} className="sr-only">
          Search models
        </label>
        <Input
          id={`${editorId}-search`}
          aria-label="Search models"
          placeholder="Search models or providers…"
          value={search}
          autoCorrect="off"
          autoCapitalize="none"
          spellCheck={false}
          onChange={(event) => setSearch(event.target.value)}
        />
      </search>
      <p className="font-mono text-xs tabular-nums text-muted-foreground">
        {value.length
          ? `${value.length} approved models selected`
          : "Connected provider models allowed"}
        <span className="font-sans">. Expand a provider to choose models.</span>
      </p>

      {hasVisibleProviders ? (
        <div className="divide-y divide-border-subtle border-y border-border-subtle">
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
            const selectAllId = `${editorId}-all-${provider}`;
            return (
              <details
                key={provider}
                open={search.trim() ? true : undefined}
                className="group"
              >
                <summary className="flex cursor-pointer list-none items-center justify-between gap-3 py-3 text-[13px] font-medium capitalize text-foreground transition-colors hover:bg-surface-1/50 [&::-webkit-details-marker]:hidden">
                  <span className="flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        someOn ? "bg-success" : "bg-muted-foreground/50",
                      )}
                    />
                    {provider}
                  </span>
                  <span className="font-mono text-[11px] font-normal normal-case tabular-nums text-muted-foreground">
                    {selected.length}/{models.length} selected · expand
                  </span>
                </summary>
                <label
                  htmlFor={selectAllId}
                  className="flex cursor-pointer items-center gap-3 border-t border-border-subtle bg-surface-1/30 px-2 py-2.5 transition-colors hover:bg-surface-1/50"
                >
                  <input
                    id={selectAllId}
                    type="checkbox"
                    className="accent-foreground"
                    checked={allOn}
                    ref={(element) => {
                      if (element) element.indeterminate = !allOn && someOn;
                    }}
                    onChange={() => toggleProvider(provider, models)}
                  />
                  <span className="text-xs font-medium capitalize text-foreground">
                    Select all {provider} models
                  </span>
                  <span className="ml-auto font-mono text-[11px] tabular-nums text-muted-foreground">
                    {selected.length}/{models.length} selected
                  </span>
                </label>
                <div className="grid border-t border-border-subtle sm:grid-cols-2">
                  {visible.map((model, index) => {
                    const selectedModel = allowSet.has(
                      `${provider}/${model.model}`,
                    );
                    const modelInputId = `${editorId}-${provider}-${index}`;
                    return (
                      <label
                        key={`${provider}/${model.model}`}
                        htmlFor={modelInputId}
                        className={cn(
                          "flex cursor-pointer items-center gap-2.5 px-2.5 py-2.5 transition-colors hover:bg-surface-1/50",
                          index > 0 &&
                            "border-t border-border-subtle sm:border-t-0",
                          index % 2 === 1 &&
                            "sm:border-l sm:border-border-subtle",
                          index >= 2 && "sm:border-t sm:border-border-subtle",
                        )}
                      >
                        <input
                          id={modelInputId}
                          type="checkbox"
                          className="accent-foreground"
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
                          <p className="truncate font-mono text-[10px] text-muted-foreground">
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
        </div>
      ) : null}

      {search &&
        !catalog.some((model) =>
          `${model.provider} ${model.label} ${model.model}`
            .toLowerCase()
            .includes(search.toLowerCase()),
        ) && (
          <p className="py-4 text-xs text-muted-foreground">
            No models match “{search}”.
          </p>
        )}

      {value.length > 0 ? (
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => onChange([])}
        >
          Allow connected provider models
        </Button>
      ) : null}
    </div>
  );
}
