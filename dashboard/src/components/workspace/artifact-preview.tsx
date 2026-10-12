import { useState } from "react";
import { Code2, Play, ShieldCheck, Square } from "lucide-react";
import { buildArtifactDocument, extractHtmlArtifact } from "./artifact-document";

export function ArtifactPreview({ content }: { content: string }) {
  const [open, setOpen] = useState(false);
  const artifact = extractHtmlArtifact(content);
  if (!artifact) return null;

  return (
    <div className="mt-3 overflow-hidden rounded-md border border-border bg-background/70">
      <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Code2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <div>
            <div className="text-xs font-semibold tracking-tight">HTML artifact</div>
            <div className="text-[11px] text-muted-foreground">Isolated preview · external resources blocked</div>
          </div>
        </div>
        <button
          type="button"
          onClick={() => setOpen((value) => !value)}
          aria-expanded={open}
          className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface-2 px-2.5 py-1.5 text-xs font-medium transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {open ? <Square className="h-3 w-3" aria-hidden="true" /> : <Play className="h-3 w-3" aria-hidden="true" />}
          {open ? "Stop preview" : "Preview"}
        </button>
      </div>
      {open && (
        <div className="border-t border-border">
          <iframe
            title="Generated HTML artifact preview"
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            srcDoc={buildArtifactDocument(artifact)}
            className="block h-[360px] w-full bg-white"
          />
          <div className="flex items-center gap-1.5 border-t border-border px-3 py-2 text-[11px] text-muted-foreground">
            <ShieldCheck className="h-3 w-3 shrink-0" aria-hidden="true" />
            This code runs in a separate, restricted frame. Review the source above before previewing.
          </div>
        </div>
      )}
    </div>
  );
}
