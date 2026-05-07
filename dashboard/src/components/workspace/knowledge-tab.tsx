import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Plus, Trash2, Upload, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi, relativeTime } from "./types";

// formatBytes humanizes a byte count to KB / MB / GB so the KB list
// stays readable. Mirrors the gateway's formatBytes (kept local to
// avoid a workspace→gateway dep).
function formatBytes(n: number): string {
  if (n >= 1024 * 1024 * 1024) return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

// formatChars compresses a character count for the row metadata line.
function formatChars(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M chars`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k chars`;
  return `${n} chars`;
}

export function KnowledgeTab() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["workspace", "knowledge"],
    queryFn: workspaceApi.listKnowledge,
    // Poll while any source is still processing — auto-stops when all are
    // ready/failed so we don't burn cycles on a stable list.
    refetchInterval: (q) => {
      const data = q.state.data;
      if (!data) return false;
      const inflight = data.sources.some(
        (s) => s.status === "pending" || s.status === "processing",
      );
      return inflight ? 2000 : false;
    },
  });
  const [creating, setCreating] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const archive = useMutation({
    mutationFn: workspaceApi.archiveKnowledge,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] }),
  });

  const upload = useMutation({
    mutationFn: (file: File) => workspaceApi.uploadKnowledge(file),
    onSuccess: () => {
      setUploadError(null);
      qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
    },
    onError: (err) => setUploadError((err as Error).message),
  });

  const onFiles = (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploadError(null);
    // Sequence uploads so the dashboard polls a coherent list. Three at a
    // time would be fine too, but plenty of dropped batches are a single
    // file and the UI animation looks calmer this way.
    Array.from(files).forEach((f) => upload.mutate(f));
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Knowledge sources</h3>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => fileInputRef.current?.click()}
          >
            <Upload className="mr-2 h-4 w-4" /> Upload file
          </Button>
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="mr-2 h-4 w-4" /> Paste text
          </Button>
        </div>
      </div>
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        accept=".txt,.md,.markdown,.csv,.json,.log,.yaml,.yml,.pdf,.docx,text/*,application/json,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        onChange={(e) => {
          onFiles(e.target.files);
          e.currentTarget.value = "";
        }}
      />

      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          onFiles(e.dataTransfer.files);
        }}
        className={`rounded-lg border-2 border-dashed p-6 text-center text-sm transition ${
          dragging
            ? "border-cyan-500 bg-cyan-500/5 text-foreground"
            : "border-border text-muted-foreground"
        }`}
      >
        {upload.isPending ? (
          <span className="inline-flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" /> Uploading…
          </span>
        ) : (
          <>
            Drop files here, or use the buttons above. Supported: <code>.txt</code>,{" "}
            <code>.md</code>, <code>.csv</code>, <code>.json</code>, <code>.log</code>,{" "}
            <code>.yaml</code>, <code>.pdf</code>, <code>.docx</code>.
          </>
        )}
      </div>

      {uploadError && (
        <p className="text-sm text-destructive">{uploadError}</p>
      )}

      {list.isLoading && <SkeletonRows count={3} />}
      {list.data?.sources.length === 0 && !creating && !upload.isPending && (
        <Card>
          <CardContent className="p-6 text-sm text-muted-foreground">
            No knowledge sources yet. Drop a file above, or paste text.
          </CardContent>
        </Card>
      )}

      <div className="grid gap-3">
        {list.data?.sources.map((k) => (
          <Card key={k.id}>
            <CardContent className="flex items-center justify-between gap-4 p-4">
              <div className="flex min-w-0 flex-1 items-center gap-3">
                <FileText className="h-4 w-4 text-muted-foreground" />
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <h4 className="truncate text-sm font-medium">{k.name}</h4>
                    <Badge variant="outline" className="text-xs">{k.type}</Badge>
                    <Badge
                      variant={
                        k.status === "ready"
                          ? "secondary"
                          : k.status === "failed"
                            ? "destructive"
                            : "outline"
                      }
                      className="text-xs"
                    >
                      {(k.status === "pending" || k.status === "processing") && (
                        <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                      )}
                      {k.status}
                    </Badge>
                  </div>
                  {/* Metadata line — bytes, character count, mime,
                      and freshness. Only the parts that have a real
                      value render so we never show "0 B" for inline
                      text snippets. */}
                  <p className="text-xs text-muted-foreground">
                    {[
                      k.mime_type,
                      k.size_bytes > 0 && formatBytes(k.size_bytes),
                      k.character_count && k.character_count > 0 && formatChars(k.character_count),
                      k.last_synced_at && `synced ${relativeTime(k.last_synced_at)}`,
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                  {k.error && (
                    <p className="mt-1 text-xs text-destructive">{k.error}</p>
                  )}
                </div>
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  if (confirm(`Archive "${k.name}"?`)) archive.mutate(k.id);
                }}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>

      {creating && (
        <KnowledgeEditor
          onClose={() => {
            setCreating(false);
            qc.invalidateQueries({ queryKey: ["workspace", "knowledge"] });
          }}
        />
      )}
    </div>
  );
}

function KnowledgeEditor({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState("");
  const [text, setText] = useState("");

  const save = useMutation({
    mutationFn: () =>
      workspaceApi.createKnowledge({
        name,
        type: "text",
        inline_text: text,
      }),
    onSuccess: onClose,
  });

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h4 className="text-sm font-semibold">New knowledge source</h4>
        <p className="text-xs text-muted-foreground">
          Inline text only for MVP — file + URL ingestion ships next.
        </p>
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted-foreground">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted-foreground">Content</span>
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={8}
            className="w-full resize-y rounded-md border border-border bg-background px-2 py-1 text-sm"
          />
        </label>
        {save.error && (
          <p className="text-sm text-destructive">{(save.error as Error).message}</p>
        )}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            onClick={() => save.mutate()}
            disabled={!name.trim() || !text.trim() || save.isPending}
          >
            {save.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
