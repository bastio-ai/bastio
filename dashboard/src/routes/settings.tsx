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
import { AdminPageHeader } from "@/components/admin/admin-primitives";
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
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Workspace administration"
        title="Settings"
        description="Manage workspace ownership and billing, or connect a new OpenAI- or Anthropic-compatible client."
      />

      {ext.accountSections}

      <div className="grid gap-8 lg:grid-cols-[minmax(0,1.4fr)_minmax(18rem,0.6fr)]">
        <section aria-label="Quick-start SDK connection" className="min-w-0">
          <div className="mb-3">
            <h2 className="text-[13px] font-medium text-foreground">
              Connect an OpenAI-compatible client
            </h2>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              Every request is scanned and forwarded through the same gateway
              URL.
            </p>
          </div>
          <div className="relative">
            <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface-1/60 p-4 font-mono text-[12px] leading-relaxed text-foreground/85">
              {codeSnippet}
            </pre>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="absolute top-2.5 right-2.5 h-7 w-7 p-0 text-muted-foreground hover:text-foreground"
              onClick={handleCopy}
              aria-label="Copy quick-start code"
            >
              {copied ? (
                <Check aria-hidden="true" className="h-3.5 w-3.5 text-success" />
              ) : (
                <Copy aria-hidden="true" className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
          <p className="mt-3 border-l-2 border-border-default pl-3 text-xs leading-relaxed text-muted-foreground">
            <span className="font-medium text-foreground">
              One host, one security policy.
            </span>{" "}
            The Anthropic-compatible endpoint is available at{" "}
            <code className="font-mono text-foreground">/v1/messages</code>{" "}
            using the same Bastio API key and gateway host.
          </p>
        </section>

        <section aria-label="Before production">
          <div className="mb-3">
            <h2 className="text-[13px] font-medium text-foreground">
              Before production
            </h2>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              Complete the two controls that establish access and enforcement.
            </p>
          </div>
          <div className="divide-y divide-border-subtle border-y border-border-subtle">
            <Link
              to="/api-keys"
              className="group flex items-center gap-3 py-3.5 transition-colors hover:bg-surface-1/60"
            >
              <KeyRound
                aria-hidden="true"
                className="size-4 shrink-0 text-muted-foreground"
              />
              <div className="min-w-0 flex-1">
                <p className="text-[13px] font-medium text-foreground">
                  Create a scoped API key
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  Bind each workload to a gateway and environment boundary.
                </p>
              </div>
              <ArrowRight
                aria-hidden="true"
                className="size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground"
              />
            </Link>
            <Link
              to="/security-settings"
              className="group flex items-center gap-3 py-3.5 transition-colors hover:bg-surface-1/60"
            >
              <ShieldCheck
                aria-hidden="true"
                className="size-4 shrink-0 text-muted-foreground"
              />
              <div className="min-w-0 flex-1">
                <p className="text-[13px] font-medium text-foreground">
                  Review enforcement
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  Confirm detector actions, PII masking, and runtime controls.
                </p>
              </div>
              <ArrowRight
                aria-hidden="true"
                className="size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground"
              />
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}
