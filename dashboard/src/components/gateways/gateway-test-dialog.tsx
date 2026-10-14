import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Loader2, MessageCircle, Sparkles } from "lucide-react";
import type { Proxy } from "@/api/client";
import { FieldLabel } from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { GatewayModelPicker } from "./gateway-model-picker";
import { defaultGatewayTestModel } from "./gateway-models";
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
  const [apiKey, setAPIKey] = useState("");
  const [model, setModel] = useState(() =>
    defaultGatewayTestModel(proxy.target_provider, proxy.target_model),
  );
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<TestResult | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const close = () => {
    controller.current?.abort();
    setOpen(false);
    setAPIKey("");
    setPending(false);
    setResult(null);
  };
  const runTest = async () => {
    if (!model.trim() || !apiKey.trim() || !proxy.is_active || pending) return;
    const request = buildGatewayTestRequest(
      proxy,
      model,
      new URL(
        import.meta.env.VITE_API_URL || window.location.origin,
        window.location.origin,
      ).href,
    );
    const credential = apiKey.trim();
    setAPIKey("");
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
        credentials: "omit",
        redirect: "error",
        signal: abort.signal,
        headers: {
          ...request.headers,
          ...(proxy.target_provider === "anthropic"
            ? { "x-api-key": credential }
            : { Authorization: `Bearer ${credential}` }),
        },
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
          setModel(
            defaultGatewayTestModel(proxy.target_provider, proxy.target_model),
          );
          setResult(null);
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
            <GatewayModelPicker
              provider={proxy.target_provider}
              configuredModel={proxy.target_model}
              value={model}
              onChange={(next) => {
                setModel(next);
                setResult(null);
              }}
              disabled={pending}
            />
            <div>
              <FieldLabel htmlFor="test-gateway-key">
                Bastio application API key
              </FieldLabel>
              <Input
                id="test-gateway-key"
                type="password"
                required
                disabled={pending}
                autoComplete="off"
                value={apiKey}
                onChange={(event) => setAPIKey(event.target.value)}
                placeholder="Paste your sk-bastio-… key"
              />
              <p className="mt-1.5 text-xs text-muted-foreground">
                Used only for this request and cleared after sending. Your
                provider’s key stays on the server.
              </p>
            </div>
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
                disabled={pending || !model.trim() || !apiKey.trim()}
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
