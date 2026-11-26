import {
  ResourceSharingButton,
  SharingPicker,
  sharingLabel,
  adminOnlySharing,
  type ResourceSharing,
} from "./sharing-controls";
import { useId, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, ClipboardPaste, FileUp, Loader2, Plus } from "lucide-react";

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
import { cn } from "@/lib/utils";
import { http, unwrap } from "@/api/typed";
import { workspaceApi, relativeTime, type KnowledgeSource } from "./types";

function formatBytes(n: number): string {
  if (n >= 1024 * 1024 * 1024)
    return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
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
      return data?.sources.some(
        (item) => item.status === "pending" || item.status === "processing",
      )
        ? 2000
        : false;
    },
  });
  const indexing = useQuery({
    queryKey: ["workspace", "knowledge-index-status"],
    queryFn: () => unwrap(http.GET("/v1/workspace/knowledge/index-status")),
    refetchInterval: 10000,
  });
  const [creating, setCreating] = useState(false);
  const [archiveTarget, setArchiveTarget] = useState<KnowledgeSource | null>(
    null,
  );
  const [releaseTarget, setReleaseTarget] = useState<KnowledgeSource | null>(
    null,
  );
  const [dragging, setDragging] = useState(false);
  const [pendingFiles, setPendingFiles] = useState<File[]>([]);
  const [uploadSharing, setUploadSharing] = useState<ResourceSharing>(
    adminOnlySharing(),
  );
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
    mutationFn: async () => {
      const files = [...pendingFiles];
      for (const file of files) {
        await workspaceApi.uploadKnowledge(file, undefined, uploadSharing);
        setPendingFiles((current) => current.filter((item) => item !== file));
        qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
      }
    },
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
    setUploadSharing(adminOnlySharing());
    setPendingFiles(Array.from(files));
    upload.reset();
  };

  const sources = list.data?.sources ?? [];
  const readyCount = sources.filter((item) => item.status === "ready").length;
  const processingCount = sources.filter(
    (item) => item.status === "pending" || item.status === "processing",
  ).length;
  const failedCount = sources.filter((item) => item.status === "failed").length;
  const quarantinedCount = sources.filter(
    (item) => item.status === "quarantined",
  ).length;
  const issueCount = quarantinedCount + failedCount;

  return (
    <div>
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

      <AdminSummaryStrip
        items={[
          {
            label: "Sources",
            value: list.isLoading ? "—" : sources.length,
            detail: "Active inventory",
          },
          {
            label: "Ready",
            value: list.isLoading ? "—" : readyCount,
            detail: "Available for retrieval",
            tone: readyCount > 0 ? "success" : "default",
          },
          {
            label: "Processing",
            value: list.isLoading ? "—" : processingCount,
            detail: "Extraction in progress",
            tone: processingCount > 0 ? "warning" : "default",
          },
          {
            label: "Needs review",
            value: list.isLoading ? "—" : issueCount,
            detail: `${quarantinedCount} quarantined · ${failedCount} failed`,
            tone: issueCount > 0 ? "danger" : "default",
          },
        ]}
      />

      <AdminPanel
        title="Knowledge inventory"
        description={
          <>
            Upload governed sources and explicitly assign them to assistants.
            {indexing.data ? (
              <span className="mt-1 block font-mono text-[11px] text-muted-foreground">
                {indexing.data.enabled
                  ? `${indexing.data.model.startsWith("bge:") ? "Self-hosted semantic search" : "Semantic search enabled"} · ${indexing.data.pending_sources ? `${indexing.data.pending_sources} sources indexing in the background` : "Index up to date"}`
                  : "Keyword search · embedding service is not configured"}
              </span>
            ) : null}
          </>
        }
        action={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              onClick={() => fileInputRef.current?.click()}
            >
              <FileUp className="size-3.5" aria-hidden="true" />
              Upload files
            </Button>
            <Button
              type="button"
              size="sm"
              className="h-8 text-xs"
              onClick={() => setCreating(true)}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              Paste text
            </Button>
          </div>
        }
        contentClassName="py-0"
      >
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
            "flex flex-wrap items-center justify-between gap-4 border-b border-border-subtle px-3 py-3 transition-colors",
            dragging ? "bg-surface-1" : "bg-surface-1/30",
          )}
        >
          <div className="flex min-w-0 items-center gap-2.5">
            {upload.isPending ? (
              <Loader2
                className="size-4 shrink-0 animate-spin text-muted-foreground"
                aria-hidden="true"
              />
            ) : (
              <FileUp
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            )}
            <div className="min-w-0">
              <p className="text-xs font-medium text-foreground">
                {upload.isPending
                  ? "Uploading and extracting…"
                  : "Drop knowledge files here"}
              </p>
              <p className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">
                PDF, DOCX, TXT, Markdown, CSV, JSON, LOG, and YAML. Review
                source sensitivity before assigning it to an assistant.
              </p>
            </div>
          </div>
          <Button
            type="button"
            className="hidden sm:inline-flex"
            size="xs"
            variant="outline"
            onClick={() => fileInputRef.current?.click()}
          >
            Browse
          </Button>
        </div>

        {uploadError ? (
          <div role="alert" className="border-b border-border-subtle py-3">
            <div className="border-l-2 border-destructive pl-3.5 text-xs text-destructive">
              <span className="font-medium text-foreground">
                Upload failed.
              </span>{" "}
              {uploadError}
            </div>
          </div>
        ) : null}

        {list.isLoading ? (
          <div className="border-b border-border-subtle py-4">
            <SkeletonRows count={4} />
          </div>
        ) : sources.length === 0 ? (
          <div className="border-b border-border-subtle py-10 text-center">
            <h3 className="text-[13px] font-medium text-foreground">
              No knowledge sources
            </h3>
            <p className="mx-auto mt-1 max-w-md text-xs leading-relaxed text-muted-foreground">
              Upload a document or paste authoritative text. A source becomes
              retrievable only after processing completes and you assign it to
              an assistant.
            </p>
            <div className="mt-4 inline-flex items-center gap-2">
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-8 text-xs"
                onClick={() => fileInputRef.current?.click()}
              >
                <FileUp className="size-3.5" aria-hidden="true" />
                Upload
              </Button>
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
                onClick={() => setCreating(true)}
              >
                <ClipboardPaste className="size-3.5" aria-hidden="true" />
                Paste text
              </Button>
            </div>
          </div>
        ) : (
          <div
            tabIndex={0}
            role="region"
            aria-label="Knowledge inventory"
            className="overflow-x-auto border-b border-border-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <table className="w-full min-w-[680px] border-collapse text-left">
              <thead>
                <tr className="border-b border-border-subtle text-xs text-muted-foreground">
                  <th scope="col" className="py-2.5 pr-4 font-medium">
                    Source
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Status
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Size
                  </th>
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Last synced
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
                {sources.map((source) => (
                  <tr
                    key={source.id}
                    className="transition-colors hover:bg-surface-1/50"
                  >
                    <td className="max-w-[340px] py-3.5 pr-4 align-middle">
                      <p className="truncate text-[13px] font-medium text-foreground">
                        {source.name}
                      </p>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">
                        {source.mime_type || source.type} ·{" "}
                        {sharingLabel(source.sharing)}
                      </p>
                      {source.error ? (
                        <p className="mt-1.5 border-l-2 border-destructive pl-2.5 text-[11px] text-destructive">
                          {source.error}
                        </p>
                      ) : null}
                      {source.status === "quarantined" ? (
                        <ScanSummary source={source} />
                      ) : null}
                    </td>
                    <td className="px-4 py-3.5 align-middle">
                      <StatusIndicator status={source.status} />
                    </td>
                    <td className="px-4 py-3.5 align-middle font-mono text-[11px] tabular-nums text-muted-foreground">
                      <span>
                        {source.size_bytes > 0
                          ? formatBytes(source.size_bytes)
                          : "Inline"}
                      </span>
                      {source.character_count ? (
                        <span className="mt-0.5 block">
                          {formatChars(source.character_count)} chars
                        </span>
                      ) : null}
                    </td>
                    <td className="px-4 py-3.5 align-middle font-mono text-[11px] tabular-nums text-muted-foreground">
                      {source.last_synced_at
                        ? relativeTime(source.last_synced_at)
                        : "Not synced"}
                    </td>
                    <td className="py-3.5 pl-4 text-right align-middle">
                      <div className="inline-flex items-center justify-end gap-1">
                        {source.status === "quarantined" ? (
                          <Button
                            type="button"
                            size="xs"
                            variant="outline"
                            onClick={() => setReleaseTarget(source)}
                          >
                            Review
                          </Button>
                        ) : null}
                        <ResourceSharingButton
                          type="knowledge"
                          id={source.id}
                          name={source.name}
                          sharing={source.sharing}
                        />
                        <Button
                          type="button"
                          size="icon-xs"
                          variant="ghost"
                          aria-label={`Archive ${source.name}`}
                          onClick={() => setArchiveTarget(source)}
                        >
                          <Archive aria-hidden="true" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </AdminPanel>

      <div className="mt-6 space-y-5">
        <div className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
          <span className="font-medium text-foreground">
            Retrieval remains permission-bound.
          </span>{" "}
          Uploading a source does not expose it globally. Only assistants with
          an explicit source assignment can retrieve its content.
        </div>

        <details className="border-y border-border-subtle py-3 text-xs leading-relaxed">
          <summary className="cursor-pointer text-[13px] font-medium text-foreground">
            What happens to uploaded knowledge?
          </summary>
          <ol className="mt-3 list-decimal space-y-2 pl-4 text-muted-foreground">
            <li>
              The original file is stored privately on the Bastio server.
              Extracted text is checked against your workspace’s security
              profile before indexing.
            </li>
            <li>
              Block quarantines matching files. Mask or Tokenize processes the
              detected values before the searchable text is stored. Warn and Log
              only allow unchanged text. Original files are retained separately,
              including any personal data they contain.
            </li>
            <li>
              Cloud uses Bastio’s self-hosted GPU embedding service over a
              private connection. Other deployments use the embedding service
              configured by their operator. Processed text is sent to that
              service to create search vectors. Relevant passages are sent to
              the selected model when an employee asks a question.
            </li>
          </ol>
          <p className="mt-3 border-t border-border-subtle pt-3 text-muted-foreground">
            The conversation “do not store” setting does not apply to knowledge
            sources. Changing a security profile does not automatically rewrite
            previously indexed sources.
          </p>
        </details>
      </div>

      <Dialog
        open={pendingFiles.length > 0}
        onOpenChange={(open) =>
          !open && !upload.isPending && setPendingFiles([])
        }
      >
        <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Upload knowledge files</DialogTitle>
            <DialogDescription>
              Choose recipients before these files are stored or indexed.
            </DialogDescription>
          </DialogHeader>
          <ul className="max-h-28 divide-y divide-border-subtle overflow-y-auto border-y border-border-subtle font-mono text-xs text-muted-foreground">
            {pendingFiles.map((file, index) => (
              <li
                key={`${file.name}:${index}`}
                className="flex items-center justify-between gap-3 py-1.5"
              >
                <span className="truncate text-foreground">{file.name}</span>
                <span className="shrink-0 tabular-nums">
                  {formatBytes(file.size)}
                </span>
              </li>
            ))}
          </ul>
          <SharingPicker
            value={uploadSharing}
            onChange={setUploadSharing}
            disabled={upload.isPending}
          />
          {upload.error ? (
            <p role="alert" className="text-xs text-destructive">
              {upload.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={upload.isPending}
              onClick={() => setPendingFiles([])}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={upload.isPending}
              onClick={() => upload.mutate()}
            >
              {upload.isPending
                ? "Uploading…"
                : `Upload ${pendingFiles.length === 1 ? "file" : "files"}`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <KnowledgeEditor
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={() =>
          qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] })
        }
      />

      <Dialog
        open={Boolean(archiveTarget)}
        onOpenChange={(open) => !open && setArchiveTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive knowledge source?</DialogTitle>
            <DialogDescription>
              {archiveTarget?.name} will stop being available to assistants.
              Existing conversation history and citations remain intact.
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
              {archive.isPending ? "Archiving…" : "Archive source"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(releaseTarget)}
        onOpenChange={(open) => !open && setReleaseTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Review quarantined source</DialogTitle>
            <DialogDescription>
              {releaseTarget?.name} was blocked during ingest and is not
              available to any assistant.
            </DialogDescription>
          </DialogHeader>
          {releaseTarget ? <QuarantineEvidence source={releaseTarget} /> : null}
          <div className="border-l-2 border-warn pl-3.5 text-xs leading-relaxed text-muted-foreground">
            <span className="font-medium text-foreground">
              Release is an audited security override.
            </span>{" "}
            Only release content you have reviewed and trust. The source will be
            scanned again and may be quarantined again if the detector still
            blocks it.
          </div>
          {release.error ? (
            <p role="alert" className="text-xs text-destructive">
              {(release.error as Error).message}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setReleaseTarget(null)}
            >
              Keep quarantined
            </Button>
            <Button
              type="button"
              disabled={release.isPending}
              onClick={() => releaseTarget && release.mutate(releaseTarget.id)}
            >
              {release.isPending ? "Releasing…" : "Release and rescan"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function StatusIndicator({ status }: { status: string }) {
  const processing = status === "pending" || status === "processing";
  return (
    <span className="inline-flex items-center gap-2 text-xs">
      {processing ? (
        <Loader2
          className="size-3 shrink-0 animate-spin text-warn"
          aria-hidden="true"
        />
      ) : (
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            status === "ready"
              ? "bg-success"
              : status === "quarantined"
                ? "bg-warn"
                : status === "failed"
                  ? "bg-destructive"
                  : "bg-muted-foreground/50",
          )}
        />
      )}
      <span
        className={cn(
          "capitalize",
          status === "ready"
            ? "text-foreground"
            : status === "quarantined"
              ? "text-warn"
              : status === "failed"
                ? "text-destructive"
                : "text-muted-foreground",
        )}
      >
        {status}
      </span>
    </span>
  );
}

function scanResult(source: KnowledgeSource) {
  return (source.scan_result ?? {}) as Record<string, unknown>;
}

function scanCategories(source: KnowledgeSource): string[] {
  const categories = scanResult(source).categories;
  return Array.isArray(categories)
    ? categories.filter((item): item is string => typeof item === "string")
    : [];
}

function ScanSummary({ source }: { source: KnowledgeSource }) {
  const categories = scanCategories(source);
  return (
    <p className="mt-1.5 truncate border-l-2 border-warn pl-2.5 font-mono text-[11px] text-warn">
      {categories.length
        ? categories.join(", ")
        : "Blocked by ingest security policy"}
    </p>
  );
}

function QuarantineEvidence({ source }: { source: KnowledgeSource }) {
  const result = scanResult(source);
  const categories = scanCategories(source);
  return (
    <div className="divide-y divide-border-subtle border-y border-border-subtle text-xs">
      <div className="grid grid-cols-[120px_1fr] gap-3 py-2.5">
        <span className="text-muted-foreground">Detector action</span>
        <span className="font-mono text-foreground">
          {String(result.action ?? "block")}
        </span>
      </div>
      <div className="grid grid-cols-[120px_1fr] gap-3 py-2.5">
        <span className="text-muted-foreground">Categories</span>
        <span className="font-mono text-foreground">
          {categories.join(", ") || "Not reported"}
        </span>
      </div>
      <div className="grid grid-cols-[120px_1fr] gap-3 py-2.5">
        <span className="text-muted-foreground">Threat score</span>
        <span className="font-mono text-foreground">
          {result.threat_score == null
            ? "Not reported"
            : String(result.threat_score)}
        </span>
      </div>
      <div className="grid grid-cols-[120px_1fr] gap-3 py-2.5">
        <span className="text-muted-foreground">Content hash</span>
        <span className="truncate font-mono text-foreground">
          {source.content_hash ?? "Inline content"}
        </span>
      </div>
    </div>
  );
}

function KnowledgeEditor({
  open,
  onClose,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const uid = useId();
  const nameId = `${uid}-name`;
  const contentId = `${uid}-content`;
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [sharing, setSharing] = useState<ResourceSharing>(adminOnlySharing());
  const save = useMutation({
    mutationFn: () =>
      workspaceApi.createKnowledge({
        name,
        type: "text",
        inline_text: content,
        sharing,
      }),
    onSuccess: () => {
      onSaved();
      onClose();
      setName("");
      setContent("");
      setSharing(adminOnlySharing());
    },
  });

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Paste knowledge text</DialogTitle>
          <DialogDescription>
            Create an inline source for policies, procedures, or other reviewed
            reference content.
          </DialogDescription>
        </DialogHeader>
        <div>
          <FieldLabel htmlFor={nameId}>Name</FieldLabel>
          <Input
            id={nameId}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="e.g. Incident response policy"
            autoFocus
          />
        </div>
        <div>
          <FieldLabel htmlFor={contentId}>Content</FieldLabel>
          <textarea
            id={contentId}
            value={content}
            onChange={(event) => setContent(event.target.value)}
            rows={10}
            placeholder="Paste reviewed source content…"
            className="w-full resize-y rounded-md border border-input bg-transparent px-2.5 py-2 text-xs leading-relaxed outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        </div>
        <SharingPicker
          value={sharing}
          onChange={setSharing}
          disabled={save.isPending}
        />
        <div className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
          <span className="font-medium text-foreground">
            Treat pasted text as a governed source.
          </span>{" "}
          Avoid credentials and secrets. The content is stored and may be
          retrieved into an assigned assistant’s prompt context.
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
            disabled={!name.trim() || !content.trim() || save.isPending}
            onClick={() => save.mutate()}
          >
            {save.isPending ? "Saving…" : "Create source"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
