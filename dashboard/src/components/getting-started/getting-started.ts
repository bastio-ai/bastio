import type { APIKey, ProviderKey, Proxy } from "../../api/client";
import { gatewayAppKeys, gatewayConnection } from "../gateway-setup.ts";

// Pure logic for the Overview "Get started" checklist. Kept free of React
// and path aliases so `node --test --experimental-strip-types` can import it.

export type GetStartedStepId = "provider" | "gateway" | "request";

export type GetStartedInput = {
  providerKeys: ProviderKey[];
  /** Providers the host supplies credentials for (cloud-managed keys). */
  managedProviders: string[];
  proxies: Proxy[];
  apiKeys: APIKey[];
  /** Result of an unbounded `traces.list({ limit: 1 })`. */
  traceCount: number;
};

export type GetStartedState = {
  done: Record<GetStartedStepId, boolean>;
  /** First incomplete step, or null when setup is complete. */
  current: GetStartedStepId | null;
  completed: number;
  /** Gateway the request snippet and "create key" action target. */
  gateway: Proxy | null;
};

const SERVER_AUTH_PROVIDERS = ["ollama", "bedrock"];

export function deriveGetStarted(input: GetStartedInput): GetStartedState {
  const hasTraffic = input.traceCount > 0;
  const activeProxies = input.proxies.filter((proxy) => proxy.is_active);
  // A provider is "connected" by a stored key, a host-managed credential, or a
  // gateway whose provider authenticates through server configuration.
  const provider =
    hasTraffic ||
    input.providerKeys.length > 0 ||
    input.managedProviders.length > 0 ||
    activeProxies.some((proxy) => SERVER_AUTH_PROVIDERS.includes(proxy.target_provider));
  const activeKeys = input.apiKeys.filter((key) => key.is_active);
  const gateway = pickGateway(activeProxies, input.apiKeys);
  const gatewayDone = hasTraffic || (activeProxies.length > 0 && activeKeys.length > 0);
  const done: Record<GetStartedStepId, boolean> = {
    provider,
    gateway: gatewayDone,
    request: hasTraffic,
  };
  const order: GetStartedStepId[] = ["provider", "gateway", "request"];
  return {
    done,
    current: order.find((id) => !done[id]) ?? null,
    completed: order.filter((id) => done[id]).length,
    gateway,
  };
}

/** Prefer the newest active gateway that already has an app key. */
function pickGateway(proxies: Proxy[], apiKeys: APIKey[]): Proxy | null {
  const newestFirst = [...proxies].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""));
  return newestFirst.find((proxy) => gatewayAppKeys(proxy, apiKeys).length > 0) ?? newestFirst[0] ?? null;
}

export function shouldShowGetStarted(state: GetStartedState, dismissed: boolean) {
  return !dismissed && state.current !== null;
}

export type FirstRequestSnippets = {
  curl: string;
  sdk: string;
  sdkLabel: string;
};

export function firstRequestSnippets(proxy: Proxy, origin: string): FirstRequestSnippets {
  const connection = gatewayConnection(proxy, origin);
  return {
    curl: connection.curl,
    sdk: connection.python,
    sdkLabel:
      proxy.target_provider === "anthropic"
        ? "Anthropic SDK"
        : proxy.target_provider === "typesafe"
          ? "Python"
          : "OpenAI SDK",
  };
}

/** Fill the `$BASTIO_API_KEY` placeholder with a freshly created secret. */
export function withApiKey(snippet: string, secret: string) {
  return snippet.split("$BASTIO_API_KEY").join(secret);
}

export const GET_STARTED_DISMISS_KEY = "bastio:get-started:dismissed";

type StorageLike = Pick<Storage, "getItem" | "setItem">;

export function readDismissed(storage: () => StorageLike | undefined): boolean {
  try {
    return storage()?.getItem(GET_STARTED_DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeDismissed(storage: () => StorageLike | undefined) {
  try {
    storage()?.setItem(GET_STARTED_DISMISS_KEY, "1");
  } catch {
    // Private mode or blocked storage: dismissal lasts for this page view only.
  }
}
