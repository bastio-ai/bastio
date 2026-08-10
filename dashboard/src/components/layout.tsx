import { useEffect } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import {
  LayoutDashboard,
  Activity,
  ShieldAlert,
  BarChart3,
  Server,
  Shield,
  Key,
  Layers,
  MessagesSquare,
  Sun,
  Moon,
  BookOpen,
  FlaskConical,
  Sparkles,
  Zap,
  Bell,
  FileCheck,
} from "lucide-react";
import { Search, Menu } from "lucide-react";
import { Fragment, useState } from "react";
import type { LayoutNavSection } from "@/components/layout-extension";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { useTheme } from "@/lib/use-theme";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { BastioWordmark } from "@/components/bastio-wordmark";
import { Separator } from "@/components/ui/separator";
import { FooterStrip } from "@/components/data/footer-strip";
import { CommandPalette, useCommandPalette } from "@/components/command-palette";
import { useLayoutNav } from "@/components/layout-extension";
import type { ReactNode } from "react";

// Build SHA surfaced in the footer. Wired via `define` in vite.config.ts.
const BUILD_SHA =
  (typeof import.meta !== "undefined" && (import.meta as ImportMeta & { env: Record<string, string | undefined> }).env?.VITE_BUILD_SHA) ||
  "dev";

import { UserProfileWidget } from "@/components/user-profile-widget";

const navSections = [
  {
    label: "Observe",
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard },
      { to: "/traces", label: "Traces", icon: Activity },
      { to: "/cache", label: "Response Cache", icon: Zap },
      { to: "/sessions", label: "Sessions", icon: MessagesSquare },
      { to: "/analytics", label: "Analytics", icon: BarChart3 },
    ],
  },
  {
    label: "Security & Guardrails",
    items: [
      { to: "/threats", label: "Threats", icon: ShieldAlert },
      { to: "/security-settings", label: "Security Center", icon: Shield },
      { to: "/overlays", label: "Custom Policies", icon: Layers },
      { to: "/playground", label: "API Sandbox", icon: FlaskConical },
      { to: "/webhooks", label: "SIEM & Webhooks", icon: Bell },
      { to: "/compliance", label: "Compliance & Audit", icon: FileCheck },
    ],
  },
  {
    label: "Workforce",
    items: [
      { to: "/workspace", label: "Private AI Portal", icon: Sparkles },
    ],
  },
  {
    label: "Developer API",
    items: [
      { to: "/api-keys", label: "API Keys", icon: Key },
      { to: "/proxies", label: "LLM Gateways", icon: Server },
    ],
  },
] as const;

export function Layout({ children }: { children: ReactNode }) {
  const router = useRouterState();
  const currentPath = router.location.pathname;
  const { theme, toggleTheme } = useTheme();
  const palette = useCommandPalette();
  const [mobileOpen, setMobileOpen] = useState(false);

  // Extension nav (cloud-dashboard injects extension routes). Empty in OSS standalone.
  const navExt = useLayoutNav();

  // Close mobile drawer whenever the route changes.
  useEffect(() => {
    setMobileOpen(false);
  }, [currentPath]);

  // Connection pulse derived from /healthz — footer only claims what we verified.
  const health = useQuery({
    queryKey: ["health"],
    queryFn: api.health,
    refetchInterval: 15_000,
    retry: false,
  });
  const connected = health.data?.status === "healthy";

  return (
    <div className="flex h-screen bg-background">
      {/* Mobile: top bar with hamburger + brand */}
      <header className="md:hidden fixed top-0 inset-x-0 z-40 h-12 border-b border-border-subtle bg-background flex items-center px-4 gap-3">
        <button
          type="button"
          onClick={() => setMobileOpen(true)}
          aria-label="Open navigation"
          className="inline-flex items-center justify-center w-8 h-8 rounded-md hover:bg-surface-2 text-text-secondary"
        >
          <Menu className="h-4 w-4" />
        </button>
        <div className="flex items-center text-text-primary">
          <BastioWordmark height={20} mark />
        </div>
      </header>

      {/* Mobile backdrop */}
      {mobileOpen && (
        <div
          onClick={() => setMobileOpen(false)}
          className="md:hidden fixed inset-0 z-40 bg-black/50"
          aria-hidden
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          "w-[220px] flex-shrink-0 border-r border-border-subtle bg-card/50 flex flex-col",
          // Mobile: fixed drawer, slides in/out
          "md:static fixed inset-y-0 left-0 z-50 transition-transform duration-200 md:transition-none",
          mobileOpen ? "translate-x-0" : "-translate-x-full md:translate-x-0",
        )}
      >
        {/* Logo */}
        <div className="px-4 py-4">
          <div className="flex items-center text-text-primary">
            <BastioWordmark height={32} />
          </div>
        </div>

        {/* ⌘K command palette trigger */}
        <div className="px-3 pb-2">
          <button
            type="button"
            onClick={palette.toggle}
            className="w-full flex items-center gap-2 px-2.5 h-8 rounded-md border border-border-subtle text-text-muted hover:text-text-primary hover:border-border-default transition-colors font-sans text-[12px]"
          >
            <Search className="h-3.5 w-3.5" />
            <span>Search or ask…</span>
            <kbd className="ml-auto font-mono text-[10px] text-text-muted px-1.5 py-0.5 rounded bg-surface-2 border border-border-subtle">
              ⌘K
            </kbd>
          </button>
        </div>

        <Separator className="opacity-50" />

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto px-3 py-3 space-y-4">
          {navSections.map((section) => (
            <Fragment key={section.label}>
              <div>
                <p className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">
                  {section.label}
                </p>
                <div className="space-y-0.5">
                  {section.items.map(({ to, label, icon: Icon }) => {
                    const isActive = currentPath === to;
                    return (
                      <Link
                        key={to}
                        to={to as never}
                        className={cn(
                          "flex items-center gap-2.5 px-2.5 py-[7px] rounded-lg text-[13px] transition-all duration-150",
                          isActive
                            ? "bg-foreground/[0.08] text-foreground font-medium shadow-sm"
                            : "text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04]"
                        )}
                      >
                        <Icon className={cn("h-4 w-4 flex-shrink-0", isActive ? "text-foreground" : "text-muted-foreground/70")} />
                        {label}
                      </Link>
                    );
                  })}
                </div>
              </div>
              {/* Cloud-injected sections anchored to this OSS section. */}
              {(navExt.sectionsAfter?.[section.label.toLowerCase()] ?? []).map((extSection) =>
                renderExtSection(extSection, currentPath),
              )}
            </Fragment>
          ))}
          {/* Trailing append-only extension sections. */}
          {navExt.sections?.map((section) => renderExtSection(section, currentPath))}
        </nav>

        {/* Footer & User Profile Widget */}
        <Separator className="opacity-50" />
        <div className="px-3 py-3 space-y-2">
          <UserProfileWidget />

          <div className="flex items-center justify-between px-2 pt-1">
            <a
              href="/docs"
              target="_blank"
              rel="noreferrer"
              className="text-[11px] text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
            >
              <BookOpen className="h-3 w-3" /> Docs
            </a>
            <div className="flex items-center gap-1.5">
              <span className="text-[10px] text-muted-foreground/50 font-medium">v0.1.0</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 text-muted-foreground/50 hover:text-foreground"
                onClick={toggleTheme}
                aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
              >
                {theme === "dark" ? (
                  <Sun className="h-3 w-3" />
                ) : (
                  <Moon className="h-3 w-3" />
                )}
              </Button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 flex flex-col overflow-hidden bg-background pt-12 md:pt-0">
        <div className="flex-1 overflow-auto px-4 md:px-6 py-5">
          <div className="max-w-[1400px] mx-auto">{children}</div>
        </div>
        <FooterStrip buildSha={BUILD_SHA.slice(0, 7)} connected={connected} />
      </main>

      <CommandPalette open={palette.open} onOpenChange={palette.setOpen} />
    </div>
  );
}

// renderExtSection renders one cloud-injected nav section. Extracted
// because it gets called twice — once for sections anchored after a
// specific OSS section (LayoutNavExtension.sectionsAfter), and once
// for the trailing append-only sections (LayoutNavExtension.sections).
// `to` is cast through `as never` because extension `to` values come
// from runtime context and don't appear in the OSS-typed routesByPath
// (cloud-dashboard's router has the actual route registered).
function renderExtSection(section: LayoutNavSection, currentPath: string) {
  return (
    <div key={`ext:${section.label}`}>
      <p className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">
        {section.label}
      </p>
      <div className="space-y-0.5">
        {section.items.map(({ to, label, icon: Icon }) => {
          const isActive = currentPath === to;
          return (
            <Link
              key={to}
              to={to as never}
              className={cn(
                "flex items-center gap-2.5 px-2.5 py-[7px] rounded-lg text-[13px] transition-all duration-150",
                isActive
                  ? "bg-foreground/[0.08] text-foreground font-medium shadow-sm"
                  : "text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04]"
              )}
            >
              <Icon className={cn("h-4 w-4 flex-shrink-0", isActive ? "text-foreground" : "text-muted-foreground/70")} />
              {label}
            </Link>
          );
        })}
      </div>
    </div>
  );
}
