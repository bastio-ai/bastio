import { useState, useRef, useEffect, useId } from "react";
import { Link } from "@tanstack/react-router";
import {
  User,
  CreditCard,
  Key,
  LogOut,
  ChevronUp,
  Zap,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface UserProfileProps {
  name?: string;
  email?: string;
  usageCount?: number;
  usageLimit?: number;
  planName?: string;
  onLogout?: () => void;
  variant?: "sidebar" | "header";
  compact?: boolean;
}

export function UserProfileWidget({
  name = "Local operator",
  email = "",
  usageCount,
  usageLimit,
  planName,
  onLogout,
  variant = "sidebar",
  compact = false,
}: UserProfileProps) {
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuId = useId();

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => menuRef.current?.querySelector<HTMLElement>("a, button")?.focus());
    return () => cancelAnimationFrame(frame);
  }, [open]);

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function handleEscape(event: KeyboardEvent) {
      if (event.key === "Escape" && open) { event.preventDefault(); setOpen(false); triggerRef.current?.focus(); }
    }
    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("keydown", handleEscape);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("keydown", handleEscape);
    };
  }, [open]);

  const hasUsage = typeof usageCount === "number" && typeof usageLimit === "number" && usageLimit > 0;
  const usagePercent = hasUsage ? Math.min(100, Math.round((usageCount / usageLimit) * 100)) : 0;

  const handleLogout = () => {
    setOpen(false);
    onLogout?.();
  };

  return (
    <div className={cn("relative", variant === "header" ? "w-auto" : "w-full")} ref={dropdownRef}>
      {/* Dropdown Menu Popup */}
      {open && (
        <div
          ref={menuRef}
          id={menuId}
          role="region"
          aria-label="Account menu"
          className={cn(
            "absolute z-50 w-64 rounded-xl border border-border-subtle bg-popover p-2 shadow-md animate-in fade-in duration-150",
            variant === "header"
              ? "right-0 top-full mt-2 slide-in-from-top-2"
              : "bottom-full left-0 mb-2 slide-in-from-bottom-2",
          )}
        >
          {/* User Info Header */}
          <div className="px-3 py-2 border-b border-border-subtle/60 mb-1">
            <p className="text-[13px] font-semibold text-foreground truncate">{name}</p>
            {email ? <p className="text-[11px] text-muted-foreground truncate">{email}</p> : null}
            
            {/* Monthly Usage Pill */}
            {hasUsage ? <div className="mt-2 pt-2 border-t border-border-subtle/40">
              <div className="flex items-center justify-between text-[11px] mb-1">
                <span className="text-muted-foreground flex items-center gap-1">
                  <Zap className="h-3 w-3 text-amber-500" /> Usage
                </span>
                <span className="font-mono text-foreground font-medium">
                  {usageCount} / {usageLimit} reqs
                </span>
              </div>
              <div className="w-full bg-surface-2 rounded-full h-1.5 overflow-hidden">
                <div
                  className={cn(
                    "h-full transition-all duration-300",
                    usagePercent > 80 ? "bg-red-500" : "bg-amber-500"
                  )}
                  style={{ width: `${usagePercent}%` }}
                />
              </div>
            </div> : null}
          </div>

          {/* Action Links */}
          <div className="space-y-0.5">
            <Link
              to="/profile"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[12px] text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04] transition-colors"
            >
              <User className="h-3.5 w-3.5" />
              Profile & Security
            </Link>
            <Link
              to="/api-keys"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[12px] text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04] transition-colors"
            >
              <Key className="h-3.5 w-3.5" />
              API Keys
            </Link>
            <Link
              to={"/billing" as never}
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[12px] text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04] transition-colors"
            >
              <CreditCard className="h-3.5 w-3.5" />
              Billing & Subscription{planName ? ` (${planName})` : ""}
            </Link>
          </div>

          {onLogout ? <div className="border-t border-border-subtle/60 mt-1 pt-1">
            <button
              type="button"
              onClick={handleLogout}
              className="w-full flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[12px] text-red-500 hover:bg-red-500/10 transition-colors"
            >
              <LogOut className="h-3.5 w-3.5" />
              Log Out
            </button>
          </div> : null}
        </div>
      )}

      {/* Main Trigger Button */}
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen(!open)}
        aria-label="Open account menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        className={cn(
          "flex items-center text-left transition-colors duration-150",
          variant === "header"
            ? "h-8 w-8 justify-center rounded-full hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            : "w-full gap-2.5 rounded-md px-2.5 py-2 hover:bg-sidebar-accent",
          variant === "sidebar" && open && "border-border-default bg-surface-2",
          compact && "w-9 justify-center px-0",
        )}
      >
        {/* Avatar circle */}
        <div className="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full bg-sidebar-accent text-[11px] font-medium text-foreground">
          {name.trim().split(/\s+/).slice(0, 2).map((part) => part[0]?.toUpperCase()).join("") || "LO"}
        </div>
        
        {variant === "sidebar" && !compact ? <div className="min-w-0 flex-1">
          <p className="text-[12px] font-medium text-foreground truncate leading-none mb-0.5">{name}</p>
          {email ? <p className="text-[10px] text-muted-foreground truncate leading-none">{email}</p> : null}
        </div> : null}

        {variant === "sidebar" && !compact ? <ChevronUp className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform duration-200", open ? "rotate-0" : "rotate-180")} /> : null}
      </button>
    </div>
  );
}
