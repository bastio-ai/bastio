import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Loader2, MessageCircle, Sparkles } from "lucide-react";
import type { Proxy } from "@/api/client";
import { Button } from "@/components/ui/button";
import { GatewayModelPicker } from "./gateway-model-picker";
import {
  defaultListedGatewayModel,
  listedGatewayModelChoices,
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
import { helloPrompt } from "./gateway-test";
import {
  fetchGatewayModelListing,
  runGatewayTest,
  type GatewayTestResult,
} from "./gateway-requests";

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
  const loadModels = async () => {
    modelController.current?.abort();
    const abort = new AbortController();
    modelController.current = abort;
    setLoadingModels(true);
    setModelError("");
    setModel("");
    setChoices([]);
    setResult(null);
    try {
      const listing = await fetchGatewayModelListing(proxy, abort.signal);
      if (!abort.signal.aborted) {
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
      if (!abort.signal.aborted)
        setModelError(
          error instanceof Error
            ? error.message
            : "Could not load models. Check this gateway’s provider key or try again.",
        );
    } finally {
      if (modelController.current === abort) {
        modelController.current = null;
        setLoadingModels(false);
      }
    }
  };
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<GatewayTestResult | null>(null);
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
    setResult(null);
    setPending(true);
    const abort = new AbortController();
    controller.current = abort;
    try {
      const outcome = await runGatewayTest(proxy, model, abort.signal);
      if (!abort.signal.aborted) setResult(outcome);
    } catch {
      // Aborted by close/unmount: nothing to show.
    } finally {
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
                    <p
                      className={
                        result.outcome === "blocked"
                          ? "text-sm font-medium text-warn"
                          : "text-sm font-medium text-destructive"
                      }
                    >
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
