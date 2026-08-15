import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { BookOpen, Check, Copy, Paperclip, Pencil, RotateCcw } from "lucide-react";

import type { Message } from "./types";

// github-dark is the base. Light-mode overrides live in index.css
// under `:root:not(.dark) .hljs-*` so code blocks match the page
// theme without any JS swap.
import "highlight.js/styles/github-dark.css";

// parseAttachments splits a stored message body into the attachment
// chips that prefix it (when the user paperclip-uploaded files) and
// the actual user text trailing. The compose helper writes blocks
// shaped like:
//
//   ### budget.pdf
//
//   ```pdf
//   …extracted text…
//   ```
//
//   user's actual question
//
// We don't want to dump multi-page PDF text into the chat bubble —
// the model gets the full content (no DB change), the UI just
// renders a "📎 budget.pdf" chip.
//
// Rules:
//   - A leading `### filename\n\n```lang\n...\n````\n\n` block is an
//     attachment with extractable text.
//   - A leading `### filename\n\n_(...)_\n\n` is a binary placeholder.
//   - Anything else (incl. the trailing user message) flows through.
//
// String matching is forgiving: if a user types `### something` as
// their actual question, we leave it alone (the regex requires the
// fenced/italicised body to come right after).
export type ParsedAttachment =
	| { kind: "text"; name: string }
	| { kind: "binary"; name: string }
	| { kind: "image"; name: string; dataURL: string };

// parseMessageAttachments peels every recognized attachment marker
// out of `content` — image data URLs anywhere in the body, plus
// leading code-fenced text blocks and binary placeholders. Returns
// the cleaned body for the bubble to render and a flat list of
// attachments for the chip strip.
//
// Image data URLs are matched anywhere in the content (not just
// leading), because the server's recomposeWithImages may put text
// before images (e.g. when scan sanitizes only the text portion).
// The body left over has those markdown image lines stripped, so
// nothing leaks as raw "![file.png](data:...)" text into the bubble.
//
// Code-fenced text + binary placeholders are still leading-only —
// they're the legacy attachment shape the compose helper writes for
// PDFs / docx / unsupported types.
export function parseMessageAttachments(content: string): {
	attachments: ParsedAttachment[];
	body: string;
} {
	const attachments: ParsedAttachment[] = [];
	let rest = content;

	// 1) Peel leading code-fenced + binary blocks (ordered, repeated).
	for (;;) {
		const fenced = /^### ([^\n]+)\n\n```[^\n]*\n[\s\S]*?\n```(?:\n\n|$)/.exec(rest);
		if (fenced && fenced[1]) {
			attachments.push({ kind: "text", name: fenced[1].trim() });
			rest = rest.slice(fenced[0].length);
			continue;
		}
		const binary = /^### ([^\n]+)\n\n_\([^\n]*\)_(?:\n\n|$)/.exec(rest);
		if (binary && binary[1]) {
			attachments.push({ kind: "binary", name: binary[1].trim() });
			rest = rest.slice(binary[0].length);
			continue;
		}
		break;
	}

	// 2) Strip image data URLs anywhere in the remaining body. Each
	//    match becomes an "image" attachment in display order; the
	//    surrounding text stays in the body. Non-anchored regex
	//    (no ^) so it picks up images after a leading text question.
	const imgPattern =
		/!\[([^\]]*)\]\((data:image\/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=]+)\)/g;
	const matches = [...rest.matchAll(imgPattern)];
	for (const m of matches) {
		if (m[1] !== undefined && m[2]) {
			attachments.push({ kind: "image", name: m[1] || "image", dataURL: m[2] });
		}
	}
	rest = rest.replace(imgPattern, "");
	// Collapse 3+ newlines that the strip leaves behind to a single
	// blank line so the bubble doesn't show a yawning gap.
	rest = rest.replace(/\n{3,}/g, "\n\n").trim();

	return { attachments, body: rest };
}

// MessageBubble renders a single conversation message. Assistant
// messages get full markdown — fenced code blocks with syntax
// highlighting, GFM tables, lists, links, blockquotes. User messages
// stay plain so a copy/paste of code into a chat doesn't accidentally
// collapse into a rendered preview before the model sees it.
//
// Design rationale: chatgpt.com renders both as markdown, but our
// users may be pasting source code, IAM JSON, or stack traces — they
// need to see the literal text they typed. Workspace prefers truth
// over polish on the user side.
export function MessageBubble({
  m,
  onRegenerate,
  onEdit,
}: {
  m: Message;
  onRegenerate?: () => void;
  onEdit?: (newContent: string) => void;
}) {
  const isAssistant = m.role === "assistant";
  const [editing, setEditing] = useState(false);
  // For user messages, peel off any leading attachment blocks so the
  // bubble shows "📎 budget.pdf" + the actual question, not a wall of
  // PDF text. The persisted content is unchanged — the model still
  // sees the full fenced text on the next turn.
  const { attachments, body } = isAssistant
    ? { attachments: [], body: m.content }
    : parseMessageAttachments(m.content);

  // Edit mode lifts the user bubble into a textarea + Save/Cancel.
  // Save calls onEdit which deletes from this message onward and
  // re-sends the new content. Editing keeps the original attachments
  // out of the editor view — they were sent once already, and re-
  // editing them would mean re-uploading. Save will replay just
  // the textual body.
  if (editing && !isAssistant && onEdit) {
    return (
      <UserEditBubble
        initial={body}
        onCancel={() => setEditing(false)}
        onSave={(next) => {
          setEditing(false);
          onEdit(next);
        }}
      />
    );
  }

  return (
    <div className={`group flex ${isAssistant ? "justify-start" : "justify-end"}`}>
      <div
        className={`relative max-w-[85%] rounded-lg px-4 py-3 text-sm ${
          isAssistant
            ? "bg-muted text-foreground"
            : "bg-cyan-500/10 text-foreground"
        }`}
      >
        {attachments.length > 0 && (
          <AttachmentChipStrip attachments={attachments} />
        )}
        {isAssistant ? (
          <MarkdownBody content={body} />
        ) : body.trim() ? (
          <pre className="whitespace-pre-wrap font-sans">{body}</pre>
        ) : null}
        {m.error && (
          <p className="mt-2 text-xs text-destructive">{m.error}</p>
        )}
        {isAssistant && <CitationChips metadata={m.metadata} />}
        {isAssistant && m.model && (
          <MessageMeta
            model={m.model}
            promptTokens={m.prompt_tokens}
            completionTokens={m.completion_tokens}
            costCents={m.cost_cents}
          />
        )}
        {isAssistant && m.content && <CopyButton text={m.content} />}
        {isAssistant && onRegenerate && (
          <RegenerateButton onClick={onRegenerate} />
        )}
        {!isAssistant && onEdit && (
          <EditButton onClick={() => setEditing(true)} />
        )}
      </div>
    </div>
  );
}

// UserEditBubble replaces the static user bubble with an editable
// textarea + Save/Cancel. Cmd/Ctrl+Enter saves, Esc cancels — matches
// the compose-row keymap so muscle memory transfers.
function UserEditBubble({
  initial,
  onSave,
  onCancel,
}: {
  initial: string;
  onSave: (next: string) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(initial);
  return (
    <div className="flex justify-end">
      <div className="w-full max-w-[85%] rounded-lg border border-border bg-cyan-500/10 p-3 text-sm">
        <textarea
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              if (draft.trim()) onSave(draft);
            }
            if (e.key === "Escape") {
              e.preventDefault();
              onCancel();
            }
          }}
          rows={Math.max(2, initial.split("\n").length)}
          className="block w-full resize-y rounded-md border border-border bg-background p-2 text-sm focus:outline-none focus:ring-1 focus:ring-foreground/20"
        />
        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-3 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={!draft.trim()}
            onClick={() => onSave(draft)}
            className="rounded-md bg-foreground px-3 py-1 text-xs text-background hover:opacity-90 disabled:opacity-50"
          >
            Save & resend
          </button>
        </div>
      </div>
    </div>
  );
}

// EditButton — bottom-right of a user bubble, hover-visible.
function EditButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="absolute -bottom-2 right-2 rounded-md bg-background p-1 text-muted-foreground opacity-0 shadow-sm transition hover:text-foreground group-hover:opacity-100"
      title="Edit message"
      aria-label="Edit message"
    >
      <Pencil className="h-3.5 w-3.5" />
    </button>
  );
}

// RegenerateButton — bottom-left of an assistant bubble, hover-visible.
// Only rendered on the LAST assistant message (parent gates this).
function RegenerateButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="absolute -bottom-2 left-2 rounded-md bg-background p-1 text-muted-foreground opacity-0 shadow-sm transition hover:text-foreground group-hover:opacity-100"
      title="Regenerate response"
      aria-label="Regenerate response"
    >
      <RotateCcw className="h-3.5 w-3.5" />
    </button>
  );
}

// AttachmentChipStrip renders attachment indicators above the user
// message body. Images get inline thumbnails (so the user sees what
// they sent); text + binary attachments get small "📎 filename"
// chips. Used both in persisted bubbles and the streaming preview.
export function AttachmentChipStrip({
  attachments,
}: {
  attachments: ParsedAttachment[];
}) {
  return (
    <div className="mb-2 flex flex-wrap gap-1.5">
      {attachments.map((a, i) => {
        if (a.kind === "image") {
          // Static thumbnail. We previously wrapped this in an <a>
          // pointing at the data URL so users could open it
          // full-size, but Chrome treats long data: URLs as
          // about:blank navigations — blank tab, no image. Better to
          // keep it inline and not pretend it's openable.
          return (
            <span
              key={`${a.name}-${i}`}
              className="inline-block overflow-hidden rounded-md border border-border/40"
              title={a.name}
            >
              <img
                src={a.dataURL}
                alt={a.name}
                className="block max-h-48 max-w-[280px] object-contain"
              />
            </span>
          );
        }
        return (
          <span
            key={`${a.name}-${i}`}
            className="inline-flex items-center gap-1 rounded-md border border-border/40 bg-background/40 px-2 py-0.5 text-[11px] text-muted-foreground"
            title={
              a.kind === "binary"
                ? "binary attachment; content not extracted"
                : "text content extracted and sent to the model"
            }
          >
            <Paperclip className="h-3 w-3" />
            <span className="max-w-[200px] truncate">{a.name}</span>
          </span>
        );
      })}
    </div>
  );
}

// CopyButton drops the raw markdown into the user's clipboard. Sits
// in the bottom-right of an assistant bubble. Hover-only on desktop
// to keep the bubble clean; always visible on touch is a follow-up.
function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={async (e) => {
        e.stopPropagation();
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        } catch {
          // Clipboard API can fail under non-secure contexts. Falling
          // back to execCommand isn't worth the surface; the user
          // can still select and copy manually.
        }
      }}
      className="absolute right-2 top-2 rounded-md p-1 text-muted-foreground opacity-0 transition hover:bg-background hover:text-foreground group-hover:opacity-100"
      title={copied ? "Copied" : "Copy message"}
      aria-label={copied ? "Copied" : "Copy message"}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  );
}

// MarkdownBody is the assistant-side renderer. The .markdown-body
// class in index.css carries the typography overrides — heading
// scale, list spacing, code-block padding — so the bubble inherits
// the dashboard's tokens instead of react-markdown's defaults.
function MarkdownBody({ content }: { content: string }) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: true, ignoreMissing: true }]]}
        components={{
          // Tame heading sizes inside chat bubbles — h1 in a chat
          // shouldn't dwarf the rest of the conversation.
          h1: ({ children }) => (
            <h1 className="mb-2 mt-3 text-base font-semibold">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="mb-2 mt-3 text-sm font-semibold">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="mb-1 mt-2 text-sm font-semibold">{children}</h3>
          ),
          p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
          ul: ({ children }) => (
            <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>
          ),
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noreferrer noopener"
              className="text-cyan-500 underline underline-offset-2 hover:text-cyan-400"
            >
              {children}
            </a>
          ),
          code: ({ className, children }) => {
            // Inline code (no language class) gets a subtle pill;
            // fenced code blocks (have language-* class) inherit
            // highlight.js styles via the parent <pre>. We don't
            // forward arbitrary props because the spread carries a
            // ref whose type collides when two copies of @types/react
            // are reachable (the alias-imported source path), and we
            // don't need any of the upstream props anyway.
            const isInline = !className?.includes("language-");
            if (isInline) {
              return (
                <code className="rounded bg-background/60 px-1 py-0.5 text-[0.85em] font-mono">
                  {children}
                </code>
              );
            }
            return <code className={className}>{children}</code>;
          },
          pre: ({ children }) => (
            <pre className="my-2 overflow-x-auto rounded-md bg-background/80 p-3 text-xs">
              {children}
            </pre>
          ),
          blockquote: ({ children }) => (
            <blockquote className="my-2 border-l-2 border-border pl-3 italic text-muted-foreground">
              {children}
            </blockquote>
          ),
          table: ({ children }) => (
            <div className="my-2 overflow-x-auto">
              <table className="border-collapse border border-border text-xs">
                {children}
              </table>
            </div>
          ),
          th: ({ children }) => (
            <th className="border border-border bg-background/40 px-2 py-1 text-left font-semibold">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-border px-2 py-1">{children}</td>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

// CitationChips renders the KB sources the RAG pass pulled in for
// this assistant turn. The backend writes the list to the message's
// `metadata.citations` field at AppendMessage time (see
// rag.go:encodeCitationsMetadata). We render a chip per unique
// source so the user can tell which KB the answer leaned on,
// without having to parse the model's free-form text.
//
// Hidden when no citations are present — a chat surface free of
// chrome stays clean for the (still-common) zero-RAG case.
function CitationChips({ metadata }: { metadata: unknown }) {
  const citations = extractCitations(metadata);
  if (citations.length === 0) return null;
  return (
    <div className="mt-3 flex flex-wrap items-center gap-1.5">
      <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wide text-muted-foreground">
        <BookOpen className="h-3 w-3" />
        Sources
      </span>
      {citations.map((c) => (
        <span
          key={c.source_id}
          className="inline-flex items-center gap-1 rounded-md border border-border bg-background/60 px-1.5 py-0.5 text-[11px] text-muted-foreground"
          title={`Knowledge source: ${c.source_name}`}
        >
          {c.source_name}
        </span>
      ))}
    </div>
  );
}

// extractCitations narrows the message's `metadata` blob to the
// `{ citations: [{source_id, source_name}] }` shape. Defensive
// because the OpenAPI type is `additionalProperties: true` —
// metadata could theoretically hold anything, and the shape may
// evolve over time. Anything that doesn't match returns an empty
// list and the chips just don't render.
function extractCitations(
  metadata: unknown,
): { source_id: string; source_name: string }[] {
  if (!metadata || typeof metadata !== "object") return [];
  const c = (metadata as { citations?: unknown }).citations;
  if (!Array.isArray(c)) return [];
  const out: { source_id: string; source_name: string }[] = [];
  for (const entry of c) {
    if (!entry || typeof entry !== "object") continue;
    const e = entry as { source_id?: unknown; source_name?: unknown };
    if (typeof e.source_id !== "string") continue;
    if (typeof e.source_name !== "string" || !e.source_name) continue;
    out.push({ source_id: e.source_id, source_name: e.source_name });
  }
  return out;
}

// MessageMeta is the small footnote under each assistant response —
// model identity, token usage, cost. Surfaces the same numbers the
// EU AI Act § 50 transparency obligation expects to be visible to
// the user, without dominating the chat surface.
function MessageMeta({
  model,
  promptTokens,
  completionTokens,
  costCents,
}: {
  model: string;
  promptTokens: number;
  completionTokens: number;
  costCents: number;
}) {
  const totalTokens = promptTokens + completionTokens;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
      <span>{model}</span>
      <span>·</span>
      <span>{totalTokens.toLocaleString()} tok</span>
      {costCents > 0 && (
        <>
          <span>·</span>
          <span>{formatCost(costCents)}</span>
        </>
      )}
    </div>
  );
}

function formatCost(cents: number): string {
  if (cents < 1) return "<$0.01";
  return `$${(cents / 100).toFixed(2)}`;
}
