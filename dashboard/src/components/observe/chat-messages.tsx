import { useMemo } from "react";
import { JsonViewer } from "./json-viewer";

type ChatMessage = {
  role: string;
  content?: string | { type: string; text?: string; [k: string]: unknown }[];
  tool_calls?: unknown;
  name?: string;
};

type Props = {
  // Either a JSON string (OpenAI chat body) or an already-parsed payload.
  raw: string;
  // Optional label shown above the bubbles (e.g. "Request" / "Response").
  label?: string;
  // When true the component tries to render assistant replies (streamed or
  // completion-style) as bubbles as well, falling back to raw JSON when the
  // shape doesn't match.
  direction?: "in" | "out";
};

// Renders OpenAI-shape chat messages as speech bubbles. Falls back to the
// raw JSON if the content doesn't conform — no risk of misleading parsing.
export function ChatMessages({ raw, label, direction = "in" }: Props) {
  const messages = useMemo(() => extractMessages(raw, direction), [raw, direction]);

  if (!raw) {
    return <p className="py-6 text-center text-xs text-muted-foreground">Empty.</p>;
  }

  if (!messages) {
    return <JsonViewer rawString={raw} />;
  }

  return (
    <div className="space-y-2">
      {label ? (
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          {label}
        </p>
      ) : null}
      {messages.map((m, idx) => (
        <MessageBubble key={idx} msg={m} />
      ))}
    </div>
  );
}

function MessageBubble({ msg }: { msg: ChatMessage }) {
  const role = (msg.role || "unknown").toLowerCase();
  const accent =
    role === "system"
      ? "border-muted-foreground/30 bg-muted/30"
      : role === "user"
      ? "border-primary/40 bg-primary/5"
      : role === "assistant"
      ? "border-success-border bg-success-bg"
      : role === "tool"
      ? "border-warn-border bg-warn-bg"
      : "border-border/50 bg-muted/10";
  const text = normalizeContent(msg.content);
  return (
    <div className={`rounded border px-3 py-2 ${accent}`}>
      <div className="mb-1 flex items-center gap-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/80">
        <span>{role}</span>
        {msg.name ? <span className="font-mono normal-case">{msg.name}</span> : null}
      </div>
      {text ? (
        <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-foreground/90">
          {text}
        </pre>
      ) : null}
      {msg.tool_calls ? (
        <div className="mt-2 border-t border-border/30 pt-2">
          <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
            tool calls
          </p>
          <JsonViewer value={msg.tool_calls} maxHeight="20rem" />
        </div>
      ) : null}
    </div>
  );
}

function normalizeContent(content: ChatMessage["content"]): string {
  if (!content) return "";
  if (typeof content === "string") return content;
  return content
    .map((part) => {
      if (typeof part === "string") return part;
      if (part.type === "text" || typeof part.text === "string") return part.text ?? "";
      return JSON.stringify(part);
    })
    .join("\n");
}

function extractMessages(raw: string, direction: "in" | "out"): ChatMessage[] | null {
  try {
    const parsed = JSON.parse(raw);
    if (direction === "in") {
      if (Array.isArray(parsed?.messages)) return parsed.messages as ChatMessage[];
    } else {
      // OpenAI chat.completions response shape.
      const choices = parsed?.choices;
      if (Array.isArray(choices) && choices.length > 0) {
        const msgs: ChatMessage[] = [];
        for (const c of choices) {
          if (c?.message) msgs.push(c.message as ChatMessage);
        }
        if (msgs.length) return msgs;
      }
      // Anthropic messages response: has content array + role=assistant.
      if (parsed?.role && parsed?.content) {
        return [parsed as ChatMessage];
      }
    }
  } catch {
    /* noop */
  }
  return null;
}
