import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Loader2, MessageCircle, Sparkles } from "lucide-react";
import type { Proxy } from "@/api/client";
import { Button } from "@/components/ui/button";
import { GatewayModelPicker } from "./gateway-model-picker";
import {
  defaultListedGatewayModel,
  listedGatewayModelChoices,
  type GatewayModelListing,
} from "./gateway-models";
import type { ModelChoice } from "@/components/workspace/model-catalog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  buildGatewayTestRequest,
  gatewayTestFailure,
  gatewayTestReply,
  gatewayTraceId,
  helloPrompt,
} from "./gateway-test";

type TestResult = {
  reply?: string;
  title?: string;
  detail?: string;
  traceId?: string;
  elapsed: number;
};

export function GatewayTestDialog({ proxy }: { proxy: Proxy }) {
  const [open, setOpen] = useState(false);
  const [model, setModel] = useState("");
  const [choices, setChoices] = useState<ModelChoice[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelError, setModelError] = useState("");
  const [modelSource, setModelSource] = useState<"provider" | "catalog">(
    "provider",
  );
  const modelController = useRef<AbortController | null>(null);
  const apiOrigin = new URL(
    import.meta.env.VITE_API_URL || window.location.origin,
    window.location.origin,
  ).href.replace(/\/$/, "");
  const loadModels = async () => {
    modelController.current?.abort();
    const abort = new AbortController();
    modelController.current = abort;
    setLoadingModels(true);
    setModelError("");
    setModel("");
    setChoices([]);
    setResult(null);
    const timeout = window.setTimeout(() => abort.abort("timeout"), 30000);
    try {
      const response = await fetch(
        `${apiOrigin}/v1/proxies/${encodeURIComponent(proxy.id)}/models`,
        {
          credentials: "include",
          cache: "no-store",
          redirect: "error",
          signal: abort.signal,
        },
      );
      const body = await response.json();
      if (!response.ok)
        throw new Error(gatewayTestFailure(response.status, body).detail);
      if (
        body.provider !== proxy.target_provider ||
        !Array.isArray(body.models) ||
        !["provider", "catalog"].includes(body.source)
      )
        throw new Error("The provider returned an unexpected model list.");
      if (!abort.signal.aborted) {
        const listing = body as GatewayModelListing;
        const available = listedGatewayModelChoices(listing);
        setChoices(available);
        setModelSource(listing.source);
        setModel(defaultListedGatewayModel(available, proxy.target_model));
        if (available.length === 0)
          setModelError(
            "No chat models were reported. You can enter a custom model ID from your provider.",
          );
      }
    } catch (error) {
      if (!abort.signal.aborted || abort.signal.reason === "timeout")
        setModelError(
          error instanceof Error
            ? error.message
            : "Could not load models. Check this gateway’s provider key or try again.",
        );
    } finally {
      window.clearTimeout(timeout);
      if (modelController.current === abort) {
        modelController.current = null;
        setLoadingModels(false);
      }
    }
  };
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<TestResult | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(
    () => () => {
      controller.current?.abort();
      modelController.current?.abort();
    },
    [],
  );
  const close = () => {
    controller.current?.abort();
    setOpen(false);
    modelController.current?.abort();
    setLoadingModels(false);
    setPending(false);
    setResult(null);
  };
  const runTest = async () => {
    if (!model.trim() || loadingModels || !proxy.is_active || pending) return;
    const request = buildGatewayTestRequest(
      proxy,
      model,
      new URL(
        import.meta.env.VITE_API_URL || window.location.origin,
        window.location.origin,
      ).href,
    );
    setResult(null);
    setPending(true);
    const abort = new AbortController();
    controller.current = abort;
    const timeout = window.setTimeout(() => abort.abort("timeout"), 30000);
    const started = performance.now();
    try {
      const response = await fetch(request.url, {
        method: "POST",
        body: request.body,
        credentials: "include",
        redirect: "error",
        signal: abort.signal,
        headers: request.headers,
      });
      const raw = await response.text();
      let body: unknown;
      try {
        body = JSON.parse(raw);
      } catch {
        body = {
          message:
            "The gateway returned an unexpected response. Check provider configuration and the gateway logs.",
        };
      }
      const traceId = gatewayTraceId(
        response.headers.get("X-Bastio-Trace-Id"),
        body,
      );
      const elapsed = Math.round(performance.now() - started);
      const reply = gatewayTestReply(body);
      if (!abort.signal.aborted)
        setResult(
          response.ok && reply
            ? { reply, traceId, elapsed }
            : {
                ...(response.ok
                  ? {
                      title: "The model returned no text",
                      detail:
                        "Choose a chat model or try again. No text reply was returned, so we could not confirm the hello test.",
                    }
                  : gatewayTestFailure(response.status, body)),
                traceId,
                elapsed,
              },
        );
    } catch {
      if (!abort.signal.aborted || abort.signal.reason === "timeout")
        setResult({
          title:
            abort.signal.reason === "timeout"
              ? "The gateway took too long to reply"
              : "Could not reach the gateway",
          detail:
            "Check the model, provider credentials, and gateway availability, then try again.",
          elapsed: Math.round(performance.now() - started),
        });
    } finally {
      window.clearTimeout(timeout);
      if (controller.current === abort) {
        controller.current = null;
        setPending(false);
      }
    }
  };
  return (
    <>
      <Button
        variant="outline"
        disabled={!proxy.is_active}
        onClick={() => {
          void loadModels();
          setOpen(true);
        }}
      >
        <MessageCircle /> Test gateway
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) close();
        }}
      >
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Say hello through {proxy.name}</DialogTitle>
            <DialogDescription>
              A small live request through your gateway’s normal authentication,
              routing, and security controls. Provider charges and Bastio usage
              limits apply.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void runTest();
            }}
          >
            {loadingModels ? (
              <p
                role="status"
                className="flex items-center gap-2 py-3 text-sm text-muted-foreground"
              >
                <Loader2 className="size-4 animate-spin" /> Loading models with
                your gateway’s provider key…
              </p>
            ) : (
              <GatewayModelPicker
                key={`${modelSource}-${choices.length}`}
                provider={proxy.target_provider}
                choices={choices}
                value={model}
                onChange={(next) => {
                  setModel(next);
                  setResult(null);
                }}
                disabled={pending}
              />
            )}
            {modelError ? (
              <div role="alert" className="space-y-1 text-sm">
                <p className="break-words text-muted-foreground">
                  {modelError}
                </p>
                <Button
                  type="button"
                  variant="link"
                  className="h-auto px-0"
                  disabled={loadingModels || pending}
                  onClick={() => void loadModels()}
                >
                  Retry model discovery
                </Button>
              </div>
            ) : !loadingModels ? (
              <p className="text-xs text-muted-foreground">
                {modelSource === "provider"
                  ? "Models reported by your gateway’s provider key. Say hello confirms that the selected model can answer."
                  : "Suggested models: this provider does not support automatic discovery here. Your provider account controls access."}
                {proxy.target_model &&
                !choices.some(
                  (choice) => choice.model === proxy.target_model,
                ) &&
                modelSource === "provider"
                  ? " Your configured default was not reported; choose a listed model or enter its ID manually."
                  : ""}
              </p>
            ) : null}
            <p className="text-xs text-muted-foreground">
              Bastio handles test authentication automatically. No API key to copy or
              paste; your provider’s key stays on the server.
            </p>
            <div className="rounded-lg border border-border bg-muted/30 p-3">
              <p className="mb-1 text-xs font-medium">Our tiny test prompt</p>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {helloPrompt}
              </p>
            </div>
            {result ? (
              <div
                aria-live="polite"
                className="space-y-2 rounded-lg border border-border bg-muted/30 p-4"
              >
                {result.reply ? (
                  <>
                    <p className="flex items-center gap-2 text-sm font-medium text-success">
                      <Sparkles className="size-4" /> Hello, secure world. Your
                      gateway replied.
                    </p>
                    <p className="whitespace-pre-wrap break-words text-sm leading-relaxed">
                      {result.reply}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {result.elapsed.toLocaleString()} ms · Open the trace to
                      inspect security decisions.
                    </p>
                  </>
                ) : (
                  <div role="alert">
                    <p className="text-sm font-medium text-destructive">
                      {result.title}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">
                      {result.detail}
                    </p>
                  </div>
                )}
                {result.traceId ? (
                  <Link
                    to="/traces/$id"
                    params={{ id: result.traceId }}
                    className="inline-block text-xs underline underline-offset-4"
                  >
                    View test trace
                  </Link>
                ) : null}
              </div>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={close}>
                {pending ? "Cancel test" : "Close"}
              </Button>
              <Button
                type="submit"
                disabled={pending || loadingModels || !model.trim()}
              >
                {pending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <MessageCircle />
                )}
                {pending
                  ? "Sending hello…"
                  : result
                    ? "Say hello again"
                    : "Say hello"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
