import { useState, useRef, useEffect } from "react";
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
}

export function UserProfileWidget({
  name = "Developer User",
  email = "developer@bastio.ai",
  usageCount = 342,
  usageLimit = 2500,
  planName = "Free Tier",
  onLogout,
}: UserProfileProps) {
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const usagePercent = Math.min(100, Math.round((usageCount / usageLimit) * 100));

  const handleLogout = () => {
    setOpen(false);
    if (onLogout) {
      onLogout();
    } else {
      // Default logout handler: clear token and redirect
      localStorage.removeItem("bastio_token");
      window.location.href = "/";
    }
  };

  return (
    <div className="relative w-full" ref={dropdownRef}>
      {/* Dropdown Menu Popup */}
      {open && (
        <div className="absolute bottom-full left-0 mb-2 w-full bg-card border border-border-subtle rounded-xl shadow-xl p-2 z-50 animate-in fade-in slide-in-from-bottom-2 duration-150">
          {/* User Info Header */}
          <div className="px-3 py-2 border-b border-border-subtle/60 mb-1">
            <p className="text-[13px] font-semibold text-foreground truncate">{name}</p>
            <p className="text-[11px] text-muted-foreground truncate">{email}</p>
            
            {/* Monthly Usage Pill */}
            <div className="mt-2 pt-2 border-t border-border-subtle/40">
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
            </div>
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
              Billing & Upgrade ({planName})
            </Link>
          </div>

          <div className="border-t border-border-subtle/60 mt-1 pt-1">
            <button
              type="button"
              onClick={handleLogout}
              className="w-full flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[12px] text-red-500 hover:bg-red-500/10 transition-colors"
            >
              <LogOut className="h-3.5 w-3.5" />
              Log Out
            </button>
          </div>
        </div>
      )}

      {/* Main Trigger Button */}
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className={cn(
          "w-full flex items-center gap-2.5 px-2.5 py-2 rounded-xl text-left border border-border-subtle/60 hover:border-border-default transition-all duration-150",
          open ? "bg-surface-2 border-border-default" : "bg-card/80 hover:bg-surface-2/60"
        )}
      >
        {/* Avatar circle */}
        <div className="h-7 w-7 rounded-full bg-gradient-to-tr from-amber-500 to-amber-300 text-black font-semibold text-[11px] flex items-center justify-center flex-shrink-0 shadow-sm">
          {name.slice(0, 2).toUpperCase()}
        </div>
        
        <div className="flex-1 min-w-0">
          <p className="text-[12px] font-medium text-foreground truncate leading-none mb-0.5">{name}</p>
          <p className="text-[10px] text-muted-foreground truncate leading-none">{email}</p>
        </div>

        <ChevronUp className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform duration-200", open ? "rotate-0" : "rotate-180")} />
      </button>
    </div>
  );
}
