import {
  ResourceSharingButton,
  SharingPicker,
  sharingLabel,
  everyoneSharing,
  type ResourceSharing,
} from "./sharing-controls";
import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Plus, ShieldCheck } from "lucide-react";

import {
  AdminPanel,
  AdminSummaryStrip,
  FieldLabel,
} from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { SkeletonRows } from "@/components/skeleton";
import { http, unwrap } from "@/api/typed";
import { cn } from "@/lib/utils";
import { workspaceApi, type Assistant, type KnowledgeSource } from "./types";
import {
  DEFAULT_MODEL,
  DEFAULT_MODELS,
  MODEL_PROVIDERS,
  providerLabel,
  type ModelProvider,
} from "./model-picker";

export function AssistantsTab() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["workspace", "assistants"],
    queryFn: workspaceApi.listAssistants,
  });
  const [editing, setEditing] = useState<Assistant | null>(null);
  const [creating, setCreating] = useState(false);
  const [archiveTarget, setArchiveTarget] = useState<Assistant | null>(null);

  const archive = useMutation({
    mutationFn: workspaceApi.archiveAssistant,
    onSuccess: () => {
      setArchiveTarget(null);
      qc.invalidateQueries({ queryKey: ["workspace", "assistants"] });
    },
  });

  const assistants = list.data?.assistants ?? [];
  const defaultAssistant = assistants.find((item) => item.is_default);
  const knowledgeAssignments = assistants.reduce(
    (total, item) => total + (item.knowledge_source_ids?.length ?? 0),
    0,
  );
  const toolAssignments = assistants.reduce(
    (total, item) => total + (item.mcp_connection_ids?.length ?? 0),
    0,
  );

  return (
    <div>
      <AdminSummaryStrip
        items={[
          {
            label: "Configured",
            value: list.isLoading ? "—" : assistants.length,
            detail: "Available in the portal",
          },
          {
            label: "Default assistant",
            value: list.isLoading ? "—" : defaultAssistant ? 1 : 0,
            detail: defaultAssistant
              ? defaultAssistant.name
              : "Not set for new chats",
            tone: defaultAssistant ? "success" : "default",
          },
          {
            label: "Knowledge access",
            value: list.isLoading ? "—" : knowledgeAssignments,
            detail: "Source assignments across assistants",
          },
          {
            label: "Shared tools",
            value: list.isLoading ? "—" : toolAssignments,
            detail: "MCP integrations attached",
          },
        ]}
      />

      <AdminPanel
        title="Assistant inventory"
        description="Create purpose-built assistants and limit each one to the knowledge and tools it needs."
        action={
          <Button
            type="button"
            size="sm"
            className="h-8 text-xs"
            onClick={() => setCreating(true)}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            New assistant
          </Button>
        }
        contentClassName="py-0"
      >
        {list.isLoading ? (
          <div className="border-b border-border-subtle py-4">
            <SkeletonRows count={4} />
          </div>
        ) : assistants.length === 0 ? (
          <div className="border-b border-border-subtle py-10 text-center">
            <h3 className="text-[13px] font-medium text-foreground">
              Create your first governed assistant
            </h3>
            <p className="mx-auto mt-1 max-w-md text-xs leading-relaxed text-muted-foreground">
              Set a role, model default, response language, and approved
              knowledge sources. Employees will only see assistants you
              configure here.
            </p>
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="mt-4 h-8 text-xs"
              onClick={() => setCreating(true)}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              New assistant
            </Button>
          </div>
        ) : (
          <div
            tabIndex={0}
            role="region"
            aria-label="Assistant inventory"
            className="overflow-x-auto border-b border-border-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <table className="w-full min-w-[640px] border-collapse text-left">
              <thead>
                <tr className="border-b border-border-subtle text-xs text-muted-foreground">
                  <th scope="col" className="py-2.5 pr-4 font-medium">
                    Assistant
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Model
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Knowledge
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Access
                  </th>
                  <th
                    scope="col"
                    className="py-2.5 pl-4 text-right font-medium"
                  >
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {assistants.map((assistant) => {
                  const sourceCount = assistant.knowledge_source_ids?.length ?? 0;
                  return (
                    <tr
                      key={assistant.id}
                      className="transition-colors hover:bg-surface-1/50"
                    >
                      <td className="max-w-[320px] py-3.5 pr-4 align-middle">
                        <div className="flex items-center gap-2">
                          <span
                            aria-hidden="true"
                            className={cn(
                              "size-1.5 shrink-0 rounded-full",
                              assistant.is_default
                                ? "bg-success"
                                : "bg-muted-foreground/50",
                            )}
                          />
                          <span className="truncate text-[13px] font-medium text-foreground">
                            {assistant.name}
                          </span>
                          {assistant.is_default ? (
                            <span className="shrink-0 font-mono text-[11px] text-success">
                              Default
                            </span>
                          ) : null}
                        </div>
                        <p className="mt-0.5 truncate pl-3.5 text-xs text-muted-foreground">
                          {assistant.description || "No description provided"}
                        </p>
                      </td>
                      <td className="px-4 py-3.5 align-middle">
                        <p className="font-mono text-[11px] text-foreground">
                          {assistant.default_model}
                        </p>
                        <p className="mt-0.5 text-[11px] capitalize text-muted-foreground">
                          {assistant.default_provider}
                        </p>
                      </td>
                      <td className="px-4 py-3.5 align-middle font-mono text-[11px] tabular-nums text-muted-foreground">
                        {sourceCount} {sourceCount === 1 ? "source" : "sources"}
                      </td>
                      <td className="px-4 py-3.5 align-middle text-xs text-muted-foreground">
                        {sharingLabel(assistant.sharing)}
                      </td>
                      <td className="py-3.5 pl-4 text-right align-middle">
                        <div className="inline-flex items-center justify-end gap-1">
                          <ResourceSharingButton
                            type="assistant"
                            id={assistant.id}
                            name={assistant.name}
                            sharing={assistant.sharing}
                          />
                          <Button
                            type="button"
                            size="xs"
                            variant="ghost"
                            onClick={() => setEditing(assistant)}
                          >
                            Edit
                          </Button>
                          <Button
                            type="button"
                            size="icon-xs"
                            variant="ghost"
                            aria-label={`Archive ${assistant.name}`}
                            onClick={() => setArchiveTarget(assistant)}
                          >
                            <Archive aria-hidden="true" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </AdminPanel>

      <div className="mt-6 border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground">
          Least-privilege knowledge access.
        </span>{" "}
        Knowledge is assigned per assistant. Sources that are not selected are
        never retrieved into that assistant’s prompt context.
      </div>

      <AssistantEditor
        key={editing?.id ?? (creating ? "new" : "closed")}
        open={creating || Boolean(editing)}
        initial={editing}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={() =>
          qc.invalidateQueries({ queryKey: ["workspace", "assistants"] })
        }
      />

      <Dialog
        open={Boolean(archiveTarget)}
        onOpenChange={(open) => !open && setArchiveTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive assistant?</DialogTitle>
            <DialogDescription>
              {archiveTarget?.name} will no longer be available for new portal
              conversations. Existing conversation history is preserved.
            </DialogDescription>
          </DialogHeader>
          {archive.error ? (
            <p role="alert" className="text-xs text-destructive">
              {(archive.error as Error).message}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setArchiveTarget(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={archive.isPending}
              onClick={() => archiveTarget && archive.mutate(archiveTarget.id)}
            >
              {archive.isPending ? "Archiving…" : "Archive assistant"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function AssistantEditor({
  open,
  initial,
  onClose,
  onSaved,
}: {
  open: boolean;
  initial: Assistant | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const uid = useId();
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [systemPrompt, setSystemPrompt] = useState(initial?.system_prompt ?? "");
  const [provider, setProvider] = useState<ModelProvider>(
    initial?.default_provider ?? DEFAULT_MODEL.provider,
  );
  const [model, setModel] = useState(
    initial?.default_model ?? DEFAULT_MODEL.model,
  );
  const [isDefault, setIsDefault] = useState(initial?.is_default ?? false);
  const [language, setLanguage] = useState(initial?.language ?? "");
  const [suggestedPrompts, setSuggestedPrompts] = useState(
    (initial?.suggested_prompts ?? []).join("\n"),
  );
  const [sharing, setSharing] = useState<ResourceSharing>(
    initial?.sharing ?? everyoneSharing(),
  );
  const [selectedTools, setSelectedTools] = useState<string[]>(
    initial?.mcp_connection_ids ?? [],
  );
  const integrations = useQuery({
    queryKey: ["workspace-mcp-integrations"],
    queryFn: () => unwrap(http.GET("/v1/workspace/mcp")),
    staleTime: 0,
  });
  const sharedTools = (integrations.data?.connections ?? []).filter(
    (c) => c.scope === "workspace",
  );
  const [selectedKB, setSelectedKB] = useState<string[]>(
    initial?.knowledge_source_ids ?? [],
  );
  const knowledge = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
    staleTime: 60_000,
  });
  const availableKB: KnowledgeSource[] = knowledge.data?.sources ?? [];
  const providerModels = DEFAULT_MODELS.filter(
    (item) => item.provider === provider,
  );
  const currentModelKnown = providerModels.some((item) => item.model === model);

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        name,
        description,
        system_prompt: systemPrompt,
        default_provider: provider,
        default_model: model,
        is_default: isDefault,
        language: language.trim() || null,
        suggested_prompts: suggestedPrompts
          .split("\n")
          .map((item) => item.trim())
          .filter(Boolean),
        knowledge_source_ids: selectedKB,
        mcp_connection_ids: selectedTools,
        sharing,
      };
      return initial
        ? workspaceApi.updateAssistant(initial.id, body)
        : workspaceApi.createAssistant(body);
    },
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });

  const nameId = `${uid}-name`;
  const descriptionId = `${uid}-description`;
  const systemPromptId = `${uid}-system-prompt`;
  const providerId = `${uid}-provider`;
  const modelId = `${uid}-model`;
  const languageId = `${uid}-language`;
  const startersId = `${uid}-starters`;

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {initial ? "Edit assistant" : "New assistant"}
          </DialogTitle>
          <DialogDescription>
            Configure the assistant’s purpose, model defaults, and explicit
            knowledge permissions.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 py-1 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <FieldLabel htmlFor={nameId}>Name</FieldLabel>
            <Input
              id={nameId}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="e.g. Security policy advisor"
              autoFocus
            />
          </div>
          <div className="sm:col-span-2">
            <FieldLabel htmlFor={descriptionId} optional>
              Description
            </FieldLabel>
            <Input
              id={descriptionId}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="What employees should use this assistant for"
            />
          </div>
          <div className="sm:col-span-2">
            <FieldLabel htmlFor={systemPromptId}>System prompt</FieldLabel>
            <textarea
              id={systemPromptId}
              value={systemPrompt}
              onChange={(event) => setSystemPrompt(event.target.value)}
              rows={6}
              placeholder="Define the role, boundaries, and escalation behavior…"
              className="w-full resize-y rounded-md border border-input bg-transparent px-2.5 py-2 font-mono text-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>
          <div>
            <FieldLabel htmlFor={providerId}>Provider</FieldLabel>
            <select
              id={providerId}
              value={provider}
              onChange={(event) => {
                const next = event.target.value as ModelProvider;
                setProvider(next);
                const suggested = DEFAULT_MODELS.find(
                  (item) => item.provider === next,
                );
                if (suggested) setModel(suggested.model);
              }}
              className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              {MODEL_PROVIDERS.map((item) => (
                <option key={item} value={item}>
                  {providerLabel(item)}
                </option>
              ))}
            </select>
          </div>
          <div>
            <FieldLabel htmlFor={modelId}>Default model</FieldLabel>
            {providerModels.length > 0 ? (
              <select
                id={modelId}
                value={model}
                onChange={(event) => setModel(event.target.value)}
                className="h-8 w-full rounded-md border border-input bg-background px-2.5 font-mono text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                {!currentModelKnown ? (
                  <option value={model}>{model}</option>
                ) : null}
                {providerModels.map((item) => (
                  <option key={item.model} value={item.model}>
                    {item.label}
                  </option>
                ))}
              </select>
            ) : (
              <Input
                id={modelId}
                value={model}
                onChange={(event) => setModel(event.target.value)}
                className="font-mono"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            )}
          </div>
          <div className="sm:col-span-2">
            <FieldLabel htmlFor={languageId}>Response language</FieldLabel>
            <select
              id={languageId}
              value={language}
              onChange={(event) => setLanguage(event.target.value)}
              className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <option value="">Auto-detect from each user message</option>
              <option value="en">English</option>
              <option value="es">Spanish</option>
              <option value="fr">French</option>
              <option value="de">German</option>
              <option value="it">Italian</option>
              <option value="pt">Portuguese</option>
              <option value="nl">Dutch</option>
              <option value="da">Danish</option>
              <option value="sv">Swedish</option>
              <option value="ja">Japanese</option>
              <option value="zh">Chinese</option>
            </select>
          </div>

          <div className="sm:col-span-2">
            <FieldLabel htmlFor={startersId} optional>
              Conversation starters
            </FieldLabel>
            <textarea
              id={startersId}
              value={suggestedPrompts}
              onChange={(event) => setSuggestedPrompts(event.target.value)}
              rows={3}
              placeholder={
                "Summarize our incident response policy\nWho owns a security escalation?"
              }
              className="w-full resize-y rounded-md border border-input bg-transparent px-2.5 py-2 text-xs leading-relaxed outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              One prompt per line. These appear as optional shortcuts when
              employees start a conversation.
            </p>
          </div>

          <div className="sm:col-span-2">
            <FieldLabel optional>Knowledge permissions</FieldLabel>
            {availableKB.length === 0 ? (
              <p className="border-y border-border-subtle py-3 text-xs text-muted-foreground">
                No knowledge sources are available yet.
              </p>
            ) : (
              <div className="max-h-44 divide-y divide-border-subtle overflow-y-auto border-y border-border-subtle">
                {availableKB.map((item) => {
                  const checked = selectedKB.includes(item.id);
                  return (
                    <label
                      key={item.id}
                      className="flex cursor-pointer items-center gap-3 px-2 py-2 transition-colors hover:bg-surface-1/50"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(event) =>
                          setSelectedKB((current) =>
                            event.target.checked
                              ? [...current, item.id]
                              : current.filter((id) => id !== item.id),
                          )
                        }
                      />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-xs font-medium text-foreground">
                          {item.name}
                        </p>
                        <p className="font-mono text-[11px] text-muted-foreground">
                          {item.type} · {item.status}
                        </p>
                      </div>
                      <span
                        aria-hidden="true"
                        className={cn(
                          "size-1.5 shrink-0 rounded-full",
                          item.status === "ready"
                            ? "bg-success"
                            : item.status === "failed" ||
                                item.status === "quarantined"
                              ? "bg-destructive"
                              : "bg-warn",
                        )}
                      />
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        <SharingPicker
          value={sharing}
          onChange={setSharing}
          disabled={save.isPending}
        />
        <p className="text-xs leading-relaxed text-muted-foreground">
          Attached knowledge keeps its own sharing rules. Employees need access
          to both the assistant and a source before that source can be retrieved.
        </p>

        <div className="border-l-2 border-warn pl-3.5 text-xs leading-relaxed text-muted-foreground">
          <span className="font-medium text-foreground">
            System prompts may contain sensitive operating instructions.
          </span>{" "}
          Avoid secrets and credentials. Selected knowledge excerpts are added
          only when relevant, but the assistant prompt is applied to every
          message.
        </div>

        <label className="flex cursor-pointer items-center gap-2.5 border-y border-border-subtle py-2.5 text-xs text-foreground">
          <input
            type="checkbox"
            checked={isDefault}
            onChange={(event) => setIsDefault(event.target.checked)}
          />
          <ShieldCheck
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          Use as the default assistant for new conversations
        </label>

        <div className="space-y-2 border-b border-border-subtle pb-3">
          <FieldLabel optional>Shared integrations</FieldLabel>
          <p className="text-xs leading-relaxed text-muted-foreground">
            Employees can enable these tools in chat. Each proposed action needs
            their approval, and integration sharing still applies.
          </p>
          {sharedTools.length > 0 ? (
            <div className="divide-y divide-border-subtle border-t border-border-subtle">
              {sharedTools.map((c) => (
                <label
                  key={c.id}
                  className="flex cursor-pointer items-center gap-2.5 py-2 text-xs text-foreground transition-colors hover:bg-surface-1/50"
                >
                  <input
                    type="checkbox"
                    checked={selectedTools.includes(c.id)}
                    onChange={(e) =>
                      setSelectedTools(
                        e.target.checked
                          ? [...selectedTools, c.id]
                          : selectedTools.filter((id) => id !== c.id),
                      )
                    }
                  />
                  <span className="truncate">{c.name}</span>
                </label>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              Add a shared connection in Workspace → Integrations first.
            </p>
          )}
          {integrations.error ? (
            <p role="alert" className="text-xs text-destructive">
              Could not load integrations.
            </p>
          ) : null}
        </div>

        {save.error ? (
          <p role="alert" className="text-xs text-destructive">
            {(save.error as Error).message}
          </p>
        ) : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="button"
            onClick={() => save.mutate()}
            disabled={!name.trim() || save.isPending}
          >
            {save.isPending
              ? "Saving…"
              : initial
                ? "Save changes"
                : "Create assistant"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
