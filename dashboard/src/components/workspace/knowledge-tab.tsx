import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, BookOpen, ClipboardPaste, FileText, FileUp, Loader2, Plus, ShieldAlert } from "lucide-react";

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
import { cn } from "@/lib/utils";
import { workspaceApi, relativeTime, type KnowledgeSource } from "./types";

function formatBytes(n: number): string {
  if (n >= 1024 * 1024 * 1024) return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

function formatChars(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

export function KnowledgeTab() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
    refetchInterval: (query) => {
      const data = query.state.data;
      return data?.sources.some((item) => item.status === "pending" || item.status === "processing") ? 2000 : false;
    },
  });
  const [creating, setCreating] = useState(false);
  const [archiveTarget, setArchiveTarget] = useState<KnowledgeSource | null>(null);
  const [releaseTarget, setReleaseTarget] = useState<KnowledgeSource | null>(null);
  const [dragging, setDragging] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const archive = useMutation({
    mutationFn: workspaceApi.archiveKnowledge,
    onSuccess: () => {
      setArchiveTarget(null);
      qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
    },
  });
  const upload = useMutation({
    mutationFn: (file: File) => workspaceApi.uploadKnowledge(file),
    onSuccess: () => {
      setUploadError(null);
      qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
    },
    onError: (error) => setUploadError((error as Error).message),
  });
  const release = useMutation({
    mutationFn: workspaceApi.releaseKnowledge,
    onSuccess: () => {
      setReleaseTarget(null);
      qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
    },
  });
  const onFiles = (files: FileList | null) => {
    if (!files?.length) return;
    setUploadError(null);
    Array.from(files).forEach((file) => upload.mutate(file));
  };

  const sources = list.data?.sources ?? [];
  const readyCount = sources.filter((item) => item.status === "ready").length;
  const processingCount = sources.filter((item) => item.status === "pending" || item.status === "processing").length;
  const failedCount = sources.filter((item) => item.status === "failed").length;
  const quarantinedCount = sources.filter((item) => item.status === "quarantined").length;

  return (
    <div>
      <SectionHeader
        title="Knowledge inventory"
        description="Upload governed sources and explicitly assign them to assistants."
        action={
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => fileInputRef.current?.click()}>
              <FileUp data-icon="inline-start" /> Upload files
            </Button>
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus data-icon="inline-start" /> Paste text
            </Button>
          </div>
        }
      />

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        accept=".txt,.md,.markdown,.csv,.json,.log,.yaml,.yml,.pdf,.docx,text/*,application/json,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        onChange={(event) => {
          onFiles(event.target.files);
          event.currentTarget.value = "";
        }}
      />

      <div className="mb-5 grid overflow-hidden rounded-xl border border-border/70 bg-card sm:grid-cols-5 sm:divide-x sm:divide-border/60">
        <Metric label="Sources" value={list.isLoading ? "—" : sources.length} detail="Active inventory" />
        <Metric label="Ready" value={readyCount} detail="Available for retrieval" tone="success" />
        <Metric label="Processing" value={processingCount} detail="Extraction in progress" />
        <Metric label="Failed" value={failedCount} detail="Needs operator review" tone={failedCount ? "danger" : "default"} />
        <Metric label="Quarantined" value={quarantinedCount} detail="Security review required" tone={quarantinedCount ? "danger" : "default"} />
      </div>

      <div
        onDragOver={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          onFiles(event.dataTransfer.files);
        }}
        className={cn(
          "mb-5 flex min-h-24 items-center justify-between gap-4 rounded-xl border border-dashed px-5 py-4 transition-colors",
          dragging ? "border-ring bg-muted/50" : "border-border/80 bg-muted/15",
        )}
      >
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-card text-muted-foreground">
            {upload.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <FileUp className="h-4 w-4" />}
          </div>
          <div className="min-w-0">
            <p className="text-[12px] font-medium text-foreground">
              {upload.isPending ? "Uploading and extracting…" : "Drop knowledge files here"}
            </p>
            <p className="mt-0.5 text-[10px] leading-relaxed text-muted-foreground">
              PDF, DOCX, TXT, Markdown, CSV, JSON, LOG, and YAML. Review source sensitivity before assigning it to an assistant.
            </p>
          </div>
        </div>
        <Button className="hidden sm:inline-flex" size="sm" variant="outline" onClick={() => fileInputRef.current?.click()}>
          Browse
        </Button>
      </div>

      {uploadError ? (
        <SecurityNotice className="mb-5" title="Upload failed" tone="warning">{uploadError}</SecurityNotice>
      ) : null}

      <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
        {list.isLoading ? (
          <div className="p-4"><SkeletonRows count={4} /></div>
        ) : sources.length === 0 ? (
          <EmptyState
            icon={<BookOpen className="h-5 w-5" />}
            title="No knowledge sources"
            description="Upload a document or paste authoritative text. A source becomes retrievable only after processing completes and you assign it to an assistant."
            action={
              <div className="flex items-center gap-2">
                <Button size="sm" variant="outline" onClick={() => fileInputRef.current?.click()}><FileUp data-icon="inline-start" /> Upload</Button>
                <Button size="sm" onClick={() => setCreating(true)}><ClipboardPaste data-icon="inline-start" /> Paste text</Button>
              </div>
            }
          />
        ) : (
          <>
            <div className="hidden grid-cols-[minmax(240px,1.5fr)_120px_130px_150px_120px] gap-4 border-b border-border/60 px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground md:grid">
              <span>Source</span><span>Status</span><span>Size</span><span>Last synced</span><span />
            </div>
            <div className="divide-y divide-border/50">
              {sources.map((source) => (
                <div key={source.id} className="grid gap-3 px-4 py-3.5 transition-colors hover:bg-muted/20 md:grid-cols-[minmax(240px,1.5fr)_120px_130px_150px_120px] md:items-center md:gap-4">
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
                      <FileText className="h-3.5 w-3.5" />
                    </div>
                    <div className="min-w-0">
                      <p className="truncate text-[12px] font-medium text-foreground">{source.name}</p>
                      <p className="truncate text-[10px] text-muted-foreground">{source.mime_type || source.type}</p>
                      {source.error ? <p className="mt-1 text-[10px] text-destructive">{source.error}</p> : null}
                      {source.status === "quarantined" ? <ScanSummary source={source} /> : null}
                    </div>
                  </div>
                  <StatusBadge status={source.status} />
                  <div className="font-mono text-[10px] text-muted-foreground">
                    {source.size_bytes > 0 ? formatBytes(source.size_bytes) : "Inline"}
                    {source.character_count ? <span className="block">{formatChars(source.character_count)} chars</span> : null}
                  </div>
                  <span className="text-[10px] text-muted-foreground">{source.last_synced_at ? relativeTime(source.last_synced_at) : "Not synced"}</span>
                  <div className="flex gap-1 md:justify-end">
                    {source.status === "quarantined" ? (
                      <Button size="xs" variant="outline" onClick={() => setReleaseTarget(source)}>Review</Button>
                    ) : null}
                    <Button size="icon-xs" variant="ghost" aria-label={`Archive ${source.name}`} onClick={() => setArchiveTarget(source)}><Archive /></Button>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </section>

      <SecurityNotice className="mt-5" title="Retrieval remains permission-bound" tone="success">
        Uploading a source does not expose it globally. Only assistants with an explicit source assignment can retrieve its content.
      </SecurityNotice>

      <KnowledgeEditor
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={() => qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] })}
      />

      <Dialog open={Boolean(archiveTarget)} onOpenChange={(open) => !open && setArchiveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive knowledge source?</DialogTitle>
            <DialogDescription>
              {archiveTarget?.name} will stop being available to assistants. Existing conversation history and citations remain intact.
            </DialogDescription>
          </DialogHeader>
          {archive.error ? <p className="text-[11px] text-destructive">{(archive.error as Error).message}</p> : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setArchiveTarget(null)}>Cancel</Button>
            <Button variant="destructive" disabled={archive.isPending} onClick={() => archiveTarget && archive.mutate(archiveTarget.id)}>
              {archive.isPending ? "Archiving…" : "Archive source"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(releaseTarget)} onOpenChange={(open) => !open && setReleaseTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Review quarantined source</DialogTitle>
            <DialogDescription>
              {releaseTarget?.name} was blocked during ingest and is not available to any assistant.
            </DialogDescription>
          </DialogHeader>
          {releaseTarget ? <QuarantineEvidence source={releaseTarget} /> : null}
          <SecurityNotice title="Release is an audited security override" tone="warning">
            Only release content you have reviewed and trust. The source will be scanned again and may be quarantined again if the detector still blocks it.
          </SecurityNotice>
          {release.error ? <p className="text-[11px] text-destructive">{(release.error as Error).message}</p> : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setReleaseTarget(null)}>Keep quarantined</Button>
            <Button disabled={release.isPending} onClick={() => releaseTarget && release.mutate(releaseTarget.id)}>
              {release.isPending ? "Releasing…" : "Release and rescan"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Metric({ label, value, detail, tone = "default" }: { label: string; value: string | number; detail: string; tone?: "default" | "success" | "danger" }) {
  return (
    <div className="px-4 py-3.5">
      <p className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
      <p className={cn("mt-1 font-mono text-[16px] font-medium text-foreground", tone === "success" && "text-success", tone === "danger" && "text-destructive")}>{value}</p>
      <p className="mt-0.5 truncate text-[10px] text-muted-foreground">{detail}</p>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const processing = status === "pending" || status === "processing";
  return (
    <Badge variant={status === "ready" ? "success" : status === "failed" || status === "quarantined" ? "destructive" : "outline"} className="capitalize">
      {processing ? <Loader2 className="animate-spin" /> : null}{status}
    </Badge>
  );
}

function scanResult(source: KnowledgeSource) {
  return (source.scan_result ?? {}) as Record<string, unknown>;
}

function scanCategories(source: KnowledgeSource): string[] {
  const categories = scanResult(source).categories;
  return Array.isArray(categories) ? categories.filter((item): item is string => typeof item === "string") : [];
}

function ScanSummary({ source }: { source: KnowledgeSource }) {
  const categories = scanCategories(source);
  return (
    <p className="mt-1 flex items-center gap-1 truncate text-[10px] text-destructive">
      <ShieldAlert className="h-3 w-3 shrink-0" /> {categories.length ? categories.join(", ") : "Blocked by ingest security policy"}
    </p>
  );
}

function QuarantineEvidence({ source }: { source: KnowledgeSource }) {
  const result = scanResult(source);
  const categories = scanCategories(source);
  return (
    <div className="overflow-hidden rounded-xl border border-border/70 bg-muted/20 text-[11px]">
      <div className="grid grid-cols-[120px_1fr] gap-3 border-b border-border/50 px-3 py-2.5"><span className="text-muted-foreground">Detector action</span><span className="font-mono text-foreground">{String(result.action ?? "block")}</span></div>
      <div className="grid grid-cols-[120px_1fr] gap-3 border-b border-border/50 px-3 py-2.5"><span className="text-muted-foreground">Categories</span><span className="font-mono text-foreground">{categories.join(", ") || "Not reported"}</span></div>
      <div className="grid grid-cols-[120px_1fr] gap-3 border-b border-border/50 px-3 py-2.5"><span className="text-muted-foreground">Threat score</span><span className="font-mono text-foreground">{result.threat_score == null ? "Not reported" : String(result.threat_score)}</span></div>
      <div className="grid grid-cols-[120px_1fr] gap-3 px-3 py-2.5"><span className="text-muted-foreground">Content hash</span><span className="truncate font-mono text-foreground">{source.content_hash ?? "Inline content"}</span></div>
    </div>
  );
}

function KnowledgeEditor({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const save = useMutation({
    mutationFn: () => workspaceApi.createKnowledge({ name, type: "text", inline_text: content }),
    onSuccess: () => {
      onSaved();
      onClose();
      setName("");
      setContent("");
    },
  });

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Paste knowledge text</DialogTitle>
          <DialogDescription>
            Create an inline source for policies, procedures, or other reviewed reference content.
          </DialogDescription>
        </DialogHeader>
        <div>
          <FieldLabel>Name</FieldLabel>
          <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="e.g. Incident response policy" autoFocus />
        </div>
        <div>
          <FieldLabel>Content</FieldLabel>
          <textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            rows={10}
            placeholder="Paste reviewed source content…"
            className="w-full resize-y rounded-lg border border-input bg-transparent px-2.5 py-2 text-[12px] leading-relaxed outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        </div>
        <SecurityNotice title="Treat pasted text as a governed source" tone="info">
          Avoid credentials and secrets. The content is stored and may be retrieved into an assigned assistant’s prompt context.
        </SecurityNotice>
        {save.error ? <p className="text-[11px] text-destructive">{(save.error as Error).message}</p> : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button disabled={!name.trim() || !content.trim() || save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? "Saving…" : "Create source"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
