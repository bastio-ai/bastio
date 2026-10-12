import { useEffect, useRef, useState } from "react";
import {
  Code2, Expand, LoaderCircle, Monitor, Play, RefreshCw,
  ShieldCheck, Smartphone, Square, X,
} from "lucide-react";

import { buildArtifactDocument, buildReactArtifactDocument, extractHtmlArtifact } from "./artifact-document";
import { extractReactArtifact } from "./react-artifact";

export function ArtifactPreview({ content }: { content: string }) {
  const reactArtifact = extractReactArtifact(content);
  const htmlArtifact = reactArtifact ? null : extractHtmlArtifact(content);
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [mobile, setMobile] = useState(false);
  const [revision, setRevision] = useState(0);
  const [document, setDocument] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setDocument(null);
    setError(null);
    const react = extractReactArtifact(content);
    if (!react) {
      const html = extractHtmlArtifact(content);
      if (html) setDocument(buildArtifactDocument(html));
      return;
    }
    void Promise.all([
      import("./react-artifact-compile"),
      import("virtual:bastio-artifact-runtime"),
    ]).then(async ([compiler, runtime]) => {
      const compiled = await compiler.compileReactArtifact(react);
      if (!cancelled) {
        setDocument(buildReactArtifactDocument(
          compiled, runtime.reactRuntime, runtime.utilityRuntime,
        ));
      }
    }).catch((cause: unknown) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : String(cause));
    });
    return () => { cancelled = true; };
  }, [content, open]);

  useEffect(() => {
    if (!expanded) return;
    const previousFocus = window.document.activeElement as HTMLElement | null;
    panelRef.current?.querySelector<HTMLElement>("button")?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setExpanded(false);
      if (event.key !== "Tab") return;
      const controls = Array.from(panelRef.current?.querySelectorAll<HTMLElement>("button:not([disabled])") ?? []);
      if (!controls.length) return;
      const first = controls[0];
      const last = controls.at(-1);
      if (event.shiftKey && window.document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && window.document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      previousFocus?.focus();
    };
  }, [expanded]);

  if (!reactArtifact && !htmlArtifact) return null;
  const label = reactArtifact ? "React artifact" : "HTML artifact";

  return (
    <>
      {expanded && <div className="fixed inset-0 z-[99] bg-black/70" onClick={() => setExpanded(false)} aria-hidden="true" />}
      <div
        ref={panelRef}
        role={expanded ? "dialog" : undefined}
        aria-modal={expanded ? true : undefined}
        aria-label={expanded ? `${label} preview` : undefined}
        className={`${expanded ? "fixed inset-3 z-[100] flex flex-col shadow-sm sm:inset-8" : "mt-3"} overflow-hidden rounded-md border border-border bg-background`}
      >
        <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
          <div className="flex min-w-0 items-center gap-2">
            <Code2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            <div>
              <div className="text-xs font-semibold tracking-tight">{label}</div>
              <div className="text-[11px] text-muted-foreground">{reactArtifact ? "React · TSX · local dependencies" : "HTML · CSS · JavaScript"}</div>
            </div>
          </div>
          <div className="flex items-center gap-1.5">
            {open && (
              <>
                <button type="button" onClick={() => setMobile((value) => !value)} title={mobile ? "Desktop width" : "Mobile width"} aria-label={mobile ? "Desktop width" : "Mobile width"} className="rounded-md border border-border p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                  {mobile ? <Monitor className="h-3.5 w-3.5" /> : <Smartphone className="h-3.5 w-3.5" />}
                </button>
                <button type="button" onClick={() => setRevision((value) => value + 1)} title="Reload preview" aria-label="Reload preview" className="rounded-md border border-border p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                  <RefreshCw className="h-3.5 w-3.5" />
                </button>
                <button type="button" onClick={() => setExpanded((value) => !value)} title={expanded ? "Close expanded preview" : "Expand preview"} aria-label={expanded ? "Close expanded preview" : "Expand preview"} className="rounded-md border border-border p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                  {expanded ? <X className="h-3.5 w-3.5" /> : <Expand className="h-3.5 w-3.5" />}
                </button>
              </>
            )}
            <button
              type="button"
              onClick={() => { setOpen((value) => !value); setExpanded(false); }}
              aria-expanded={open}
              className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface-2 px-2.5 py-1.5 text-xs font-medium transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {open ? <Square className="h-3 w-3" aria-hidden="true" /> : <Play className="h-3 w-3" aria-hidden="true" />}
              {open ? "Stop" : "Preview"}
            </button>
          </div>
        </div>
        {open && (
          <div className={`flex min-h-0 flex-col border-t border-border ${expanded ? "flex-1" : ""}`}>
            {error ? (
              <div role="alert" className="m-3 rounded-md border border-destructive/30 bg-destructive/10 p-3 font-mono text-xs whitespace-pre-wrap text-destructive">
                {error}
              </div>
            ) : document ? (
              <div className={`flex min-h-0 justify-center overflow-hidden bg-surface-2 p-2 ${expanded ? "flex-1" : ""}`}>
                <iframe
                  key={revision}
                  title={`Generated ${label} preview`}
                  sandbox="allow-scripts"
                  referrerPolicy="no-referrer"
                  srcDoc={document}
                  className={`${expanded ? "h-full" : "h-[420px]"} ${mobile ? "w-[390px] max-w-full" : "w-full"} border border-border bg-white`}
                />
              </div>
            ) : (
              <div role="status" className="flex h-40 items-center justify-center gap-2 text-xs text-muted-foreground">
                <LoaderCircle className="h-4 w-4 animate-spin" aria-hidden="true" /> Building preview…
              </div>
            )}
            <div className="flex items-center gap-1.5 border-t border-border px-3 py-2 text-[11px] text-muted-foreground">
              <ShieldCheck className="h-3 w-3 shrink-0" aria-hidden="true" />
              Runs in a restricted frame after the response security check. Source is visible above.
            </div>
          </div>
        )}
      </div>
    </>
  );
}
