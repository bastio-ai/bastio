import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FileText, Plus, X } from "lucide-react";

import { api } from "@/api/client";
import type { Prompt } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";

export function PromptsPage() {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

  const { data: prompts = [], isLoading } = useQuery({
    queryKey: ["prompts"],
    queryFn: () => api.prompts.list(),
  });

  return (
    <>
      <div className="flex items-end justify-between">
        <PageHeader
          title="Prompts"
          description="Named, immutable-version prompt templates your app fetches via the SDK."
        />
        <Button size="sm" className="h-8 text-xs" onClick={() => setCreating(true)}>
          <Plus className="h-3 w-3" /> New prompt
        </Button>
      </div>

      {creating ? (
        <CreatePromptForm onClose={() => setCreating(false)} />
      ) : null}

      <Card className="border-border/50 overflow-hidden mt-4">
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={5} />
          ) : !prompts.length ? (
            <EmptyState
              icon={<FileText className="h-6 w-6" />}
              title="No prompts yet"
              description="Create your first prompt — the gateway will link every trace to the exact version that ran."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Name
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Description
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Latest
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Labels
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Updated
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {prompts.map((p: Prompt) => (
                  <TableRow
                    key={p.id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30"
                    onClick={() =>
                      navigate({ to: "/prompts/$name", params: { name: p.name } })
                    }
                  >
                    <TableCell className="font-mono text-xs">{p.name}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {p.description || "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-xs text-muted-foreground">
                      v{p.latest_version}
                    </TableCell>
                    <TableCell>
                      {(p.labels ?? []).length
                        ? (p.labels ?? []).map((l) => (
                            <Badge
                              key={l}
                              variant="outline"
                              className="mr-1 text-[10px] px-1.5 py-0 text-muted-foreground"
                            >
                              {l}
                            </Badge>
                          ))
                        : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">
                      {new Date(p.updated_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}

function CreatePromptForm({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [contentType, setContentType] = useState<"text" | "chat">("text");
  const [content, setContent] = useState("");
  const [labels, setLabels] = useState("");
  const [commit, setCommit] = useState("");
  const create = useMutation({
    mutationFn: () =>
      api.prompts.create({
        name,
        description,
        content,
        content_type: contentType,
        labels: labels
          .split(/[,\s]+/)
          .map((s) => s.trim())
          .filter(Boolean),
        commit_message: commit,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["prompts"] });
      onClose();
    },
  });

  return (
    <Card className="border-border/50 mt-4">
      <CardContent className="p-3 space-y-2">
        <div className="flex items-center justify-between">
          <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            New prompt
          </p>
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}>
            <X className="h-3 w-3" />
          </Button>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <Input
            placeholder="name (e.g. customer_support_system)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="h-8 text-xs"
          />
          <Input
            placeholder="description (optional)"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
        <div className="grid grid-cols-[8rem_1fr_1fr] gap-2">
          <Select value={contentType} onValueChange={(v) => setContentType(v as "text" | "chat")}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="text">text</SelectItem>
              <SelectItem value="chat">chat (JSON)</SelectItem>
            </SelectContent>
          </Select>
          <Input
            placeholder="labels (e.g. production, v1)"
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            className="h-8 text-xs"
          />
          <Input
            placeholder="commit message (optional)"
            value={commit}
            onChange={(e) => setCommit(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
        <textarea
          placeholder={
            contentType === "chat"
              ? `[\n  { "role": "system", "content": "You are a helpful assistant." },\n  { "role": "user", "content": "{{question}}" }\n]`
              : "You are a helpful assistant answering {{question}}."
          }
          value={content}
          onChange={(e) => setContent(e.target.value)}
          rows={8}
          className="w-full rounded border border-border/50 bg-muted/20 p-2 font-mono text-[11px] leading-relaxed focus:outline-none"
        />
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            className="h-8 text-xs"
            onClick={() => create.mutate()}
            disabled={!name || !content || create.isPending}
          >
            <Plus className="h-3 w-3" /> Create
          </Button>
        </div>
        {create.isError ? (
          <p className="text-xs text-destructive">
            {(create.error as Error | undefined)?.message ?? "Failed to create prompt"}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}
