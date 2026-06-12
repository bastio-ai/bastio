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
import { Check, Copy } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { PageHeader, SectionHeader } from "@/components/card";
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
      <PageHeader
        title="Settings"
        description="Account and integration helpers"
      />

      {ext.accountSections}

      <SectionHeader
        title="Quick start"
        description="Point your OpenAI SDK at Bastio to scan and forward requests."
      />

      <Card className="border-border/50">
        <CardContent className="p-5">
          <div className="relative">
            <pre className="p-4 bg-muted/30 rounded-xl text-[12px] font-mono leading-relaxed overflow-x-auto text-foreground/70 border border-border/30">
              {codeSnippet}
            </pre>
            <Button
              variant="ghost"
              size="sm"
              className="absolute top-2.5 right-2.5 h-7 w-7 p-0 text-muted-foreground/50 hover:text-foreground"
              onClick={handleCopy}
            >
              {copied ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
          <p className="text-[12px] text-muted-foreground mt-3">
            Generate a key under{" "}
            <span className="font-mono">/api-keys</span>. The Anthropic
            SDK works at <span className="font-mono">/v1/messages</span>{" "}
            — same key, same host.
          </p>
        </CardContent>
      </Card>
    </>
  );
}
