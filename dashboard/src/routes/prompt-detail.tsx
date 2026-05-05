import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowLeft, Check, Copy, FileText, GitBranch, Plus } from "lucide-react";

import { api } from "@/api/client";
import type { PromptUsage, PromptVersion } from "@/api/client";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { JsonViewer } from "@/components/observe/json-viewer";
import { KpiCard } from "@/components/observe/kpi-card";
import { formatCost, formatDuration } from "@/lib/utils";

export function PromptDetailPage() {
  const { name } = useParams({ from: "/prompts/$name" });
  const navigate = useNavigate();
  const qc = useQueryClient();

  const { data: versions = [], isLoading } = useQuery({
    queryKey: ["prompt-versions", name],
    queryFn: () => api.prompts.versions(name),
  });
  const { data: usage } = useQuery<PromptUsage>({
    queryKey: ["prompt-usage", name],
    queryFn: () => api.prompts.usage(name),
  });

  const [tab, setTab] = useState("versions");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = useMemo(() => {
    if (!versions.length) return null;
    if (selectedId) return versions.find((v: PromptVersion) => v.id === selectedId) ?? versions[0];
    return versions[0];
  }, [versions, selectedId]);
  const latest = versions[0];

  const [adding, setAdding] = useState(false);

  if (isLoading) {
    return <LoadingBlock />;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <BackLink />
          <FileText className="h-4 w-4 text-muted-foreground" />
          <span className="font-mono text-sm font-semibold">{name}</span>
          {latest ? (
            <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
              latest v{latest.version}
            </Badge>
          ) : null}
          {(latest?.labels ?? []).map((l) => (
            <Badge
              key={l}
              variant="outline"
              className="text-[10px] px-1.5 py-0 font-mono text-muted-foreground"
            >
              {l}
            </Badge>
          ))}
        </div>
        <Button size="sm" className="h-8 text-xs" onClick={() => setAdding(true)}>
          <Plus className="h-3 w-3" /> New version
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiCard label="Versions" value={String(versions.length)} />
        <KpiCard
          label="Traces"
          value={String(
            (usage?.summary ?? []).reduce((acc, s) => acc + (s.trace_count ?? 0), 0),
          )}
        />
        <KpiCard
          label="Cost"
          value={formatCost(
            (usage?.summary ?? []).reduce((acc, s) => acc + (s.total_cost_cents ?? 0), 0),
          )}
        />
        <KpiCard
          label="Last used"
          value={(() => {
            const first = (usage?.summary ?? [])[0];
            return first?.last_used_at
              ? new Date(first.last_used_at).toLocaleTimeString()
              : "—";
          })()}
        />
      </div>

      {adding ? (
        <NewVersionForm
          name={name}
          seed={latest}
          onClose={() => setAdding(false)}
          onCreated={() => {
            qc.invalidateQueries({ queryKey: ["prompt-versions", name] });
            qc.invalidateQueries({ queryKey: ["prompts"] });
            setAdding(false);
          }}
        />
      ) : null}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList variant="line">
          <TabsTrigger value="versions">Versions ({versions.length})</TabsTrigger>
          <TabsTrigger value="usage">Usage ({usage?.summary?.length ?? 0})</TabsTrigger>
        </TabsList>

        <TabsContent value="versions" className="pt-3">
          {versions.length ? (
            <div className="grid grid-cols-[16rem_1fr] gap-3">
              <Card className="border-border/50 overflow-hidden">
                <CardContent className="p-0">
                  <div className="divide-y divide-border/30">
                    {versions.map((v: PromptVersion) => (
                      <button
                        key={v.id}
                        type="button"
                        onClick={() => setSelectedId(v.id)}
                        className={`flex w-full flex-col gap-1 px-3 py-2 text-left text-xs hover:bg-muted/30 ${
                          selected?.id === v.id ? "bg-muted/40" : ""
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          <GitBranch className="h-3 w-3 text-muted-foreground" />
                          <span className="font-mono">v{v.version}</span>
                          {(v.labels ?? []).map((l) => (
                            <Badge
                              key={l}
                              variant="outline"
                              className="text-[9px] px-1 py-0 font-mono text-muted-foreground"
                            >
                              {l}
                            </Badge>
                          ))}
                        </div>
                        <span className="text-[10px] text-muted-foreground">
                          {new Date(v.created_at).toLocaleString()}
                        </span>
                        {v.commit_message ? (
                          <span className="truncate text-[10px] text-muted-foreground/80">
                            {v.commit_message}
                          </span>
                        ) : null}
                      </button>
                    ))}
                  </div>
                </CardContent>
              </Card>
              {selected ? (
                <VersionDetail
                  name={name}
                  version={selected}
                  onLabelsSaved={() => {
                    qc.invalidateQueries({ queryKey: ["prompt-versions", name] });
                  }}
                />
              ) : null}
            </div>
          ) : (
            <p className="py-8 text-center text-xs text-muted-foreground">
              No versions yet.
            </p>
          )}
        </TabsContent>

        <TabsContent value="usage" className="pt-3 space-y-4">
          <UsageSummary summary={usage?.summary ?? []} />
          <UsageTraces
            recent={usage?.recent ?? []}
            onOpen={(traceId) => navigate({ to: "/traces/$id", params: { id: traceId } })}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function VersionDetail({
  name,
  version,
  onLabelsSaved,
}: {
  name: string;
  version: PromptVersion;
  onLabelsSaved: () => void;
}) {
  const [labels, setLabels] = useState((version.labels ?? []).join(", "));
  const save = useMutation({
    mutationFn: () =>
      api.prompts.setLabels(name, version.version, {
        labels: labels
          .split(/[,\s]+/)
          .map((s) => s.trim())
          .filter(Boolean),
      }),
    onSuccess: onLabelsSaved,
  });
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(version.content);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* noop */
    }
  };

  return (
    <Card className="border-border/50">
      <CardContent className="p-3 space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <span className="font-mono text-sm font-semibold">v{version.version}</span>
          <span className="text-[11px] text-muted-foreground">
            {new Date(version.created_at).toLocaleString()}
          </span>
          {version.created_by ? (
            <span className="text-[11px] text-muted-foreground">by {version.created_by}</span>
          ) : null}
          <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-muted-foreground">
            {version.content_type}
          </Badge>
          <Button variant="ghost" size="icon" className="ml-auto h-6 w-6" onClick={copy}>
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </Button>
        </div>

        {version.commit_message ? (
          <p className="rounded border border-border/40 bg-muted/20 px-2 py-1.5 font-mono text-[11px]">
            {version.commit_message}
          </p>
        ) : null}

        <section className="space-y-1">
          <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Content
          </p>
          {version.content_type === "chat" ? (
            <JsonViewer rawString={version.content} />
          ) : (
            <pre className="max-h-96 overflow-auto rounded border border-border/50 bg-muted/20 p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
              {version.content}
            </pre>
          )}
        </section>

        {version.config && Object.keys(version.config as object).length ? (
          <section className="space-y-1">
            <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
              Config
            </p>
            <JsonViewer value={version.config} maxHeight="18rem" />
          </section>
        ) : null}

        <section className="space-y-1">
          <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            Labels
          </p>
          <div className="flex items-center gap-2">
            <Input
              value={labels}
              onChange={(e) => setLabels(e.target.value)}
              placeholder="comma-separated, e.g. production, stable"
              className="h-8 text-xs"
            />
            <Button
              size="sm"
              className="h-8 text-xs"
              onClick={() => save.mutate()}
              disabled={save.isPending}
            >
              Save
            </Button>
          </div>
          <p className="text-[10px] text-muted-foreground">
            Exclusive by default: setting a label here removes it from every other version.
          </p>
        </section>
      </CardContent>
    </Card>
  );
}

function NewVersionForm({
  name,
  seed,
  onClose,
  onCreated,
}: {
  name: string;
  seed?: PromptVersion;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [content, setContent] = useState(seed?.content ?? "");
  const [contentType, setContentType] = useState<"text" | "chat">(
    (seed?.content_type as "text" | "chat") ?? "text",
  );
  const [labels, setLabels] = useState((seed?.labels ?? []).join(", "));
  const [commit, setCommit] = useState("");
  const create = useMutation({
    mutationFn: () =>
      api.prompts.createVersion(name, {
        content,
        content_type: contentType,
        labels: labels
          .split(/[,\s]+/)
          .map((s) => s.trim())
          .filter(Boolean),
        commit_message: commit,
      }),
    onSuccess: onCreated,
  });

  return (
    <Card className="border-border/50">
      <CardContent className="p-3 space-y-2">
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
            placeholder="labels (e.g. production)"
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            className="h-8 text-xs"
          />
          <Input
            placeholder="commit message"
            value={commit}
            onChange={(e) => setCommit(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
        <textarea
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
            disabled={!content || create.isPending}
          >
            <Plus className="h-3 w-3" /> Append version
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function UsageSummary({ summary }: { summary: PromptUsage["summary"] }) {
  if (!summary.length) {
    return (
      <Card className="border-border/50">
        <CardContent className="py-10 text-center text-xs text-muted-foreground">
          No traces have referenced this prompt yet.
        </CardContent>
      </Card>
    );
  }
  const max = Math.max(...summary.map((s) => s.trace_count ?? 0));
  return (
    <Card className="border-border/50">
      <CardContent className="p-3 space-y-2">
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          Per-version usage
        </p>
        {summary.map((s) => (
          <div
            key={s.version}
            className="grid grid-cols-[5rem_1fr_6rem_6rem_10rem] items-center gap-2 text-xs"
          >
            <span className="font-mono">v{s.version}</span>
            <div className="h-2 rounded bg-muted/50">
              <div
                className="h-2 rounded bg-primary/70"
                style={{ width: `${((s.trace_count ?? 0) / max) * 100}%` }}
              />
            </div>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {s.trace_count}
            </span>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {formatDuration(Math.round(s.avg_duration_ms ?? 0))}
            </span>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {formatCost(s.total_cost_cents ?? 0)}
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function UsageTraces({
  recent,
  onOpen,
}: {
  recent: PromptUsage["recent"];
  onOpen: (traceID: string) => void;
}) {
  if (!recent.length) {
    return null;
  }
  return (
    <Card className="border-border/50 overflow-hidden">
      <CardContent className="p-0">
        <div className="border-b border-border/40 bg-muted/10 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          Recent traces
        </div>
        <div className="divide-y divide-border/30">
          {recent.map((t) => (
            <button
              key={`${t.trace_id}-${t.version}`}
              type="button"
              onClick={() => onOpen(t.trace_id)}
              className="grid w-full grid-cols-[5rem_1fr_6rem_5rem_10rem] items-center gap-2 px-3 py-2 text-left text-xs hover:bg-muted/30"
            >
              <span className="font-mono">v{t.version}</span>
              <span className="truncate font-mono">{t.span_name || t.trace_id.slice(0, 8)}</span>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {formatDuration(t.duration_ms ?? 0)}
              </span>
              <Badge
                variant={t.status === "ok" ? "success" : "destructive"}
                className="justify-self-start text-[10px] px-1.5 py-0"
              >
                {t.status}
              </Badge>
              <span className="text-right text-[11px] text-muted-foreground">
                {new Date(t.started_at).toLocaleString()}
              </span>
            </button>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function BackLink() {
  return (
    <Link
      to="/prompts"
      className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="h-3.5 w-3.5" /> Back to prompts
    </Link>
  );
}

function LoadingBlock() {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
        Loading prompt…
      </div>
    </div>
  );
}
