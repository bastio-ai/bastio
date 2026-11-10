import { useQuery } from "@tanstack/react-query";
import type { Proxy } from "@/api/client";
import type { GatewayModelListing } from "./gateway-models";
import {
  buildGatewayTestRequest,
  gatewayBlockReason,
  gatewayTestBlocked,
  gatewayTestFailure,
  gatewayTestReply,
  gatewayTraceId,
} from "./gateway-test";

// Browser requests shared by the gateway test dialog and the connection
// examples. Both use the dashboard session (credentials: "include") against
// the dashboard API origin — never a Bastio API key.

const TIMEOUT_MS = 30_000;

/** Origin of the dashboard API (not the public gateway origin). */
export function dashboardApiOrigin() {
  return new URL(
    import.meta.env.VITE_API_URL || window.location.origin,
    window.location.origin,
  ).href.replace(/\/$/, "");
}

/** Run `work` with a 30s timeout, linked to an optional outer abort signal. */
async function withTimeout<T>(
  outer: AbortSignal | undefined,
  work: (signal: AbortSignal) => Promise<T>,
) {
  const abort = new AbortController();
  const timeout = window.setTimeout(() => abort.abort("timeout"), TIMEOUT_MS);
  const forward = () => abort.abort(outer?.reason);
  outer?.addEventListener("abort", forward);
  try {
    return await work(abort.signal);
  } catch (error) {
    if (abort.signal.reason === "timeout") throw new GatewayTimeoutError();
    throw error;
  } finally {
    window.clearTimeout(timeout);
    outer?.removeEventListener("abort", forward);
  }
}

export class GatewayTimeoutError extends Error {
  constructor() {
    super("The request timed out");
  }
}

/** Models reported by the gateway's provider key (or a public catalog). */
export async function fetchGatewayModelListing(
  proxy: Proxy,
  signal?: AbortSignal,
): Promise<GatewayModelListing> {
  return withTimeout(signal, async (linked) => {
    const response = await fetch(
      `${dashboardApiOrigin()}/v1/proxies/${encodeURIComponent(proxy.id)}/models`,
      { credentials: "include", cache: "no-store", redirect: "error", signal: linked },
    );
    const body: unknown = await response.json();
    if (!response.ok)
      throw new Error(gatewayTestFailure(response.status, body).detail);
    const listing = body as Partial<GatewayModelListing>;
    if (
      listing.provider !== proxy.target_provider ||
      !Array.isArray(listing.models) ||
      (listing.source !== "provider" && listing.source !== "catalog")
    )
      throw new Error("The provider returned an unexpected model list.");
    return listing as GatewayModelListing;
  });
}

/**
 * Cached model discovery for one gateway. Shared query key so the snippet,
 * picker and panel reuse one provider call; no retries (each is a live
 * provider request).
 */
export function useGatewayModelListing(proxy: Proxy) {
  return useQuery({
    queryKey: ["gateway-models", proxy.id, proxy.target_provider],
    queryFn: ({ signal }) => fetchGatewayModelListing(proxy, signal),
    enabled: proxy.is_active,
    staleTime: 5 * 60_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

export type GatewayTestResult = {
  /** "ok": the model replied; "blocked": a Bastio security policy stopped it. */
  outcome: "ok" | "blocked" | "error";
  reply?: string;
  title?: string;
  detail?: string;
  traceId?: string;
  status?: number;
  elapsed: number;
};

/** Send the dashboard's hello test through the gateway with the session. */
export async function runGatewayTest(
  proxy: Proxy,
  model: string,
  signal?: AbortSignal,
): Promise<GatewayTestResult> {
  const request = buildGatewayTestRequest(proxy, model, dashboardApiOrigin());
  const started = performance.now();
  const elapsed = () => Math.round(performance.now() - started);
  try {
    return await withTimeout(signal, async (linked) => {
      const response = await fetch(request.url, {
        method: "POST",
        body: request.body,
        credentials: "include",
        redirect: "error",
        signal: linked,
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
      const traceId = gatewayTraceId(response.headers.get("X-Bastio-Trace-Id"), body);
      const reply = gatewayTestReply(body);
      if (response.ok && reply)
        return { outcome: "ok" as const, reply, traceId, status: response.status, elapsed: elapsed() };
      if (response.ok)
        return {
          outcome: "error" as const,
          title: "The model returned no text",
          detail:
            "Choose a chat model or try again. No text reply was returned, so we could not confirm the hello test.",
          traceId,
          status: response.status,
          elapsed: elapsed(),
        };
      if (gatewayTestBlocked(response.status, body))
        return {
          outcome: "blocked" as const,
          title: "Bastio blocked this request",
          detail: gatewayBlockReason(body),
          traceId,
          status: response.status,
          elapsed: elapsed(),
        };
      return {
        outcome: "error" as const,
        ...gatewayTestFailure(response.status, body),
        traceId,
        status: response.status,
        elapsed: elapsed(),
      };
    });
  } catch (error) {
    if (signal?.aborted) throw error;
    return {
      outcome: "error",
      title:
        error instanceof GatewayTimeoutError
          ? "The gateway took too long to reply"
          : "Could not reach the gateway",
      detail:
        "Check the model, provider credentials, and gateway availability, then try again.",
      elapsed: elapsed(),
    };
  }
}
