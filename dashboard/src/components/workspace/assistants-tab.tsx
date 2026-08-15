import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Bot, BookOpen, CheckCircle2, Plus, ShieldCheck } from "lucide-react";

import { EmptyState, SectionHeader } from "@/components/card";
import { FieldLabel, SecurityNotice } from "@/components/admin/admin-primitives";
import { Badge } from "@/components/ui/badge";
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
import { workspaceApi, type Assistant, type KnowledgeSource } from "./types";
import { DEFAULT_MODEL, DEFAULT_MODELS, providerLabel, type ModelProvider } from "./model-picker";

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

  return (
    <div>
      <SectionHeader
        title="Assistant inventory"
        description="Create purpose-built assistants and limit each one to the knowledge it needs."
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus data-icon="inline-start" /> New assistant
          </Button>
        }
      />

      <div className="mb-5 grid overflow-hidden rounded-xl border border-border/70 bg-card sm:grid-cols-3 sm:divide-x sm:divide-border/60">
        <Metric label="Configured" value={list.isLoading ? "—" : assistants.length} detail="Available in the portal" />
        <Metric label="Default" value={defaultAssistant?.name ?? "Not set"} detail="Used for new conversations" />
        <Metric
          label="Knowledge access"
          value={assistants.reduce((total, item) => total + (item.knowledge_source_ids?.length ?? 0), 0)}
          detail="Source assignments across assistants"
        />
      </div>

      <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
        {list.isLoading ? (
          <div className="p-4"><SkeletonRows count={4} /></div>
        ) : assistants.length === 0 ? (
          <EmptyState
            icon={<Bot className="h-5 w-5" />}
            title="Create your first governed assistant"
            description="Set a role, model default, response language, and approved knowledge sources. Employees will only see assistants you configure here."
            action={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus data-icon="inline-start" /> New assistant
              </Button>
            }
          />
        ) : (
          <>
            <div className="hidden grid-cols-[minmax(220px,1.4fr)_minmax(180px,0.8fr)_140px_100px] gap-4 border-b border-border/60 px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground md:grid">
              <span>Assistant</span><span>Model</span><span>Knowledge</span><span className="text-right">Actions</span>
            </div>
            <div className="divide-y divide-border/50">
              {assistants.map((assistant) => (
                <div
                  key={assistant.id}
                  className="grid gap-3 px-4 py-3.5 transition-colors hover:bg-muted/20 md:grid-cols-[minmax(220px,1.4fr)_minmax(180px,0.8fr)_140px_100px] md:items-center md:gap-4"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[12px] font-medium text-foreground">{assistant.name}</span>
                      {assistant.is_default ? <Badge variant="success">Default</Badge> : null}
                    </div>
                    <p className="mt-0.5 line-clamp-1 text-[10px] text-muted-foreground">
                      {assistant.description || "No description provided"}
                    </p>
                  </div>
                  <div className="min-w-0">
                    <p className="truncate font-mono text-[11px] text-foreground">{assistant.default_model}</p>
                    <p className="text-[10px] capitalize text-muted-foreground">{assistant.default_provider}</p>
                  </div>
                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                    <BookOpen className="h-3.5 w-3.5" />
                    {assistant.knowledge_source_ids?.length ?? 0} sources
                  </div>
                  <div className="flex justify-start gap-1 md:justify-end">
                    <Button size="xs" variant="ghost" onClick={() => setEditing(assistant)}>Edit</Button>
                    <Button size="icon-xs" variant="ghost" aria-label={`Archive ${assistant.name}`} onClick={() => setArchiveTarget(assistant)}>
                      <Archive />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </section>

      <SecurityNotice className="mt-5" title="Least-privilege knowledge access" tone="info">
        Knowledge is assigned per assistant. Sources that are not selected are never retrieved into that assistant’s prompt context.
      </SecurityNotice>

      <AssistantEditor
        key={editing?.id ?? (creating ? "new" : "closed")}
        open={creating || Boolean(editing)}
        initial={editing}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={() => qc.invalidateQueries({ queryKey: ["workspace", "assistants"] })}
      />

      <Dialog open={Boolean(archiveTarget)} onOpenChange={(open) => !open && setArchiveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive assistant?</DialogTitle>
            <DialogDescription>
              {archiveTarget?.name} will no longer be available for new portal conversations. Existing conversation history is preserved.
            </DialogDescription>
          </DialogHeader>
          {archive.error ? <p className="text-[11px] text-destructive">{(archive.error as Error).message}</p> : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setArchiveTarget(null)}>Cancel</Button>
            <Button variant="destructive" disabled={archive.isPending} onClick={() => archiveTarget && archive.mutate(archiveTarget.id)}>
              {archive.isPending ? "Archiving…" : "Archive assistant"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Metric({ label, value, detail }: { label: string; value: string | number; detail: string }) {
  return (
    <div className="px-4 py-3.5">
      <p className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
      <p className="mt-1 truncate text-[14px] font-medium text-foreground">{value}</p>
      <p className="mt-0.5 truncate text-[10px] text-muted-foreground">{detail}</p>
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
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [systemPrompt, setSystemPrompt] = useState(initial?.system_prompt ?? "");
  const [provider, setProvider] = useState<ModelProvider>(initial?.default_provider ?? DEFAULT_MODEL.provider);
  const [model, setModel] = useState(initial?.default_model ?? DEFAULT_MODEL.model);
  const [isDefault, setIsDefault] = useState(initial?.is_default ?? false);
  const [language, setLanguage] = useState(initial?.language ?? "");
  const [suggestedPrompts, setSuggestedPrompts] = useState((initial?.suggested_prompts ?? []).join("\n"));
  const [selectedKB, setSelectedKB] = useState<string[]>(initial?.knowledge_source_ids ?? []);
  const knowledge = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
    staleTime: 60_000,
  });
  const availableKB: KnowledgeSource[] = knowledge.data?.sources ?? [];
  const providerModels = DEFAULT_MODELS.filter((item) => item.provider === provider);
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
        suggested_prompts: suggestedPrompts.split("\n").map((item) => item.trim()).filter(Boolean),
        knowledge_source_ids: selectedKB,
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

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{initial ? "Edit assistant" : "New assistant"}</DialogTitle>
          <DialogDescription>
            Configure the assistant’s purpose, model defaults, and explicit knowledge permissions.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 py-1 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <FieldLabel>Name</FieldLabel>
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="e.g. Security policy advisor" autoFocus />
          </div>
          <div className="sm:col-span-2">
            <FieldLabel optional>Description</FieldLabel>
            <Input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="What employees should use this assistant for" />
          </div>
          <div className="sm:col-span-2">
            <FieldLabel>System prompt</FieldLabel>
            <textarea
              value={systemPrompt}
              onChange={(event) => setSystemPrompt(event.target.value)}
              rows={6}
              placeholder="Define the role, boundaries, and escalation behavior…"
              className="w-full resize-y rounded-lg border border-input bg-transparent px-2.5 py-2 font-mono text-[12px] outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>
          <div>
            <FieldLabel>Provider</FieldLabel>
            <select
              value={provider}
              onChange={(event) => {
                const next = event.target.value as ModelProvider;
                setProvider(next);
                const suggested = DEFAULT_MODELS.find((item) => item.provider === next);
                if (suggested) setModel(suggested.model);
              }}
              className="h-8 w-full rounded-lg border border-input bg-background px-2.5 text-[12px] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              {(["openai", "anthropic", "gemini", "deepseek", "groq", "bedrock", "ollama"] as ModelProvider[]).map((item) => (
                <option key={item} value={item}>{providerLabel(item)}</option>
              ))}
            </select>
          </div>
          <div>
            <FieldLabel>Default model</FieldLabel>
            {providerModels.length > 0 ? (
              <select
                value={model}
                onChange={(event) => setModel(event.target.value)}
                className="h-8 w-full rounded-lg border border-input bg-background px-2.5 font-mono text-[12px] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                {!currentModelKnown ? <option value={model}>{model}</option> : null}
                {providerModels.map((item) => (
                  <option key={item.model} value={item.model}>{item.label}</option>
                ))}
              </select>
            ) : (
              <Input value={model} onChange={(event) => setModel(event.target.value)} className="font-mono" />
            )}
          </div>
          <div className="sm:col-span-2">
            <FieldLabel>Response language</FieldLabel>
            <select
              value={language}
              onChange={(event) => setLanguage(event.target.value)}
              className="h-8 w-full rounded-lg border border-input bg-background px-2.5 text-[12px] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <option value="">Auto-detect from each user message</option>
              <option value="en">English</option><option value="es">Spanish</option><option value="fr">French</option>
              <option value="de">German</option><option value="it">Italian</option><option value="pt">Portuguese</option>
              <option value="nl">Dutch</option><option value="da">Danish</option><option value="sv">Swedish</option>
              <option value="ja">Japanese</option><option value="zh">Chinese</option>
            </select>
          </div>

          <div className="sm:col-span-2">
            <FieldLabel optional>Conversation starters</FieldLabel>
            <textarea
              value={suggestedPrompts}
              onChange={(event) => setSuggestedPrompts(event.target.value)}
              rows={3}
              placeholder={"Summarize our incident response policy\nWho owns a security escalation?"}
              className="w-full resize-y rounded-lg border border-input bg-transparent px-2.5 py-2 text-[12px] leading-relaxed outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
            <p className="mt-1 text-[10px] text-muted-foreground">One prompt per line. These appear as optional shortcuts when employees start a conversation.</p>
          </div>

          <div className="sm:col-span-2">
            <FieldLabel optional>Knowledge permissions</FieldLabel>
            {availableKB.length === 0 ? (
              <div className="rounded-lg border border-dashed border-border/70 px-4 py-5 text-center">
                <BookOpen className="mx-auto h-4 w-4 text-muted-foreground" />
                <p className="mt-2 text-[11px] text-muted-foreground">No knowledge sources are available yet.</p>
              </div>
            ) : (
              <div className="max-h-44 divide-y divide-border/50 overflow-y-auto rounded-lg border border-border/70">
                {availableKB.map((item) => {
                  const checked = selectedKB.includes(item.id);
                  return (
                    <label key={item.id} className="flex cursor-pointer items-center gap-3 px-3 py-2.5 hover:bg-muted/30">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(event) => setSelectedKB((current) => event.target.checked ? [...current, item.id] : current.filter((id) => id !== item.id))}
                      />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-[11px] font-medium text-foreground">{item.name}</p>
                        <p className="text-[10px] text-muted-foreground">{item.type} · {item.status}</p>
                      </div>
                      {item.status === "ready" ? <CheckCircle2 className="h-3.5 w-3.5 text-success" /> : null}
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        <SecurityNotice title="System prompts may contain sensitive operating instructions" tone="warning">
          Avoid secrets and credentials. Selected knowledge excerpts are added only when relevant, but the assistant prompt is applied to every message.
        </SecurityNotice>

        <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border border-border/70 px-3 py-2.5 text-[11px] text-foreground">
          <input type="checkbox" checked={isDefault} onChange={(event) => setIsDefault(event.target.checked)} />
          <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground" />
          Use as the default assistant for new conversations
        </label>

        {save.error ? <p className="text-[11px] text-destructive">{(save.error as Error).message}</p> : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={() => save.mutate()} disabled={!name.trim() || save.isPending}>
            {save.isPending ? "Saving…" : initial ? "Save changes" : "Create assistant"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
