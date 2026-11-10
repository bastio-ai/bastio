import { useId, useState } from "react";
import type { Proxy } from "@/api/client";
import {
  firstRequestSnippets,
  withApiKey,
} from "@/components/getting-started/getting-started";
import { cn } from "@/lib/utils";
import { CopyButton, navigationClass } from "./primitives";
import { useGatewayOrigin } from "./extension-context";

/**
 * Ready-to-run request example for one gateway: cURL or SDK, with a copy
 * button. Without `apiKey` the snippet reads the key from $BASTIO_API_KEY;
 * with it (only right after creation) the secret is filled in.
 */
export function ConnectionExample({
  proxy,
  apiKey,
  className,
}: {
  proxy: Proxy;
  apiKey?: string;
  className?: string;
}) {
  const origin = useGatewayOrigin();
  const id = useId();
  const [selected, setFormat] = useState<"curl" | "sdk">("curl");
  // With a real key filled in, show cURL only: SDK code should read the key
  // from the environment, never embed it.
  const format = apiKey ? "curl" : selected;
  const snippets = firstRequestSnippets(proxy, origin);
  const raw = format === "curl" ? snippets.curl : snippets.sdk;
  const snippet = apiKey ? withApiKey(raw, apiKey) : raw;
  return (
    <div className={cn("min-w-0 space-y-2", className)}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div
          role="group"
          className="flex gap-1"
          aria-label="Request example format"
        >
          {(apiKey ? (["curl"] as const) : (["curl", "sdk"] as const)).map((value) => (
            <button
              type="button"
              key={value}
              aria-pressed={format === value}
              aria-controls={id}
              className={cn(
                navigationClass,
                "min-h-8 px-3 text-xs",
                format === value && "bg-muted text-foreground",
              )}
              onClick={() => setFormat(value)}
            >
              {value === "curl" ? "cURL" : snippets.sdkLabel}
            </button>
          ))}
        </div>
        <CopyButton
          value={snippet}
          label={`Copy ${format === "curl" ? "cURL" : snippets.sdkLabel} example`}
        />
      </div>
      <pre
        id={id}
        className="max-h-72 overflow-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed"
      >
        <code>{snippet}</code>
      </pre>
      {!proxy.target_model && proxy.target_provider !== "typesafe" ? (
        <p className="text-xs text-muted-foreground">
          Replace YOUR_MODEL_ID with a model ID from your provider.
        </p>
      ) : null}
    </div>
  );
}
