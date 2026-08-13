import { useMemo, useState } from "react";
import { Check, Copy, WrapText } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

type Props = {
  value?: unknown;
  // Pretty-print when value is already a string (attempts to JSON.parse).
  rawString?: string;
  className?: string;
  maxHeight?: string;
};

// Lightweight JSON viewer. Renders a pretty-printed, syntax-highlighted
// block with a copy button. Non-JSON strings fall through to plain text so
// this also works for truncated streams, markdown, etc.
export function JsonViewer({ value, rawString, className, maxHeight = "70vh" }: Props) {
  const { pretty, isJson } = useMemo(() => {
    if (rawString !== undefined) {
      try {
        return { pretty: JSON.stringify(JSON.parse(rawString), null, 2), isJson: true };
      } catch {
        return { pretty: rawString, isJson: false };
      }
    }
    try {
      return { pretty: JSON.stringify(value, null, 2), isJson: true };
    } catch {
      return { pretty: String(value ?? ""), isJson: false };
    }
  }, [value, rawString]);

  const [copied, setCopied] = useState(false);
  const [wrapped, setWrapped] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(pretty);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* noop */
    }
  };

  if (!pretty) {
    return (
      <p className="py-6 text-center text-xs text-muted-foreground">Empty.</p>
    );
  }

  return (
    <div className={cn("relative rounded border border-border/50 bg-muted/20", className)}>
      <div className="absolute right-1.5 top-1.5 z-10 flex items-center gap-0.5 rounded-md border border-border-subtle bg-background/90 p-0.5 backdrop-blur">
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setWrapped((current) => !current)}
          className={cn("h-6 w-6 text-muted-foreground hover:text-foreground", wrapped && "bg-surface-2 text-foreground")}
          aria-label={wrapped ? "Disable line wrapping" : "Enable line wrapping"}
          aria-pressed={wrapped}
        >
          <WrapText className="h-3 w-3" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={copy}
          className="h-6 w-6 text-muted-foreground hover:text-foreground"
          aria-label="Copy"
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        </Button>
      </div>
      <pre
        className={cn(
          "overflow-auto p-3 pr-16 font-mono text-[11px] leading-relaxed",
          wrapped ? "whitespace-pre-wrap break-words" : "whitespace-pre",
          isJson ? "text-foreground" : "text-foreground/80",
        )}
        style={{ maxHeight }}
      >
        {pretty}
      </pre>
    </div>
  );
}
