import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi, type Assistant, type KnowledgeSource } from "./types";

export function AssistantsTab() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["workspace", "assistants"],
    queryFn: workspaceApi.listAssistants,
  });
  const [editing, setEditing] = useState<Assistant | null>(null);
  const [creating, setCreating] = useState(false);

  const archive = useMutation({
    mutationFn: workspaceApi.archiveAssistant,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "assistants"] }),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Assistants</h3>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="mr-2 h-4 w-4" /> New assistant
        </Button>
      </div>

      {list.isLoading && <SkeletonRows count={3} />}
      {list.data?.assistants.length === 0 && !creating && (
        <Card>
          <CardContent className="p-6 text-sm text-muted-foreground">
            No assistants yet. Create one to give your team a tailored chat experience.
          </CardContent>
        </Card>
      )}

      <div className="grid gap-3">
        {list.data?.assistants.map((a) => (
          <Card key={a.id}>
            <CardContent className="flex items-start justify-between gap-4 p-4">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <h4 className="text-sm font-medium">{a.name}</h4>
                  {a.is_default && <Badge variant="secondary">Default</Badge>}
                  <Badge variant="outline" className="font-mono text-xs">
                    {a.default_provider}/{a.default_model}
                  </Badge>
                </div>
                {a.description && (
                  <p className="mt-1 text-sm text-muted-foreground">{a.description}</p>
                )}
                <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
                  {a.system_prompt || <em>(no system prompt)</em>}
                </p>
              </div>
              <div className="flex items-center gap-1">
                <Button size="sm" variant="ghost" onClick={() => setEditing(a)}>
                  Edit
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    if (confirm(`Archive "${a.name}"?`)) archive.mutate(a.id);
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {(creating || editing) && (
        <AssistantEditor
          initial={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
            qc.invalidateQueries({ queryKey: ["workspace", "assistants"] });
          }}
        />
      )}
    </div>
  );
}

function AssistantEditor({
  initial,
  onClose,
}: {
  initial: Assistant | null;
  onClose: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [systemPrompt, setSystemPrompt] = useState(initial?.system_prompt ?? "");
  const [provider, setProvider] =
    useState<Assistant["default_provider"]>(initial?.default_provider ?? "openai");
  const [model, setModel] = useState(initial?.default_model ?? "gpt-4o-mini");
  const [isDefault, setIsDefault] = useState(initial?.is_default ?? false);
  // language: null/undefined = auto-detect from user input. Existing
  // assistants from migration 015 default to "en"; the new
  // "Auto-detect" option (value="") writes null on save so the chat
  // mirrors the user's language.
  const [language, setLanguage] = useState<string>(initial?.language ?? "");
  // Knowledge sources attached to this assistant. The chat surface
  // only runs RAG against an assistant's attached KB — without
  // anything wired here, the workspace's KB tab is decorative.
  // Backend treats an unset (omitted) field as "leave alone" and an
  // empty array as "detach all"; we always send the array so the
  // dashboard's edit semantics match the visual state.
  const [selectedKB, setSelectedKB] = useState<string[]>(
    initial?.knowledge_source_ids ?? [],
  );
  const knowledge = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: () => workspaceApi.listKnowledge(),
    staleTime: 60_000,
  });
  // Backend already filters archived sources at the list endpoint,
  // so anything here is selectable.
  const availableKB: KnowledgeSource[] = knowledge.data?.sources ?? [];

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        name,
        description,
        system_prompt: systemPrompt,
        default_provider: provider,
        default_model: model,
        is_default: isDefault,
        // empty string → null = auto-detect; otherwise an ISO code
        // forces the response language.
        language: language.trim() || null,
        knowledge_source_ids: selectedKB,
      };
      if (initial) {
        return workspaceApi.updateAssistant(initial.id, body);
      }
      return workspaceApi.createAssistant(body);
    },
    onSuccess: onClose,
  });

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h4 className="text-sm font-semibold">
          {initial ? "Edit assistant" : "New assistant"}
        </h4>
        <Field label="Name">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
          />
        </Field>
        <Field label="Description (optional)">
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
          />
        </Field>
        <Field label="System prompt">
          <textarea
            value={systemPrompt}
            onChange={(e) => setSystemPrompt(e.target.value)}
            rows={5}
            className="w-full resize-y rounded-md border border-border bg-background px-2 py-1 text-sm font-mono"
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Provider">
            <select
              value={provider}
              onChange={(e) =>
                setProvider(e.target.value as Assistant["default_provider"])
              }
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            >
              <option value="openai">openai</option>
              <option value="anthropic">anthropic</option>
              <option value="bedrock">bedrock</option>
              <option value="ollama">ollama</option>
              <option value="google">google</option>
            </select>
          </Field>
          <Field label="Model">
            <input
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm font-mono"
            />
          </Field>
        </div>
        <Field label="Response language">
          <select
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
          >
            <option value="">Auto-detect from user message</option>
            <option value="en">English (always)</option>
            <option value="es">Spanish (always)</option>
            <option value="fr">French (always)</option>
            <option value="de">German (always)</option>
            <option value="it">Italian (always)</option>
            <option value="pt">Portuguese (always)</option>
            <option value="nl">Dutch (always)</option>
            <option value="da">Danish (always)</option>
            <option value="sv">Swedish (always)</option>
            <option value="ja">Japanese (always)</option>
            <option value="zh">Chinese (always)</option>
          </select>
        </Field>
        <Field label="Knowledge base sources">
          {availableKB.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              No knowledge sources yet. Upload PDFs or add inline text in the
              Knowledge Base tab, then attach them here so this assistant can
              cite them in answers.
            </p>
          ) : (
            <div className="max-h-44 space-y-1 overflow-y-auto rounded-md border border-border bg-background p-2">
              {availableKB.map((k) => {
                const checked = selectedKB.includes(k.id);
                return (
                  <label
                    key={k.id}
                    className="flex cursor-pointer items-start gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-muted"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => {
                        setSelectedKB((prev) =>
                          e.target.checked
                            ? [...prev, k.id]
                            : prev.filter((id) => id !== k.id),
                        );
                      }}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{k.name}</div>
                      <div className="text-[10px] text-muted-foreground">
                        {k.type}
                        {k.status && k.status !== "ready" ? ` · ${k.status}` : ""}
                      </div>
                    </div>
                  </label>
                );
              })}
            </div>
          )}
          <span className="text-[11px] text-muted-foreground">
            Selected sources are searched on every chat turn — relevant
            excerpts are added to the system prompt and the model is asked to
            cite them.
          </span>
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={isDefault}
            onChange={(e) => setIsDefault(e.target.checked)}
          />
          Default assistant for new chats
        </label>
        {save.error && (
          <p className="text-sm text-destructive">{(save.error as Error).message}</p>
        )}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={() => save.mutate()} disabled={!name.trim() || save.isPending}>
            {save.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </CardContent>
    </Card>
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
