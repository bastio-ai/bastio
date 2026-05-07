import { useMemo, useState } from "react";
import { Check, Copy } from "lucide-react";
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
      <Button
        variant="ghost"
        size="icon"
        onClick={copy}
        className="absolute right-1.5 top-1.5 h-6 w-6 text-muted-foreground hover:text-foreground"
        aria-label="Copy"
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      </Button>
      <pre
        className={cn(
          "overflow-auto whitespace-pre p-3 font-mono text-[11px] leading-relaxed",
          isJson ? "text-foreground" : "text-foreground/80",
        )}
        style={{ maxHeight }}
      >
        {pretty}
      </pre>
    </div>
  );
}
