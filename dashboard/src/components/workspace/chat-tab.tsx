import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "@tanstack/react-query";
import { Plus, Trash2, Pin, PinOff, Bot, Paperclip, X, ArrowUp, MoreHorizontal, Pencil, PanelLeft, PanelLeftClose, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { SkeletonRows } from "@/components/skeleton";
import { MessageBubble, parseMessageAttachments, AttachmentChipStrip } from "./message-bubble";
import { OnboardingCard } from "./onboarding-card";
import {
  ModelPicker,
  DEFAULT_MODEL,
  DEFAULT_MODELS,
  pickModelChoices,
  type ModelChoice,
} from "./model-picker";

import {
  workspaceApi,
  relativeTime,
  type ConversationListItem,
  type Assistant,
} from "./types";

// Aliased so the chat-tab body reads cleanly even though the picker
// module exports its default under the canonical name.
const DEFAULT_MODEL_INITIAL = DEFAULT_MODEL;

// isFirstTime: the workspace has never had a conversation. Drives
// the OnboardingCard branch in the chat surface — vs. the lighter
// "prompt suggestions" empty state shown to returning users who
// just don't have a conversation actively selected.
function isFirstTime(items: ConversationListItem[]): boolean {
  return items.length === 0;
}

type Props = {
  initialConversationID?: string;
};

// readConversationIDFromPath pulls /c/<uuid> from the current
// pathname. Used in useState's initializer so the first render
// already has the right activeID — without this, the activeID
// pushState effect runs on mount with activeID=null and rewrites
// the URL to "/" before the path-read effect can rescue it.
function readConversationIDFromPath(): string | null {
  if (typeof window === "undefined") return null;
  const m = window.location.pathname.match(/^\/c\/([^/]+)$/);
  return m ? (m[1] ?? null) : null;
}

export function ChatTab({ initialConversationID }: Props) {
  const qc = useQueryClient();
  const [activeID, setActiveID] = useState<string | null>(
    () => readConversationIDFromPath() ?? initialConversationID ?? null,
  );
  const [draft, setDraft] = useState("");
  const [streamingDraft, setStreamingDraft] = useState<string | null>(null);
  const [pendingUserMessage, setPendingUserMessage] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Model selection lives at the chat-surface level so the picker
  // applies to the next message — previously-sent messages keep
  // their own provider/model metadata (visible in MessageMeta).
  const [model, setModel] = useState<ModelChoice>(DEFAULT_MODEL_INITIAL);
  const messagesEnd = useRef<HTMLDivElement>(null);
  const scrollContainer = useRef<HTMLDivElement>(null);
  // Tracks whether the user is "stuck to the bottom" — the only state
  // in which we auto-scroll on new content. Once they scroll up, we
  // leave them there until they manually scroll back down (or hit the
  // floating "jump to latest" button).
  const stickToBottom = useRef(true);
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);

  // Assistant the user picked for the NEXT new conversation. Once a
  // conversation exists with an assistant_id bound, that wins — this
  // state only matters for the empty / pre-first-message flow. Null
  // = "use the workspace's default_assistant".
  const [selectedAssistantID, setSelectedAssistantID] = useState<string | null>(null);

  // Pending file attachments. We extract text content client-side for
  // text-shaped files so the model sees the content; binaries are
  // listed by name with a marker. On send, the contents prefix the
  // user message and the local list clears.
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // dragOver = the user is currently dragging files over the chat
  // surface. Drives the visual "Drop files here" overlay so it's
  // obvious this surface accepts drops, vs. the rest of the page.
  const [dragOver, setDragOver] = useState(false);

  // sidebarOpen persists to localStorage so a user's preference
  // survives reloads. Defaults to "open" on desktop, "closed" on
  // mobile — the chat takes the full viewport on phones, the sidebar
  // slides in as an overlay when toggled. Storage key is workspace-
  // scoped so toggling here doesn't bleed into the OSS dashboard.
  const [sidebarOpen, setSidebarOpen] = useState<boolean>(() => {
    if (typeof window === "undefined") return true;
    if (window.innerWidth < 1024) return false;
    return window.localStorage.getItem("workspace-chat-sidebar") !== "closed";
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    // Don't persist mobile open/close transitions — those are
    // ephemeral overlay toggles, not durable layout choices.
    if (window.innerWidth < 1024) return;
    window.localStorage.setItem(
      "workspace-chat-sidebar",
      sidebarOpen ? "open" : "closed",
    );
  }, [sidebarOpen]);

  // ingestFiles is the shared entry point for both the paperclip
  // input and the drop zone. Reads each file via the same readAttachment
  // path (text → client; PDF/docx → server-side ExtractText; image →
  // binary placeholder) and appends to the attachments list.
  const ingestFiles = async (files: FileList | File[]) => {
    const list = Array.from(files);
    if (list.length === 0) return;
    const next = await Promise.all(list.map(readAttachment));
    setAttachments((prev) => [...prev, ...next]);
  };

  const conversations = useQuery({
    queryKey: ["workspace", "conversations"],
    queryFn: () => workspaceApi.listConversations(50),
  });

  const assistants = useQuery({
    queryKey: ["workspace", "assistants"],
    queryFn: workspaceApi.listAssistants,
    staleTime: 60_000,
  });

  // Tenant settings drive the model picker's whitelist. Empty
  // allowed_models = show full catalog; non-empty = strict filter.
  // We refetch settings less often than messages — admin changes are
  // rare relative to chat traffic.
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: () => workspaceApi.getSettings(),
    staleTime: 60_000,
  });
  const availableModels =
    settings.data?.allowed_models && settings.data.allowed_models.length > 0
      ? pickModelChoices(settings.data.allowed_models)
      : DEFAULT_MODELS;

  // Auto-correct the active model when it's not in the allowed list
  // (admin changed the whitelist while the user was on a now-banned
  // model). Run only when the available list changes.
  useEffect(() => {
    if (availableModels.length === 0) return;
    const active = availableModels.find(
      (m) => m.provider === model.provider && m.model === model.model,
    );
    if (!active) {
      setModel(availableModels[0]!);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [availableModels.map((m) => `${m.provider}/${m.model}`).join(",")]);

  const messages = useQuery({
    queryKey: ["workspace", "messages", activeID],
    queryFn: () => workspaceApi.listMessages(activeID!),
    enabled: !!activeID,
  });

  // Auto-scroll only when the user is "stuck to bottom" — i.e. they
  // haven't scrolled up to read history. Without this guard, every
  // streaming token + every conversation poll yanks the viewport
  // back to the latest message and the user can't read older
  // content without sending an empty message to "release" the lock.
  //
  // Threshold: 80px from the absolute bottom counts as "near bottom".
  // Tolerant of small jitter when a token bumps content height.
  useEffect(() => {
    if (!stickToBottom.current) return;
    if (messagesEnd.current) {
      messagesEnd.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages.data, streamingDraft]);

  // onScroll runs on the message-list container. Recomputes whether
  // the user is near the bottom; if not, we stop yanking. The
  // floating "jump to latest" button appears so they can opt back
  // into stick-to-bottom without having to scroll manually.
  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const nearBottom = distFromBottom < 80;
    stickToBottom.current = nearBottom;
    setShowJumpToLatest(!nearBottom);
  };

  // jumpToLatest clears the user's "I'm reading history" state and
  // scrolls to the bottom in one shot. Called by the floating button.
  const jumpToLatest = () => {
    stickToBottom.current = true;
    setShowJumpToLatest(false);
    messagesEnd.current?.scrollIntoView({ behavior: "smooth" });
  };

  // When the user picks a different conversation, reset stick-to-bottom
  // so the new thread opens scrolled to its latest message.
  useEffect(() => {
    stickToBottom.current = true;
    setShowJumpToLatest(false);
  }, [activeID]);

  // Keyboard shortcuts. Cmd/Ctrl+K focuses the compose textarea —
  // matches the universal "command palette / focus search" muscle
  // memory. Cmd/Ctrl+Shift+O starts a new chat. Esc cancels an
  // in-flight stream. We listen on document so the shortcuts work
  // anywhere on the page, including from the sidebar or the
  // assistants tab below.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey;
      if (meta && !e.shiftKey && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        document
          .querySelector<HTMLTextAreaElement>(
            'textarea[placeholder^="Send a message"]',
          )
          ?.focus();
        return;
      }
      if (meta && e.shiftKey && (e.key === "o" || e.key === "O")) {
        e.preventDefault();
        newConversation.mutate();
        return;
      }
      if (e.key === "Escape" && abortRef.current) {
        e.preventDefault();
        cancelStream();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // URL-routed conversation IDs. The workspace-app uses /c/<id>
  // as the canonical URL for an open conversation:
  //
  //   /              → home (no conversation selected)
  //   /c/<uuid>      → that conversation is active
  //
  // The initial state is seeded by readConversationIDFromPath() in
  // useState's initializer — we don't repeat the read here. This
  // listener only handles browser back/forward (popstate).
  useEffect(() => {
    const onPop = () => {
      setActiveID(readConversationIDFromPath());
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  // Push the current activeID into the URL whenever it changes.
  // Compares against current location to avoid duplicate history
  // entries when the path is already correct (e.g. on initial mount
  // after readPath set the state).
  useEffect(() => {
    const next = activeID ? `/c/${activeID}` : "/";
    if (window.location.pathname !== next) {
      window.history.pushState({}, "", next + window.location.search + window.location.hash);
    }
  }, [activeID]);

  // Extension-redirect prefill. The Bastio Governance browser
  // extension ships a "Continue in Workspace" button on its block
  // dialog; that button deep-links here with the original prompt
  // attached as ?prompt=…&model=… (URL-encoded). On mount we decode
  // the values, populate the chat input + selected model, and strip
  // the params from the URL so a refresh doesn't re-prefill.
  useEffect(() => {
    const url = new URL(window.location.href);
    const promptParam = url.searchParams.get("prompt");
    const modelParam = url.searchParams.get("model");
    let dirty = false;

    if (promptParam) {
      setDraft(promptParam);
      url.searchParams.delete("prompt");
      dirty = true;
    }
    if (modelParam) {
      const match = DEFAULT_MODELS.find(
        (m) => `${m.provider}/${m.model}` === modelParam || m.model === modelParam,
      );
      if (match) {
        setModel(match);
        url.searchParams.delete("model");
        dirty = true;
      }
    }
    if (dirty) {
      window.history.replaceState({}, "", url.toString());
    }
    // Run once on mount only — the extension link is a one-shot
    // landing event. eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const newConversation = useMutation({
    mutationFn: () =>
      workspaceApi.createConversation({
        title: "New chat",
        assistant_id: selectedAssistantID ?? undefined,
      }),
    onSuccess: (c) => {
      setActiveID(c.id);
      qc.invalidateQueries({ queryKey: ["workspace", "conversations"] });
    },
  });

  const archive = useMutation({
    mutationFn: workspaceApi.archiveConversation,
    onSuccess: () => {
      setActiveID(null);
      qc.invalidateQueries({ queryKey: ["workspace", "conversations"] });
    },
  });

  // pin/unpin via the new updateConversation PATCH. Doesn't change
  // active conversation. Pinned ones float to the top in the list.
  const togglePin = useMutation({
    mutationFn: ({ id, pinned }: { id: string; pinned: boolean }) =>
      workspaceApi.updateConversation(id, { pinned }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "conversations"] }),
  });

  const rename = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      workspaceApi.updateConversation(id, { title }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "conversations"] }),
  });

  // Tracks the in-flight SSE request so the user can hit Stop. Cleared
  // when the stream finishes (success, error, or user cancel). One
  // request at a time — the send button is disabled while sending.
  const abortRef = useRef<AbortController | null>(null);

  const cancelStream = () => {
    abortRef.current?.abort();
    abortRef.current = null;
  };

  // sendContent runs the actual SSE stream + bookkeeping. Decoupled
  // from the form submit so regenerate + edit can reuse it without
  // reading from the draft state.
  const sendContent = async (composed: string, optimisticUserText?: string) => {
    setSending(true);
    setError(null);
    setPendingUserMessage(optimisticUserText ?? composed);
    setStreamingDraft("");
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      let convID = activeID;
      if (!convID) {
        const c = await workspaceApi.createConversation({
          title: composed.slice(0, 60) || "New chat",
          assistant_id: selectedAssistantID ?? undefined,
        });
        convID = c.id;
        setActiveID(convID);
      }
      await workspaceApi.streamSendMessage(
        convID,
        {
          content: composed,
          provider: model.provider,
          model: model.model,
        },
        (delta) => setStreamingDraft((prev) => (prev ?? "") + delta),
        controller.signal,
      );
      qc.invalidateQueries({ queryKey: ["workspace", "messages", convID] });
      qc.invalidateQueries({ queryKey: ["workspace", "conversations"] });
    } catch (err) {
      // AbortError = user pressed Stop. Don't surface as an error;
      // the partial assistant content is gone (we lose the in-flight
      // tokens because the server didn't finalize the message).
      if ((err as Error).name !== "AbortError") {
        setError((err as Error).message);
      }
    } finally {
      setSending(false);
      setPendingUserMessage(null);
      setStreamingDraft(null);
      abortRef.current = null;
    }
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = draft.trim();
    if (!trimmed && attachments.length === 0) return;
    if (sending) return;

    // Prepend attachment content as a fenced block so the model sees
    // them as context. Binary attachments only carry the filename;
    // users get a clear marker rather than an empty block.
    const composed = composeWithAttachments(trimmed, attachments);
    setDraft("");
    setAttachments([]);
    await sendContent(composed);
  };

  // Regenerate replays the last user message: deletes the last
  // assistant message + last user message, then re-sends the user
  // content. The model sees the same prior context and produces a
  // fresh reply.
  const regenerateLast = async () => {
    if (sending || !activeID) return;
    const msgs = messages.data?.messages ?? [];
    // Walk backwards to find the last user message — that's our
    // anchor for "delete from here onward, replay".
    const lastUser = [...msgs].reverse().find((m) => m.role === "user");
    if (!lastUser) return;
    try {
      await workspaceApi.deleteFromMessage(activeID, lastUser.id);
      await qc.invalidateQueries({ queryKey: ["workspace", "messages", activeID] });
    } catch (err) {
      setError((err as Error).message);
      return;
    }
    await sendContent(lastUser.content);
  };

  // editUserMessage replaces a user message's content and replays
  // from that point: delete from the target onward, send the new
  // content. The conversation re-grows from there.
  const editUserMessage = async (messageID: string, newContent: string) => {
    if (sending || !activeID) return;
    const trimmed = newContent.trim();
    if (!trimmed) return;
    try {
      await workspaceApi.deleteFromMessage(activeID, messageID);
      await qc.invalidateQueries({ queryKey: ["workspace", "messages", activeID] });
    } catch (err) {
      setError((err as Error).message);
      return;
    }
    await sendContent(trimmed);
  };

  return (
    // h-full so the chat fills its parent (the workspace-app's <main>
    // is flex-1 overflow-hidden, the OSS dashboard's /workspace
    // wrapper is flex-1 too). With min-h-0 we let the inner scroll
    // container actually scroll instead of overflowing the parent.
    <div
      className={
        sidebarOpen
          ? // Mobile: chat takes full width, sidebar overlays via
            // fixed positioning (see ConversationList wrapper below).
            // Desktop: side-by-side grid.
            "flex h-full min-h-0 lg:grid lg:grid-cols-[280px_1fr] lg:gap-4"
          : "flex h-full min-h-0"
      }
    >
      {sidebarOpen && (
        <>
          {/* Backdrop — mobile only. Clicking outside the sidebar
              closes it, matching every native side-drawer pattern. */}
          <div
            className="fixed inset-0 z-40 bg-black/40 lg:hidden"
            onClick={() => setSidebarOpen(false)}
            aria-hidden="true"
          />
          {/* Sidebar wrapper. fixed inset on mobile so it slides over
              the chat; grid column on desktop. max-w-[85vw] keeps a
              sliver of chat visible behind on phones — the user can
              tap there to dismiss too. */}
          <div className="fixed inset-y-0 left-0 z-50 w-[300px] max-w-[85vw] lg:relative lg:inset-auto lg:z-auto lg:w-auto lg:max-w-none">
            <ConversationList
              items={conversations.data?.conversations ?? []}
              loading={conversations.isLoading}
              activeID={activeID}
              onSelect={(id) => {
                setActiveID(id);
                // Auto-close on mobile after pick — matches native
                // side-drawer behavior, gets the chat back in view.
                if (typeof window !== "undefined" && window.innerWidth < 1024) {
                  setSidebarOpen(false);
                }
              }}
              onNew={() => {
                newConversation.mutate();
                if (typeof window !== "undefined" && window.innerWidth < 1024) {
                  setSidebarOpen(false);
                }
              }}
              onArchive={(id) => archive.mutate(id)}
              onTogglePin={(id, pinned) => togglePin.mutate({ id, pinned })}
              onRename={(id, title) => rename.mutate({ id, title })}
              onCollapse={() => setSidebarOpen(false)}
            />
          </div>
        </>
      )}

      <Card
        className="relative flex min-h-0 flex-col overflow-hidden"
        onDragOver={(e) => {
          // dataTransfer.types includes "Files" only for file drags —
          // suppresses the overlay during text-selection drags from
          // within the page.
          if (e.dataTransfer.types.includes("Files")) {
            e.preventDefault();
            setDragOver(true);
          }
        }}
        onDragLeave={(e) => {
          // Only clear when leaving the card boundary, not when crossing
          // between child elements (which fires dragleave too).
          if (e.currentTarget.contains(e.relatedTarget as Node)) return;
          setDragOver(false);
        }}
        onDrop={async (e) => {
          if (!e.dataTransfer.types.includes("Files")) return;
          e.preventDefault();
          setDragOver(false);
          await ingestFiles(e.dataTransfer.files);
        }}
      >
        {dragOver && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg border-2 border-dashed border-cyan-500 bg-cyan-500/10 backdrop-blur-sm">
            <div className="rounded-md bg-background/90 px-4 py-2 text-sm font-medium text-foreground shadow-md">
              Drop files to attach
            </div>
          </div>
        )}
        <CardContent className="flex min-h-0 flex-1 flex-col p-0">
          {/* Header strip — model picker + assistant chip moved into
              the compose box (Claude/ChatGPT-shape: controls live with
              the input). All that's left up here is the trust tag, so
              users see the security context without losing screen real
              estate to chrome. When the sidebar is collapsed, a
              PanelLeft button appears on the left so the user can
              bring it back. */}
          <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-2.5">
            {!sidebarOpen ? (
              <button
                type="button"
                onClick={() => setSidebarOpen(true)}
                className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition hover:bg-muted hover:text-foreground"
                aria-label="Show conversations"
                title="Show conversations"
              >
                <PanelLeft className="h-4 w-4" />
              </button>
            ) : (
              // Spacer so the trust tag stays right-aligned when the
              // sidebar is open and the toggle button is hidden.
              <span />
            )}
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
              zero retention by default · audited
            </span>
          </div>
          {/* relative so the floating "jump to latest" button can
              anchor inside the scroll viewport without affecting flow. */}
          <div className="relative flex min-h-0 flex-1 flex-col">
          <div
            ref={scrollContainer}
            onScroll={onScroll}
            className="flex-1 space-y-4 overflow-y-auto p-6"
          >
            {/* Show the empty-state hero + assistant picker when:
                - no conversation is active, OR
                - the active conversation has no messages yet
                In both cases the user is about to compose their
                first message and may want to pick an assistant.
                Once message_count > 0 the conversation hides the
                picker so the message thread takes the viewport. */}
            {(() => {
              const activeConv = conversations.data?.conversations.find(
                (c) => c.id === activeID,
              );
              const isEmpty = !activeID || (activeConv?.message_count ?? 0) === 0;
              if (!isEmpty) return null;
              // Empty-state hero only — the assistant picker is
              // reachable via the AssistantChip in the header. We
              // dropped the always-visible chip grid because it
              // crowded the empty state, especially in workspaces
              // with many assistants.
              if (activeID) return null;
              return isFirstTime(conversations.data?.conversations ?? []) ? (
                <OnboardingCard
                  onSendFirstMessage={() => {
                    document
                      .querySelector<HTMLTextAreaElement>(
                        'textarea[placeholder^="Send a message"]',
                      )
                      ?.focus();
                  }}
                />
              ) : (
                <ChatEmptyState onPickSuggestion={setDraft} />
              );
            })()}
            {activeID && messages.isLoading && <SkeletonRows count={3} />}
            {activeID &&
              messages.data?.messages.map((m, i, arr) => {
                // Regenerate is only meaningful on the LAST assistant
                // message (older ones can't be replayed without
                // throwing away later content). Edit is on every user
                // message — editing mid-history is the UX users
                // actually want.
                const isLastAssistant =
                  m.role === "assistant" && i === arr.length - 1;
                return (
                  <MessageBubble
                    key={m.id}
                    m={m}
                    onRegenerate={isLastAssistant ? regenerateLast : undefined}
                    onEdit={
                      m.role === "user"
                        ? (newContent) => editUserMessage(m.id, newContent)
                        : undefined
                    }
                  />
                );
              })}
            {pendingUserMessage !== null && (
              <PendingBubble userText={pendingUserMessage} streamingDraft={streamingDraft} />
            )}
            {error && (
              <p className="text-center text-xs text-destructive">{error}</p>
            )}
            <div ref={messagesEnd} />
          </div>
          {showJumpToLatest && (
            <button
              type="button"
              onClick={jumpToLatest}
              className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full border border-border bg-background px-3 py-1.5 text-xs text-muted-foreground shadow-md transition hover:bg-muted hover:text-foreground"
            >
              ↓ Jump to latest
            </button>
          )}
          </div>

          {/* Compose caption removed — model + assistant are now
              visible directly in the new compose box's control row,
              and the safety disclaimer lives just below the box. */}

          {/* Compose box — single rounded container holds the
              textarea, attachment chips, and a control row. Mirrors
              the Claude / ChatGPT / Grok / Gemini pattern: text on top,
              + (attach), model, assistant pills bottom-left, circular
              send bottom-right. Box itself shows focus state instead
              of putting a hard cyan ring on the textarea. */}
          <form onSubmit={onSubmit} className="px-4 pb-2 pt-2">
            <div className="rounded-xl border border-border bg-background transition-colors focus-within:border-foreground/30">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                className="hidden"
                onChange={async (e) => {
                  if (!e.target.files) return;
                  await ingestFiles(e.target.files);
                  e.currentTarget.value = "";
                }}
              />

              <textarea
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                  // Auto-grow up to ~10 lines, then internal scroll.
                  // Resets to auto first so deleting characters
                  // shrinks the field too.
                  const ta = e.currentTarget;
                  ta.style.height = "auto";
                  ta.style.height = Math.min(ta.scrollHeight, 240) + "px";
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    onSubmit(e as unknown as React.FormEvent);
                  }
                }}
                rows={1}
                className="block w-full resize-none border-none bg-transparent px-4 pt-3 text-sm placeholder:text-muted-foreground focus:outline-none"
                style={{ minHeight: "44px" }}
                placeholder="Send a message…   (Cmd/Ctrl + Enter to send)"
                disabled={sending}
              />

              {/* Attachment chip strip — appears between the textarea
                  and the control row. Files are kept in browser memory
                  only; nothing leaves until send. */}
              {attachments.length > 0 && (
                <div className="flex flex-wrap gap-1.5 px-3 pb-1">
                  {attachments.map((a, i) => (
                    a.kind === "image" ? (
                      <span
                        key={`${a.name}-${i}`}
                        className="relative inline-flex items-center gap-1 rounded-md border border-border/50 bg-muted/40 p-1 text-[11px]"
                        title={`${a.name} · ${(a.size / 1024).toFixed(0)} KB · sent to model as image`}
                      >
                        <img
                          src={a.dataURL}
                          alt={a.name}
                          className="h-10 w-10 rounded object-cover"
                        />
                        <span className="max-w-[140px] truncate pr-1">{a.name}</span>
                        <button
                          type="button"
                          onClick={() => setAttachments((prev) => prev.filter((_, j) => j !== i))}
                          className="absolute -right-1 -top-1 rounded-full bg-background p-0.5 text-muted-foreground shadow-sm hover:text-destructive"
                          aria-label={`Remove ${a.name}`}
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </span>
                    ) : (
                      <span
                        key={`${a.name}-${i}`}
                        className="inline-flex items-center gap-1 rounded-md border border-border/50 bg-muted/40 px-2 py-0.5 text-[11px]"
                        title={
                          a.kind === "text"
                            ? `${a.text.length.toLocaleString()} chars extracted`
                            : a.reason ?? "binary — content not extracted"
                        }
                      >
                        <Paperclip className="h-3 w-3" />
                        <span className="max-w-[200px] truncate">{a.name}</span>
                        <button
                          type="button"
                          onClick={() => setAttachments((prev) => prev.filter((_, j) => j !== i))}
                          className="text-muted-foreground hover:text-destructive"
                          aria-label={`Remove ${a.name}`}
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </span>
                    )
                  ))}
                </div>
              )}

              {/* Control row inside the box. Left: + (attach) +
                  model picker pill + assistant chip. Right: circular
                  send button — primary tone when there's content. */}
              <div className="flex flex-wrap items-center justify-between gap-2 px-2 py-2">
                <div className="flex flex-wrap items-center gap-1">
                  <button
                    type="button"
                    disabled={sending}
                    onClick={() => fileInputRef.current?.click()}
                    aria-label="Attach files"
                    title="Attach files for context"
                    className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:opacity-50"
                  >
                    <Plus className="h-4 w-4" />
                  </button>
                  <ModelPicker value={model} onChange={setModel} available={availableModels} />
                  <AssistantChip
                    assistants={assistants.data?.assistants ?? []}
                    activeConversationAssistantID={
                      conversations.data?.conversations.find((c) => c.id === activeID)
                        ?.assistant_id ?? null
                    }
                    // Locked once any message has been sent — switching
                    // would rewrite the model's system context. Pre-send
                    // and active conversations with 0 messages stay
                    // editable (the popover triggers an archive +
                    // recreate so a fresh row carries the new pick).
                    isLocked={
                      !!activeID &&
                      (conversations.data?.conversations.find((c) => c.id === activeID)
                        ?.message_count ?? 0) > 0
                    }
                    onStartNewChat={() => newConversation.mutate()}
                    selectedID={selectedAssistantID}
                    onSelect={async (id) => {
                      setSelectedAssistantID(id);
                      if (
                        activeID &&
                        (conversations.data?.conversations.find(
                          (c) => c.id === activeID,
                        )?.message_count ?? 0) === 0
                      ) {
                        await workspaceApi.archiveConversation(activeID).catch(() => {});
                        const c = await workspaceApi.createConversation({
                          title: "New chat",
                          assistant_id: id ?? undefined,
                        });
                        setActiveID(c.id);
                        qc.invalidateQueries({ queryKey: ["workspace", "conversations"] });
                      }
                    }}
                  />
                </div>
                {sending ? (
                  // Stop button — cancels the SSE stream. The
                  // streaming bubble disappears; the user message
                  // stays in the DB (already persisted), the
                  // assistant message is dropped (server never
                  // finalized it). Keyboard: Esc also stops.
                  <button
                    type="button"
                    onClick={cancelStream}
                    aria-label="Stop generating"
                    title="Stop generating (Esc)"
                    className="flex h-8 w-8 items-center justify-center rounded-full bg-foreground text-background transition"
                  >
                    <span className="block h-2.5 w-2.5 rounded-sm bg-background" />
                  </button>
                ) : (
                  <button
                    type="submit"
                    disabled={!draft.trim() && attachments.length === 0}
                    aria-label="Send"
                    className="flex h-8 w-8 items-center justify-center rounded-full bg-foreground text-background transition disabled:bg-muted disabled:text-muted-foreground"
                  >
                    <ArrowUp className="h-4 w-4" />
                  </button>
                )}
              </div>
            </div>
            {/* Disclaimer sits below the box, muted enough to not
                compete with the input itself but visible on every
                load. Static text, not dismissible — every chat
                product we benchmarked keeps it permanently. */}
            <p className="mt-2 text-center text-[10px] text-muted-foreground/60">
              AI can make mistakes. Double-check important information.
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

function ConversationList({
  items,
  loading,
  activeID,
  onSelect,
  onNew,
  onArchive,
  onTogglePin,
  onRename,
  onCollapse,
}: {
  items: ConversationListItem[];
  loading: boolean;
  activeID: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onArchive: (id: string) => void;
  onTogglePin: (id: string, pinned: boolean) => void;
  onRename: (id: string, title: string) => void;
  onCollapse: () => void;
}) {
  // Sidebar search filters by title (case-insensitive substring).
  // Empty query passes through unchanged — fast path. Searching past
  // a couple hundred conversations is fine on the client; if it grows
  // we move to a server-side search endpoint.
  const [query, setQuery] = useState("");
  const filtered = query.trim()
    ? items.filter((c) => c.title.toLowerCase().includes(query.toLowerCase()))
    : items;

  // Bucket conversations by relative date so the sidebar reads like
  // ChatGPT / Claude — Pinned at the top, then Today, Yesterday, Last
  // 7 days, Last 30 days, Older. Empty buckets are skipped. Within
  // each bucket, items keep the server's last_message_at DESC order.
  const groups = bucketConversations(filtered);

  return (
    <Card className="flex h-full min-h-0 flex-col overflow-hidden">
      <CardContent className="flex min-h-0 flex-1 flex-col p-2">
        <div className="mb-2 flex shrink-0 items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            className="flex-1 justify-start"
            onClick={onNew}
          >
            <Plus className="mr-2 h-4 w-4" /> New chat
          </Button>
          <button
            type="button"
            onClick={onCollapse}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition hover:bg-muted hover:text-foreground"
            aria-label="Hide conversations"
            title="Hide conversations"
          >
            <PanelLeftClose className="h-4 w-4" />
          </button>
        </div>
        {/* Sidebar search — filters the bucketed list by title.
            Hidden when there are fewer than 5 conversations to keep
            the sidebar quiet for new tenants. */}
        {items.length >= 5 && (
          <div className="relative mb-2 shrink-0">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search conversations…"
              className="w-full rounded-md border border-border bg-background py-1.5 pl-8 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-foreground/20"
            />
          </div>
        )}
        <div className="flex-1 overflow-y-auto">
          {loading && <SkeletonRows count={5} />}
          {!loading && items.length === 0 && (
            <p className="px-2 py-4 text-xs text-muted-foreground">
              No conversations yet.
            </p>
          )}
          {groups.map((g) =>
            g.items.length === 0 ? null : (
              <section key={g.label} className="mb-3">
                <p className="sticky top-0 mb-1 bg-background px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70">
                  {g.label}
                </p>
                <ul className="space-y-0.5">
                  {g.items.map((c) => (
                    <ConversationRow
                      key={c.id}
                      c={c}
                      active={activeID === c.id}
                      onSelect={onSelect}
                      onTogglePin={onTogglePin}
                      onArchive={onArchive}
                      onRename={onRename}
                    />
                  ))}
                </ul>
              </section>
            ),
          )}
        </div>
        {/* "All chats" link — drops the user on the paginated /chats
            page (Favio-shape). Plain anchor not Link so it works in
            both router-less (workspace-app) and router-aware
            consumers; the workspace-app's App component swaps render
            trees on /chats. */}
        {!loading && items.length > 0 && (
          <a
            href="/chats"
            className="mt-2 block rounded-md px-2 py-2 text-center text-xs text-muted-foreground transition hover:bg-muted hover:text-foreground"
          >
            View all chats →
          </a>
        )}
      </CardContent>
    </Card>
  );
}

function PendingBubble({
  userText,
  streamingDraft,
}: {
  userText: string;
  streamingDraft: string | null;
}) {
  // Same parsing logic the persisted bubble uses, applied to the
  // streaming preview so the user doesn't watch the full PDF text
  // race down their screen as their own message.
  const { attachments, body } = parseMessageAttachments(userText);
  return (
    <>
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-lg bg-cyan-500/10 px-4 py-3 text-sm">
          {attachments.length > 0 && (
            <AttachmentChipStrip attachments={attachments} />
          )}
          {body.trim() && (
            <pre className="whitespace-pre-wrap font-sans">{body}</pre>
          )}
        </div>
      </div>
      <div className="flex justify-start">
        <div className="max-w-[85%] rounded-lg bg-muted px-4 py-3 text-sm">
          {streamingDraft && streamingDraft.length > 0 ? (
            // Render the in-flight tokens as plain text — markdown
            // parsing mid-stream produces visual glitches when partial
            // syntax (a half-finished code fence, an unclosed list)
            // tries to format. The final message lands as full
            // markdown in MessageBubble after the SSE done event.
            <pre className="whitespace-pre-wrap font-sans">
              {streamingDraft}
              <span className="ml-0.5 inline-block h-3 w-1.5 animate-pulse bg-current align-baseline" />
            </pre>
          ) : (
            <span className="inline-block animate-pulse text-muted-foreground">Thinking…</span>
          )}
        </div>
      </div>
    </>
  );
}

// ChatEmptyState is what a redirected employee or first-time visitor
// sees when no conversation is active. The copy reinforces the
// wedge: this is where Shadow AI prompts come instead of public
// chatbots — same speed, your knowledge, your branding.
function ChatEmptyState({ onPickSuggestion }: { onPickSuggestion: (text: string) => void }) {
  const suggestions = [
    "Summarize this Q3 financial report and flag customer-list mentions.",
    "Draft an SLA breach response email to a top-5 customer.",
    "Compare our refund policy with the one in the policy_kb.",
  ];
  return (
    <div className="flex h-full flex-col items-center justify-center gap-6 px-6 text-center">
      <div className="space-y-3 max-w-md">
        <h2 className="text-2xl font-semibold tracking-tight">
          Where AI work actually happens.
        </h2>
        <p className="text-sm text-muted-foreground">
          Same speed as the public chatbots, with your knowledge inline,
          your policies enforced, and zero retention by default.
        </p>
      </div>
      <div className="grid w-full max-w-md grid-cols-1 gap-2">
        {suggestions.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onPickSuggestion(s)}
            className="rounded-md border border-border bg-background px-3 py-2 text-left text-xs text-muted-foreground transition hover:border-cyan-500/40 hover:text-foreground"
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

// =============================================================================
// Assistant picker UI — chip in the header (always visible) plus the
// empty-state chip grid (Favio-shape onboarding affordance).
// =============================================================================

// AssistantChip is the small "Bob" / "No assistant" badge sitting next
// to the ModelPicker. When a conversation exists, the chip reflects
// what's actually bound (read-only — assistants are bound at conv
// create time, you can't switch mid-conversation without breaking
// memory). When no conversation exists, the chip mirrors the user's
// pre-pick selection and clicking it toggles a small popover.
function AssistantChip({
  assistants,
  activeConversationAssistantID,
  isLocked,
  selectedID,
  onSelect,
  onStartNewChat,
}: {
  assistants: Assistant[];
  activeConversationAssistantID: string | null;
  // isLocked = true once the conversation has at least one message.
  // Switching assistants mid-conversation would rewrite the model's
  // system context, so we surface that as a hard boundary in the UI.
  isLocked: boolean;
  selectedID: string | null;
  onSelect: (id: string | null) => Promise<void> | void;
  // Called when the chip's "start a new chat to switch" affordance
  // is clicked. Optional — falls back to a no-op so callers that
  // don't wire it (preview surfaces, tests) just render an inert
  // chip in the locked branch.
  onStartNewChat?: () => void;
}) {
  const [open, setOpen] = useState(false);
  // Display priority: a bound assistant on the active conversation
  // wins (it's what the server will use). Otherwise the user's
  // pre-pick draft selection. Empty-state shows "No assistant".
  const effectiveID = activeConversationAssistantID ?? selectedID;
  const active = effectiveID
    ? assistants.find((a) => a.id === effectiveID) ?? null
    : null;
  const label = active ? active.name : "No assistant";
  const editable = !isLocked && assistants.length > 0;

  if (!editable) {
    // Two read-only sub-states:
    //  - isLocked: conversation in flight, assistant captured at first
    //    send. Click opens a popover that explains why and offers a
    //    "Start new chat" CTA — never auto-navigates without consent.
    //  - assistants.length === 0: workspace has no assistants yet.
    //    Member can't create one (admin-only); tooltip explains who
    //    to ask, click is a no-op.
    if (isLocked) {
      return (
        <LockedAssistantChip
          label={label}
          onStartNewChat={onStartNewChat}
        />
      );
    }
    return (
      <button
        type="button"
        disabled
        title="No assistants configured. Ask your workspace admin to create one."
        className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-md border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground"
      >
        <Bot className="h-3 w-3" />
        {label}
      </button>
    );
  }

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-1.5 rounded-md border border-border bg-muted/40 px-2 py-1 text-xs hover:bg-muted"
      >
        <Bot className="h-3 w-3" />
        {label}
      </button>
      {open && (
        <>
          {/* Click-outside catcher — full-screen overlay below the
              popover. Closes the menu without trapping focus. */}
          <div
            className="fixed inset-0 z-10"
            onClick={() => setOpen(false)}
            aria-hidden="true"
          />
          {/* Popover anchored ABOVE the chip (bottom-full + mb-1) so
              it doesn't overflow the screen when the chip lives near
              the bottom of the viewport (compose row). max-h + overflow-y
              keeps 25-assistant workspaces scrollable instead of
              extending off-screen. */}
          <div
            className="absolute bottom-full left-0 z-20 mb-1 w-64 max-h-[60vh] overflow-y-auto rounded-md border border-border bg-background p-1 shadow-lg"
          >
            <button
              type="button"
              onClick={() => {
                onSelect(null);
                setOpen(false);
              }}
              className={`w-full rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted ${
                selectedID === null ? "bg-muted" : ""
              }`}
            >
              <span className="font-medium">No assistant</span>
              <span className="block text-[10px] text-muted-foreground">
                Use the workspace's default
              </span>
            </button>
            {assistants.map((a) => (
              <button
                key={a.id}
                type="button"
                onClick={() => {
                  onSelect(a.id);
                  setOpen(false);
                }}
                className={`w-full rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted ${
                  selectedID === a.id ? "bg-muted" : ""
                }`}
              >
                <span className="font-medium">{a.name}</span>
                {a.description && (
                  <span className="block truncate text-[10px] text-muted-foreground">
                    {a.description}
                  </span>
                )}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// LockedAssistantChip is the read-only chip rendered when the chat
// already has messages and the assistant is therefore captured.
// Clicking opens a popover that explains *why* swapping isn't
// allowed and offers an explicit "Start a new chat" button — never
// auto-navigates on click. Mid-conversation swap is locked because
// the assistant's system prompt is captured at first send; switching
// later would shift the model's behavior partway through and the
// resulting transcript would mix two different system contexts.
function LockedAssistantChip({
  label,
  onStartNewChat,
}: {
  label: string;
  onStartNewChat?: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-1.5 rounded-md border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground transition hover:bg-muted hover:text-foreground"
      >
        <Bot className="h-3 w-3" />
        {label}
      </button>
      {open && (
        <>
          <div
            className="fixed inset-0 z-10"
            onClick={() => setOpen(false)}
            aria-hidden="true"
          />
          <div className="absolute bottom-full left-0 z-20 mb-1 w-72 rounded-md border border-border bg-background p-3 shadow-lg">
            <p className="text-xs font-medium">Assistant is locked</p>
            <p className="mt-1 text-[11px] text-muted-foreground">
              The assistant's system prompt was captured when you sent
              the first message. Swapping now would shift the model's
              behavior mid-conversation, so this chip is read-only for
              the rest of this chat.
            </p>
            {onStartNewChat && (
              <button
                type="button"
                onClick={() => {
                  onStartNewChat();
                  setOpen(false);
                }}
                className="mt-3 inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-foreground px-2 py-1.5 text-xs font-medium text-background hover:bg-foreground/90"
              >
                Start a new chat
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// =============================================================================
// Conversation list helpers — bucketing + per-row rendering.
// =============================================================================

// bucketConversations splits the (already pinned-first / recency-sorted)
// list into named time buckets that the sidebar renders as sections.
// "Pinned" wins outright — pinned items never appear in date buckets.
// Date math is calendar-day, anchored to local midnight so a chat
// from 11:30pm yesterday isn't filed under "Today" at 12:01am.
function bucketConversations(items: ConversationListItem[]): {
	label: string;
	items: ConversationListItem[];
}[] {
	const now = new Date();
	const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
	const yesterdayStart = todayStart - 24 * 60 * 60 * 1000;
	const last7Start = todayStart - 7 * 24 * 60 * 60 * 1000;
	const last30Start = todayStart - 30 * 24 * 60 * 60 * 1000;

	const pinned: ConversationListItem[] = [];
	const today: ConversationListItem[] = [];
	const yesterday: ConversationListItem[] = [];
	const last7: ConversationListItem[] = [];
	const last30: ConversationListItem[] = [];
	const older: ConversationListItem[] = [];

	for (const c of items) {
		if (c.pinned) {
			pinned.push(c);
			continue;
		}
		const t = new Date(c.last_message_at).getTime();
		if (t >= todayStart) today.push(c);
		else if (t >= yesterdayStart) yesterday.push(c);
		else if (t >= last7Start) last7.push(c);
		else if (t >= last30Start) last30.push(c);
		else older.push(c);
	}

	return [
		{ label: "Pinned", items: pinned },
		{ label: "Today", items: today },
		{ label: "Yesterday", items: yesterday },
		{ label: "Previous 7 days", items: last7 },
		{ label: "Previous 30 days", items: last30 },
		{ label: "Older", items: older },
	];
}

// ConversationRow is one item in the bucketed list. The row body
// shows the title + pin badge + meta. A single 3-dot overflow button
// (hover-visible, always visible while its menu is open or while
// renaming) opens Rename / Pin·Unpin / Archive — same shape as
// Claude's sidebar.
//
// Renaming is inline: the title text becomes an editable input,
// blur or Enter saves, Escape cancels. Cheaper UX than a separate
// dialog and matches the hover/click flow of every modern chat app.
function ConversationRow({
	c,
	active,
	onSelect,
	onTogglePin,
	onArchive,
	onRename,
}: {
	c: ConversationListItem;
	active: boolean;
	onSelect: (id: string) => void;
	onTogglePin: (id: string, pinned: boolean) => void;
	onArchive: (id: string) => void;
	onRename: (id: string, title: string) => void;
}) {
	const [menuOpen, setMenuOpen] = useState(false);
	const [renaming, setRenaming] = useState(false);
	const [draft, setDraft] = useState(c.title);

	const commitRename = () => {
		const next = draft.trim();
		if (next && next !== c.title) {
			onRename(c.id, next);
		}
		setRenaming(false);
	};

	return (
		<li
			onClick={() => {
				if (renaming) return;
				onSelect(c.id);
			}}
			className={`group relative flex cursor-pointer items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm transition ${
				active ? "bg-muted" : "hover:bg-muted/50"
			}`}
		>
			<div className="min-w-0 flex-1">
				<div className="flex items-center gap-1.5">
					{c.pinned && (
						<Pin className="h-3 w-3 shrink-0 fill-cyan-500 text-cyan-500" />
					)}
					{renaming ? (
						<input
							autoFocus
							value={draft}
							onChange={(e) => setDraft(e.target.value)}
							onClick={(e) => e.stopPropagation()}
							onKeyDown={(e) => {
								e.stopPropagation();
								if (e.key === "Enter") {
									e.preventDefault();
									commitRename();
								}
								if (e.key === "Escape") {
									setDraft(c.title);
									setRenaming(false);
								}
							}}
							onBlur={commitRename}
							className="w-full min-w-0 rounded-sm border border-border bg-background px-1 text-sm focus:outline-none focus:ring-1 focus:ring-foreground/20"
						/>
					) : (
						<p className="truncate">{c.title}</p>
					)}
				</div>
				<p className="text-[10px] text-muted-foreground">
					{relativeTime(c.last_message_at)} · {c.message_count} msgs
				</p>
			</div>
			{!renaming && (
				<RowMenu
					open={menuOpen}
					onOpenChange={setMenuOpen}
					pinned={c.pinned}
					onRename={() => {
						setDraft(c.title);
						setRenaming(true);
						setMenuOpen(false);
					}}
					onTogglePin={() => {
						onTogglePin(c.id, !c.pinned);
						setMenuOpen(false);
					}}
					onArchive={() => {
						if (confirm(`Archive "${c.title}"?`)) onArchive(c.id);
						setMenuOpen(false);
					}}
				/>
			)}
		</li>
	);
}

// RowMenu is the 3-dot popover on a conversation row. Hover-visible
// trigger; click-outside catcher closes it. Anchors to the right edge
// of the row so it doesn't push other content. Items: Rename (opens
// inline edit on the parent row), Pin/Unpin (toggles), Archive (with
// confirm).
function RowMenu({
	open,
	onOpenChange,
	pinned,
	onRename,
	onTogglePin,
	onArchive,
}: {
	open: boolean;
	onOpenChange: (next: boolean) => void;
	pinned: boolean;
	onRename: () => void;
	onTogglePin: () => void;
	onArchive: () => void;
}) {
	return (
		<>
			<button
				type="button"
				onClick={(e) => {
					e.stopPropagation();
					onOpenChange(!open);
				}}
				className={`text-muted-foreground hover:text-foreground ${
					open ? "visible" : "invisible group-hover:visible"
				}`}
				aria-label="Conversation actions"
				title="More"
			>
				<MoreHorizontal className="h-4 w-4" />
			</button>
			{open && (
				<>
					<div
						className="fixed inset-0 z-10"
						onClick={(e) => {
							e.stopPropagation();
							onOpenChange(false);
						}}
						aria-hidden="true"
					/>
					{/* Anchored to the row's right edge. mt-6 nudges it
					    below the button — Tailwind doesn't have %-based
					    offsets so we keep it simple. */}
					<div
						className="absolute right-0 top-7 z-20 w-44 rounded-md border border-border bg-background p-1 shadow-lg"
						onClick={(e) => e.stopPropagation()}
					>
						<MenuItem onClick={onRename} icon={<Pencil className="h-3.5 w-3.5" />}>
							Rename
						</MenuItem>
						<MenuItem
							onClick={onTogglePin}
							icon={
								pinned ? (
									<PinOff className="h-3.5 w-3.5" />
								) : (
									<Pin className="h-3.5 w-3.5" />
								)
							}
						>
							{pinned ? "Unpin" : "Pin"}
						</MenuItem>
						<MenuItem
							onClick={onArchive}
							icon={<Trash2 className="h-3.5 w-3.5" />}
							destructive
						>
							Archive
						</MenuItem>
					</div>
				</>
			)}
		</>
	);
}

function MenuItem({
	icon,
	onClick,
	destructive,
	children,
}: {
	icon: React.ReactNode;
	onClick: () => void;
	destructive?: boolean;
	children: React.ReactNode;
}) {
	return (
		<button
			type="button"
			onClick={onClick}
			className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted ${
				destructive
					? "text-destructive hover:text-destructive"
					: "text-foreground"
			}`}
		>
			{icon}
			{children}
		</button>
	);
}

// =============================================================================
// File attachment plumbing — read locally, prefix to message on send.
// =============================================================================

type Attachment =
  | { kind: "text"; name: string; mime: string; text: string }
  // Image attachments carry a data: URL so they round-trip through
  // the message body (markdown image syntax). The backend's
  // workspace handler peels them out into providers.Image content
  // parts before calling the model.
  | { kind: "image"; name: string; mime: string; size: number; dataURL: string }
  | { kind: "binary"; name: string; mime: string; size: number; reason?: string };

// readAttachment loads a File into a text or binary descriptor.
//
// Text-shaped files (txt, md, json, code) are read inline in the
// browser — fast, no server round-trip. PDFs, docx, and the rest are
// posted to /v1/workspace/chat-attachments where the server reuses
// the Knowledge Base extractor (ExtractText). Images are deliberately
// kept as binary placeholders — multimodal support needs the
// providers/* layer to learn content-parts, which is a separate cut.
async function readAttachment(file: File): Promise<Attachment> {
  const clientText =
    file.type.startsWith("text/") ||
    file.type === "application/json" ||
    file.type === "application/xml" ||
    /\.(txt|md|markdown|csv|json|log|yaml|yml|tsv|xml|html|css|js|ts|tsx|jsx|py|go|rs|rb|java|c|cpp|h|hpp|sh|sql)$/i.test(
      file.name,
    );
  if (clientText) {
    const text = await file.text();
    return { kind: "text", name: file.name, mime: file.type || "text/plain", text };
  }

  // Resize images client-side before upload — saves bandwidth +
  // storage + dodges provider-specific size caps. resizeImageFile
  // is a no-op for non-images, small images, and PNGs with alpha
  // (re-encoding would lose transparency). Any failure falls back
  // to the original File so the upload still works.
  const uploadFile = await resizeImageFile(file);

  // Server handles everything else (images included). Server returns
  // is_image=true + data_url for images; extracted=true + text for
  // PDFs / docx; extracted=false for unsupported types.
  try {
    const resp = await workspaceApi.uploadChatAttachment(uploadFile);
    if (resp.is_image && resp.data_url) {
      return {
        kind: "image",
        name: resp.name,
        mime: resp.mime_type,
        size: resp.size,
        dataURL: resp.data_url,
      };
    }
    if (resp.extracted) {
      return { kind: "text", name: resp.name, mime: resp.mime_type, text: resp.text };
    }
    return {
      kind: "binary",
      name: resp.name,
      mime: resp.mime_type,
      size: resp.size,
      reason: resp.extract_error || "content not extracted",
    };
  } catch (err) {
    return {
      kind: "binary",
      name: file.name,
      mime: file.type || "application/octet-stream",
      size: file.size,
      reason: (err as Error).message,
    };
  }
}

// resizeImageFile shrinks oversized images before they go up to the
// server. iPhone / Android photos routinely run 8–12 MP — at 4–6 MB
// each they bloat the message body, the trace row, and the LLM
// request. A 2048-px-on-the-longest-side JPEG at q=0.85 is plenty
// for vision models (gpt-4o, claude-3) which downsample further
// internally; we typically get a 90%+ size reduction.
//
// No-ops:
//   - Non-images: returned as-is.
//   - Small images (<1 MB): no win to be had, returned as-is.
//   - PNG with transparency: canvas re-encode flattens alpha; we
//     keep the original. Detected by drawing one pixel and probing
//     the alpha channel of the resulting ImageData.
//
// Failure: any canvas error or load timeout returns the original
// File. The upload path doesn't depend on resize success.
async function resizeImageFile(file: File): Promise<File> {
  if (!file.type.startsWith("image/")) return file;
  if (file.size < 1 * 1024 * 1024) return file;

  try {
    const dataURL = await fileToDataURL(file);
    const img = await loadImage(dataURL);

    // Alpha probe — if the original has any transparency, bail out
    // so the user's logo / icon doesn't get a black background.
    if (file.type === "image/png" && hasAlpha(img)) return file;

    const max = 2048;
    let w = img.naturalWidth;
    let h = img.naturalHeight;
    if (w <= max && h <= max && file.size < 2 * 1024 * 1024) {
      // Already in-bounds AND under 2 MB — minor savings only,
      // skip the re-encode.
      return file;
    }
    if (w > h && w > max) {
      h = Math.round((h * max) / w);
      w = max;
    } else if (h > max) {
      w = Math.round((w * max) / h);
      h = max;
    }
    const canvas = document.createElement("canvas");
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.drawImage(img, 0, 0, w, h);

    const blob: Blob | null = await new Promise((resolve) =>
      canvas.toBlob(resolve, "image/jpeg", 0.85),
    );
    if (!blob) return file;

    // New filename: original basename + .jpg. Keeps user-recognizable
    // naming while flagging the format change.
    const dot = file.name.lastIndexOf(".");
    const base = dot > 0 ? file.name.slice(0, dot) : file.name;
    return new File([blob], `${base}.jpg`, { type: "image/jpeg" });
  } catch (err) {
    // Network access errors, decode failures, canvas exhaustion —
    // any of them mean we send the original and let the server cap
    // (or the provider) decide. Logged so the user sees something
    // in DevTools if they're debugging "why is the upload huge?".
    // eslint-disable-next-line no-console
    console.warn("resizeImageFile fell back to original:", err);
    return file;
  }
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(String(fr.result));
    fr.onerror = () => reject(fr.error ?? new Error("FileReader failed"));
    fr.readAsDataURL(file);
  });
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("image load failed"));
    img.src = src;
  });
}

// hasAlpha samples a single pixel of the loaded image to check if
// any transparency is in use. Cheap (1×1 canvas), faster than
// scanning every pixel which is overkill for the "is this a logo
// with a transparent background?" decision.
function hasAlpha(img: HTMLImageElement): boolean {
  // Sampling more than one pixel would be more accurate, but PNG
  // logos with transparency typically have a transparent corner —
  // and we'd rather false-positive (skip resize) than flatten alpha.
  const c = document.createElement("canvas");
  c.width = 1;
  c.height = 1;
  const cx = c.getContext("2d");
  if (!cx) return true; // fail-safe: assume alpha
  cx.drawImage(img, 0, 0, 1, 1);
  try {
    const data = cx.getImageData(0, 0, 1, 1).data;
    return data[3] !== undefined && data[3] < 255;
  } catch {
    // Tainted canvas (CORS) — assume alpha to be safe.
    return true;
  }
}

// composeWithAttachments produces the final message body. Text
// attachments inline as fenced code blocks tagged by extension; binary
// attachments get a one-line note. Empty user message + attachments
// is allowed — the model gets just the files and can ask for
// direction.
function composeWithAttachments(message: string, atts: Attachment[]): string {
  if (atts.length === 0) return message;
  const parts: string[] = [];
  for (const a of atts) {
    if (a.kind === "text") {
      const lang = a.name.split(".").pop() ?? "";
      parts.push(`### ${a.name}\n\n\`\`\`${lang}\n${a.text}\n\`\`\``);
    } else if (a.kind === "image") {
      // Markdown image with data URL. Backend regex
      // (extractImagesForProvider) peels these out into proper
      // multimodal image parts before calling the LLM, and the
      // bubble parser renders them as <img> for display.
      parts.push(`![${a.name}](${a.dataURL})`);
    } else {
      const reason = a.reason ?? "content not extracted";
      parts.push(`### ${a.name}\n\n_(${a.mime}, ${a.size} bytes — ${reason})_`);
    }
  }
  if (message.trim()) parts.push(message);
  return parts.join("\n\n");
}
