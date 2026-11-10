import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RotateCw, Send } from "lucide-react";
import type { Proxy } from "@/api/client";
import { SecurityNotice } from "@/components/admin/admin-primitives";
import { MODEL_PLACEHOLDER } from "@/components/gateway-setup";
import {
  firstRequestSnippets,
  withApiKey,
} from "@/components/getting-started/getting-started";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { CopyButton, navigationClass } from "./primitives";
import { useGatewayOrigin } from "./extension-context";
import { snippetModelState, type SnippetModelState } from "./gateway-models";
import {
  describeModelError,
  runGatewayTest,
  useGatewayModelListing,
  type GatewayTestResult,
} from "./gateway-requests";

/**
 * Ready-to-run request example for one gateway: cURL or SDK, a model picker
 * fed by the gateway's model discovery, and a copy button. Without `apiKey`
 * the snippet reads the key from $BASTIO_API_KEY; with it (only right after
 * creation) the secret is filled in. `showTest` adds "Send test request",
 * which uses the dashboard session — never the API key.
 */
export function ConnectionExample({
  proxy,
  apiKey,
  showTest = false,
  className,
}: {
  proxy: Proxy;
  apiKey?: string;
  showTest?: boolean;
  className?: string;
}) {
  const origin = useGatewayOrigin();
  const id = useId();
  const [selected, setFormat] = useState<"curl" | "sdk">("curl");
  // With a real key filled in, show cURL only: SDK code should read the key
  // from the environment, never embed it.
  const format = apiKey ? "curl" : selected;

  const typesafe = proxy.target_provider === "typesafe";
  const listing = useGatewayModelListing(proxy);
  const [picked, setPicked] = useState<string | null>(null);
  const [typed, setTyped] = useState("");
  const errorDetail = describeModelError(listing.error);
  const state = snippetModelState({
    configured: proxy.target_model,
    provider: proxy.target_provider,
    listing: listing.data,
    // Initial fetch only: a retry after an error keeps the manual state (and its input) mounted.
    discovering: listing.fetchStatus === "fetching" && listing.status === "pending",
    error: errorDetail,
    picked,
    typed,
  });
  const model = state.model;
  const discovering = state.mode === "loading";
  const placeholder = !model && !typesafe;

  const snippets = firstRequestSnippets(proxy, origin, model);
  const raw = format === "curl" ? snippets.curl : snippets.sdk;
  const snippet = apiKey ? withApiKey(raw, apiKey) : raw;
  const formatLabel = format === "curl" ? "cURL" : snippets.sdkLabel;
  const test = useGatewayTest(proxy, model);

  return (
    <div className={cn("min-w-0 space-y-2", className)}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          <div role="group" className="flex gap-1" aria-label="Request example format">
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
          {!typesafe && state.mode === "picker" ? (
            <Select
              value={model}
              onValueChange={(next) => {
                if (typeof next === "string") setPicked(next);
              }}
            >
              <SelectTrigger
                aria-label="Model used in the example"
                className="h-8 max-w-56 min-w-0 font-mono text-[11px]"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {state.options.map((option) => (
                  <SelectItem key={option} value={option} className="font-mono text-[11px]">
                    {option}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
        </div>
        <div className="flex shrink-0 items-start gap-2">
          {showTest ? (
            <Button
              variant="outline"
              disabled={placeholder || discovering || test.pending || !proxy.is_active}
              title={placeholder ? "Enter a model ID first" : undefined}
              onClick={() => void test.send()}
            >
              {test.pending ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : <Send />}
              {test.pending ? "Sending…" : "Send test request"}
            </Button>
          ) : null}
          <CopyButton value={snippet} label={`Copy ${formatLabel} example`} />
        </div>
      </div>

      {typesafe ? (
        <div className="min-h-5" aria-hidden="true" />
      ) : (
        <ModelStatus
          mode={state.mode}
          reason={state.reason}
          placeholder={placeholder}
          inactive={!proxy.is_active}
          errorDetail={errorDetail}
          typed={typed}
          onTyped={setTyped}
          retrying={listing.isFetching}
          onRetry={() => void listing.refetch()}
        />
      )}

      <pre
        id={id}
        aria-busy={discovering ? true : undefined}
        className={cn(
          "max-h-72 overflow-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed transition-opacity",
          discovering && "opacity-60 motion-reduce:transition-none",
        )}
      >
        <code>{markPlaceholder(snippet)}</code>
      </pre>
      {showTest ? (
        <div aria-live="polite">{test.result ? <TestResultView result={test.result} /> : null}</div>
      ) : null}
    </div>
  );
}

/**
 * Status line between the controls and the code block. Loading keeps the
 * same one-line height as the idle state, so the block does not move when
 * discovery finishes. Without a discovered model it becomes the warning
 * notice with the server's reason, a retry, and a free-text model ID input
 * so the user is never stuck on the placeholder.
 */
function ModelStatus({
  mode,
  reason,
  placeholder,
  inactive,
  errorDetail,
  typed,
  onTyped,
  retrying,
  onRetry,
}: {
  mode: SnippetModelState["mode"];
  reason: SnippetModelState["reason"];
  placeholder: boolean;
  inactive: boolean;
  errorDetail: string | null;
  typed: string;
  onTyped: (value: string) => void;
  retrying: boolean;
  onRetry: () => void;
}) {
  const inputId = useId();
  if (mode === "loading")
    return (
      <p role="status" className="flex min-h-5 items-center gap-1.5 text-xs text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" aria-hidden="true" />
        Finding a model your provider key can use…
      </p>
    );
  if (mode !== "manual") return <div className="min-h-5" aria-hidden="true" />;
  const why = inactive
    ? "This gateway is disabled, so its models can’t be listed."
    : reason === "error"
      ? "Bastio couldn’t list this provider key’s models."
      : reason === "catalog"
        ? "This provider doesn’t report its models to Bastio."
        : "Your provider key reported no chat models.";
  const input = (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <label htmlFor={inputId} className="text-xs font-medium text-foreground">
        Model ID
      </label>
      <Input
        id={inputId}
        value={typed}
        onChange={(event) => onTyped(event.target.value)}
        placeholder="e.g. a model ID from your provider’s console"
        autoComplete="off"
        spellCheck={false}
        className="h-8 min-w-0 flex-1 basis-48 font-mono text-xs"
      />
      {reason === "error" && !inactive ? (
        <Button type="button" size="sm" variant="outline" disabled={retrying} onClick={onRetry}>
          {retrying ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : <RotateCw />}
          Retry
        </Button>
      ) : null}
    </div>
  );
  return (
    // One element tree for both states: typing must not remount the input.
    <SecurityNotice
      tone={placeholder ? "warning" : "info"}
      className="px-3 py-2.5"
      title={
        placeholder
          ? `Replace ${MODEL_PLACEHOLDER} with a model from your provider before running`
          : "Using the model ID you entered"
      }
    >
      <p className="text-xs">
        {why}{" "}
        {placeholder
          ? "Type a model ID below, or set a default model in the gateway’s settings."
          : "Check it matches a model your provider account can use."}
      </p>
      {errorDetail && reason === "error" ? (
        <p className="mt-1 break-words font-mono text-[11px] text-muted-foreground">{errorDetail}</p>
      ) : null}
      {input}
    </SecurityNotice>
  );
}

function markPlaceholder(snippet: string): ReactNode {
  const parts = snippet.split(MODEL_PLACEHOLDER);
  if (parts.length === 1) return snippet;
  return parts.flatMap((part, index) =>
    index === 0
      ? [part]
      : [
          <mark key={index} className="rounded-sm bg-warn-bg px-0.5 text-warn">
            {MODEL_PLACEHOLDER}
          </mark>,
          part,
        ],
  );
}

/**
 * Sends the dashboard's hello test (same gateway, same model as the example)
 * with the session. A security block is a valid outcome, shown as such.
 */
function useGatewayTest(proxy: Proxy, model: string) {
  const qc = useQueryClient();
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<GatewayTestResult | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  useEffect(() => setResult(null), [model]);

  const send = async () => {
    if (pending || !model) return;
    controller.current?.abort();
    const abort = new AbortController();
    controller.current = abort;
    setPending(true);
    setResult(null);
    try {
      const outcome = await runGatewayTest(proxy, model, abort.signal);
      if (abort.signal.aborted) return;
      setResult(outcome);
      // Any request that reached the gateway records a trace — let the
      // Get started checklist notice right away.
      void qc.invalidateQueries({ queryKey: ["traces", "first"] });
    } catch {
      // Aborted on unmount.
    } finally {
      if (controller.current === abort) {
        controller.current = null;
        setPending(false);
      }
    }
  };
  return { pending, result, send };
}

function TestResultView({ result }: { result: GatewayTestResult }) {
  const tone =
    result.outcome === "ok" ? "success" : result.outcome === "blocked" ? "warning" : "danger";
  const firstLines = result.reply?.split("\n").slice(0, 3).join("\n");
  return (
    <div
      className={cn(
        "rounded-md border px-3 py-2",
        tone === "success" && "border-success-border bg-success-bg",
        tone === "warning" && "border-warn-border bg-warn-bg",
        tone === "danger" && "border-danger-border bg-danger-bg",
      )}
    >
      <p
        className={cn(
          "text-xs font-medium",
          tone === "success" && "text-success",
          tone === "warning" && "text-warn",
          tone === "danger" && "text-danger",
        )}
      >
        {result.outcome === "ok" ? "Your gateway replied" : result.title}
        <span className="ml-1.5 font-mono font-normal tabular-nums text-muted-foreground">
          {result.status ? `${result.status} · ` : ""}
          {result.elapsed.toLocaleString()} ms
        </span>
      </p>
      <p className="mt-1 line-clamp-3 whitespace-pre-wrap break-words text-xs text-muted-foreground">
        {result.outcome === "ok" ? firstLines : result.detail}
      </p>
      {result.traceId ? (
        <Link
          to="/traces/$id"
          params={{ id: result.traceId }}
          className="mt-1 inline-block text-xs underline underline-offset-4"
        >
          {result.outcome === "blocked" ? "See why in the trace" : "View trace"}
        </Link>
      ) : null}
    </div>
  );
}
