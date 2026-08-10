import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Search, ArrowRight } from "lucide-react";
import { cn } from "@/lib/utils";

type Command = {
  id: string;
  label: string;
  hint?: string;
  shortcut?: string;
  group: string;
  onSelect: () => void;
};

/**
 * Global ⌘K / Ctrl+K toggle plus vim-style G-prefix shortcuts:
 *   G O → Overview · G T → Traces · G H → Threats · G U → Users
 *   G S → Sessions · G A → Analytics · G P → Prompts · G K → API Keys · G , → Settings
 *
 * Shortcuts are disabled while an input/textarea is focused, and a short
 * (800ms) debounce aborts the G-prefix if no follow-up key arrives.
 */
export function useCommandPalette() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const gPrefixRef = useRef<number | null>(null);

  useEffect(() => {
    const isEditable = (el: EventTarget | null) => {
      if (!(el instanceof HTMLElement)) return false;
      const tag = el.tagName;
      return (
        tag === "INPUT" ||
        tag === "TEXTAREA" ||
        tag === "SELECT" ||
        el.isContentEditable
      );
    };

    const onKey = (e: KeyboardEvent) => {
      // ⌘K always wins, even inside an input.
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
        return;
      }

      // G-prefix shortcuts — skip when typing.
      if (isEditable(e.target)) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      if (gPrefixRef.current !== null) {
        // We saw G last tick; treat this as the follow-up.
        window.clearTimeout(gPrefixRef.current);
        gPrefixRef.current = null;
        const route = gPrefixMap[e.key.toLowerCase()];
        if (route) {
          e.preventDefault();
          navigate({ to: route });
        }
        return;
      }
      if (e.key.toLowerCase() === "g") {
        gPrefixRef.current = window.setTimeout(() => {
          gPrefixRef.current = null;
        }, 800);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      if (gPrefixRef.current !== null) window.clearTimeout(gPrefixRef.current);
    };
  }, [navigate]);

  const toggle = () => setOpen((o) => !o);
  return { open, setOpen, toggle };
}

const gPrefixMap: Record<string, string> = {
  o: "/",
  t: "/traces",
  h: "/threats",
  s: "/sessions",
  u: "/users",
  a: "/analytics",
  k: "/api-keys",
  ",": "/settings",
};

// UUID / 26-char id detection — loose enough to catch user-typed fragments.
const ID_PATTERN = /^[0-9a-f]{8,}(?:-[0-9a-f]{4,12}){0,4}$/i;

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function CommandPalette({ open, onOpenChange }: Props) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [selectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const modalRef = useRef<HTMLDivElement>(null);
  const lastFocusRef = useRef<HTMLElement | null>(null);

  // Base commands — navigation + resources.
  const baseCommands = useMemo<Command[]>(
    () => [
      { id: "nav-overview",   group: "Navigation", label: "Go to Overview",          shortcut: "G O", onSelect: () => navigate({ to: "/" }) },
      { id: "nav-traces",     group: "Navigation", label: "Go to Traces",            shortcut: "G T", onSelect: () => navigate({ to: "/traces" }) },
      { id: "nav-sessions",   group: "Navigation", label: "Go to Sessions",          shortcut: "G S", onSelect: () => navigate({ to: "/sessions" }) },
      { id: "nav-users",      group: "Navigation", label: "Go to Users",             shortcut: "G U", onSelect: () => navigate({ to: "/users" }) },
      { id: "nav-analytics",  group: "Navigation", label: "Go to Analytics",         shortcut: "G A", onSelect: () => navigate({ to: "/analytics" }) },
      { id: "nav-workspace",  group: "Workforce",  label: "Go to Private AI Portal",                  onSelect: () => navigate({ to: "/workspace" }) },
      { id: "nav-threats",    group: "Security",   label: "Go to Threats",           shortcut: "G H", onSelect: () => navigate({ to: "/threats" }) },
      { id: "nav-security",   group: "Security",   label: "Go to Security Policies",                 onSelect: () => navigate({ to: "/security-settings" }) },
      { id: "nav-compliance", group: "Security",   label: "Go to Compliance & Audits",                onSelect: () => navigate({ to: "/compliance" }) },
      { id: "nav-cache",      group: "Platform",   label: "Go to Response Cache",                     onSelect: () => navigate({ to: "/cache" }) },
      { id: "nav-proxies",    group: "Platform",   label: "Go to Proxies",                            onSelect: () => navigate({ to: "/proxies" }) },
      { id: "nav-api-keys",   group: "Platform",   label: "Go to API Keys",          shortcut: "G K", onSelect: () => navigate({ to: "/api-keys" }) },
      { id: "nav-settings",   group: "Platform",   label: "Go to Settings",          shortcut: "G ,", onSelect: () => navigate({ to: "/settings" }) },
      { id: "nav-docs",       group: "Resources",  label: "Open API Docs",           hint: "↗ /docs", onSelect: () => window.open("/docs", "_blank", "noopener") },
    ],
    [navigate],
  );

  // Dynamic commands — e.g. trace ID jump when the query looks like an id.
  const dynamicCommands = useMemo<Command[]>(() => {
    const q = query.trim();
    if (!q) return [];
    if (!ID_PATTERN.test(q)) return [];
    return [
      {
        id: `jump-trace-${q}`,
        group: "Jump",
        label: `Open trace ${q}`,
        onSelect: () => navigate({ to: "/traces/$id", params: { id: q } }),
      },
    ];
  }, [query, navigate]);

  const filtered = useMemo(() => {
    const qt = query.trim().toLowerCase();
    const base = qt
      ? baseCommands.filter(
          (c) => c.label.toLowerCase().includes(qt) || c.group.toLowerCase().includes(qt),
        )
      : baseCommands;
    return [...dynamicCommands, ...base];
  }, [baseCommands, dynamicCommands, query]);

  // Reset selection when filter changes.
  useEffect(() => {
    setSelectedIdx(0);
  }, [query]);

  // Focus management — save on open, restore on close.
  useEffect(() => {
    if (open) {
      lastFocusRef.current = document.activeElement as HTMLElement;
      setQuery("");
      setSelectedIdx(0);
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
    if (lastFocusRef.current && document.contains(lastFocusRef.current)) {
      lastFocusRef.current.focus();
    }
  }, [open]);

  if (!open) return null;

  // Group filtered commands preserving insertion order.
  const groups: Array<{ name: string; entries: Array<{ cmd: Command; idx: number }> }> = [];
  let running = 0;
  for (const cmd of filtered) {
    let g = groups.find((x) => x.name === cmd.group);
    if (!g) {
      g = { name: cmd.group, entries: [] };
      groups.push(g);
    }
    g.entries.push({ cmd, idx: running++ });
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onOpenChange(false);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIdx((i) => Math.min(filtered.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIdx((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const cmd = filtered[selectedIdx];
      if (cmd) {
        cmd.onSelect();
        onOpenChange(false);
      }
    } else if (e.key === "Tab") {
      // Focus trap — confine Tab cycling to the modal's focusable elements.
      const root = modalRef.current;
      if (!root) return;
      const focusables = root.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (!first || !last) return;
      const active = document.activeElement as HTMLElement | null;
      if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };

  return (
    <>
      <div
        onClick={() => onOpenChange(false)}
        className="fixed inset-0 z-[90] bg-black/50 backdrop-blur-[2px]"
        aria-hidden
      />
      <div
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        onKeyDown={onKey}
        className="fixed top-[14%] left-1/2 -translate-x-1/2 z-[100] w-[92vw] max-w-[560px] surface-elevated p-2"
      >
        {/* Search row */}
        <div className="flex items-center gap-3 px-3 py-3 border-b border-border-subtle">
          <Search className="h-4 w-4 text-text-muted flex-shrink-0" aria-hidden />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search, paste a trace id, or run a command…"
            className="flex-1 bg-transparent border-0 outline-none text-[13px] text-text-primary placeholder:text-text-muted font-sans"
            aria-label="Command palette search"
            autoComplete="off"
            spellCheck={false}
          />
          <Kbd>esc</Kbd>
        </div>

        {/* Results */}
        <div className="max-h-[360px] overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <div className="px-3 py-10 text-center text-[12px] text-text-muted">
              No commands match "{query}"
            </div>
          ) : (
            groups.map(({ name, entries }) => (
              <div key={name}>
                <div className="px-3 py-1.5 text-[10px] font-medium uppercase tracking-[0.1em] text-text-muted">
                  {name}
                </div>
                {entries.map(({ cmd, idx }) => {
                  const isSelected = idx === selectedIdx;
                  return (
                    <button
                      key={cmd.id}
                      type="button"
                      onMouseEnter={() => setSelectedIdx(idx)}
                      onClick={() => {
                        cmd.onSelect();
                        onOpenChange(false);
                      }}
                      className={cn(
                        "w-full flex items-center gap-3 px-3 py-2 rounded-sm text-[13px] text-left transition-colors",
                        isSelected ? "bg-surface-2 text-text-primary" : "text-text-secondary",
                      )}
                    >
                      <ArrowRight
                        className={cn(
                          "h-3.5 w-3.5 flex-shrink-0 transition-opacity",
                          isSelected ? "opacity-100 text-text-primary" : "opacity-30",
                        )}
                        aria-hidden
                      />
                      <span className="flex-1 truncate">{cmd.label}</span>
                      {cmd.hint && (
                        <span className="font-mono text-[11px] text-text-muted">{cmd.hint}</span>
                      )}
                      {cmd.shortcut && <Kbd>{cmd.shortcut}</Kbd>}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center gap-4 px-3 pt-2 border-t border-border-subtle font-mono text-[10px] text-text-muted">
          <span className="inline-flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd>
            navigate
          </span>
          <span className="inline-flex items-center gap-1">
            <Kbd>↵</Kbd>
            select
          </span>
          <span className="inline-flex items-center gap-1">
            <Kbd>esc</Kbd>
            close
          </span>
        </div>
      </div>
    </>
  );
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex items-center font-mono text-[10px] text-text-muted px-1.5 py-0.5 rounded bg-surface-2 border border-border-subtle">
      {children}
    </kbd>
  );
}
