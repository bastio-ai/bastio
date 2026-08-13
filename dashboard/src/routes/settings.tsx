// Settings page — minimal by design.
//
// Earlier revisions exposed live PG/Redis/ClickHouse health and the
// gateway's configuration (mode, security mode, version, license) on
// this screen. That's appropriate for a debug surface but actively
// harmful as the canonical "Settings" landing page:
//
//   - In Cloud, customers shouldn't see infra internals.
//   - In OSS standalone, the same data is one curl away from /healthz
//     and `docker compose ps`, so the dashboard duplicating it adds
//     noise without value.
//
// The page is now a small onboarding helper. Cloud will overlay
// account / billing / team management here; for tonight, we keep it
// inert and useful: a "point your SDK at Bastio" snippet that auto-
// detects the current origin so the URL is right whether you're on
// staging, prod, or `localhost:3000` in dev.
//
// To extend, prefer additive sections over re-introducing system
// telemetry. The /workspace > Members page is the right place for
// team management; /api-keys is the right place for keys.

import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Check, Copy, KeyRound, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AdminPageHeader, AdminPanel, SecurityNotice } from "@/components/admin/admin-primitives";
import { useSettingsExtension } from "@/components/settings-extension";

function gatewayBaseURL(): string {
  if (typeof window === "undefined") return "https://your-bastio.example.com";
  return `${window.location.protocol}//${window.location.host}`;
}

export function SettingsPage() {
  const ext = useSettingsExtension();
  const [copied, setCopied] = useState(false);
  const base = gatewayBaseURL();
  const codeSnippet = `from openai import OpenAI

client = OpenAI(
    base_url="${base}/v1",
    api_key="sk-bastio-..."  # your Bastio API key
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello!"}],
)`;

  const handleCopy = () => {
    void navigator.clipboard.writeText(codeSnippet);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <>
      <AdminPageHeader
        eyebrow="Workspace administration"
        title="Settings & onboarding"
        description="Manage workspace ownership in the sections below, then connect your first secured application through the gateway."
      />

      {ext.accountSections}

      <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1.5fr)_minmax(18rem,0.5fr)]">
        <AdminPanel
          title="Connect an OpenAI-compatible client"
          description="Every request is scanned and forwarded through the same gateway URL."
        >
          <div className="relative">
            <pre className="overflow-x-auto rounded-lg border border-border/60 bg-muted/25 p-4 font-mono text-[12px] leading-relaxed text-foreground/80">
              {codeSnippet}
            </pre>
            <Button
              variant="ghost"
              size="sm"
              className="absolute top-2.5 right-2.5 h-7 w-7 p-0 text-muted-foreground/50 hover:text-foreground"
              onClick={handleCopy}
              aria-label="Copy quick-start code"
            >
              {copied ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
          <SecurityNotice title="One host, one security policy" className="mt-3">
            The Anthropic-compatible endpoint is available at <span className="font-mono text-foreground">/v1/messages</span>. Use the same Bastio key and gateway host.
          </SecurityNotice>
        </AdminPanel>

        <div className="space-y-4">
          <AdminPanel title="Before production" description="Complete the two controls that establish access and enforcement.">
            <div className="space-y-2">
              <Link to="/api-keys" className="group flex items-center gap-3 rounded-lg border border-border/60 bg-muted/20 p-3 transition-colors hover:bg-muted/40">
                <KeyRound className="size-4 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="text-[12px] font-bold text-foreground">Create a scoped API key</p>
                  <p className="mt-0.5 text-[10px] text-muted-foreground">Prefer least-privilege access.</p>
                </div>
                <ArrowRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </Link>
              <Link to="/security-settings" className="group flex items-center gap-3 rounded-lg border border-border/60 bg-muted/20 p-3 transition-colors hover:bg-muted/40">
                <ShieldCheck className="size-4 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="text-[12px] font-bold text-foreground">Review enforcement</p>
                  <p className="mt-0.5 text-[10px] text-muted-foreground">Confirm detectors and actions.</p>
                </div>
                <ArrowRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </Link>
            </div>
          </AdminPanel>
        </div>
      </div>
    </>
  );
}
