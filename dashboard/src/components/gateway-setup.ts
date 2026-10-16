import type { APIKey, ProviderKey, Proxy } from "../api/client";

export const providerLabels: Record<string, string> = {
  openai: "OpenAI",
  openrouter: "OpenRouter",
  typesafe: "TypeSafe / Jev",
  anthropic: "Anthropic / Claude",
  xai: "Grok / xAI",
  gemini: "Google Gemini",
  deepseek: "DeepSeek",
  groq: "Groq",
  ollama: "Ollama (self-hosted)",
  bedrock: "Amazon Bedrock",
};

export function availableProviderKeys(proxy: Proxy, keys: ProviderKey[]) {
  return keys.filter(
    (key) =>
      key.provider === proxy.target_provider &&
      (key.proxy_id === proxy.id || (!key.proxy_id && key.is_default)),
  );
}

export function gatewayAppKeys(proxy: Proxy, keys: APIKey[]) {
  return keys.filter(
    (key) => key.is_active && (key.scopes ?? []).includes(`proxy:${proxy.id}`),
  );
}

export function gatewaySetupStatus(
  proxy: Proxy,
  keys?: ProviderKey[],
  appKeys?: APIKey[],
  managedProviders: string[] = [],
) {
  if (!proxy.is_active) return "Disabled";
  if (!keys || !appKeys) return "Checking setup";
  if (
    !availableProviderKeys(proxy, keys).length &&
    !managedProviders.includes(proxy.target_provider) &&
    !["ollama", "bedrock"].includes(proxy.target_provider)
  )
    return "Add provider key";
  if (!gatewayAppKeys(proxy, appKeys).length) return "Create app key";
  if (
    !availableProviderKeys(proxy, keys).length &&
    !managedProviders.includes(proxy.target_provider)
  )
    return "Check server setup";
  return "Setup complete";
}

export function gatewayBasePath(proxy: Proxy) {
  return ["openai", "openrouter", "anthropic", "xai", "typesafe"].includes(
    proxy.target_provider,
  )
    ? `/v1/guard/${encodeURIComponent(proxy.id)}/v1`
    : "/v1";
}

export function gatewayConnection(proxy: Proxy, origin: string) {
  const baseURL = `${origin.replace(/\/$/, "")}${gatewayBasePath(proxy)}`;
  if (proxy.target_provider === "typesafe") {
    const payload = {model: proxy.target_model || "jev-latest", state: "Hello from Bastio", questions: {greeting:{type:"noul",instructions:"Does the state contain a greeting?"}}};
    return {baseURL,sdkBaseURL:baseURL,
      curl: `curl '${baseURL}/systemone' -H "Authorization: Bearer $BASTIO_API_KEY" -H "Content-Type: application/json" -d '${JSON.stringify(payload)}'`,
      python: `import os, requests\nresponse = requests.post(${JSON.stringify(baseURL+"/systemone")}, headers={"Authorization": "Bearer " + os.environ["BASTIO_API_KEY"]}, json=${JSON.stringify(payload)})\nresponse.raise_for_status()\nprint(response.json())`,
    };
  }
  const anthropic = proxy.target_provider === "anthropic";
  const native = gatewayBasePath(proxy) !== "/v1";
  const model = proxy.target_model || "YOUR_MODEL_ID";
  const payload = JSON.stringify(
    {
      model,
      ...(anthropic ? { max_tokens: 256 } : {}),
      messages: [{ role: "user", content: "Hello" }],
    },
    null,
    2,
  );
  const shellQuote = (value: string) => `'${value.replace(/'/g, `'"'"'`)}'`;
  const routingHeaders = native
    ? []
    : [
        `  -H ${shellQuote(`X-Bastio-Proxy-Id: ${proxy.id}`)}`,
        `  -H ${shellQuote(`X-Bastio-Provider: ${proxy.target_provider}`)}`,
      ];
  const curl = [
    `curl ${shellQuote(`${baseURL}/${anthropic ? "messages" : "chat/completions"}`)}`,
    anthropic
      ? '  -H "x-api-key: $BASTIO_API_KEY"'
      : '  -H "Authorization: Bearer $BASTIO_API_KEY"',
    ...(anthropic ? ['  -H "anthropic-version: 2023-06-01"'] : []),
    ...routingHeaders,
    '  -H "Content-Type: application/json"',
    `  -d ${shellQuote(payload)}`,
  ].join(" \\\n");
  const python = anthropic
    ? [
        "import os",
        "from anthropic import Anthropic",
        "",
        "client = Anthropic(",
        '    api_key=os.environ["BASTIO_API_KEY"],',
        `    base_url=${JSON.stringify(baseURL.replace(/\/v1$/, ""))},`,
        ")",
        "",
        "response = client.messages.create(",
        `    model=${JSON.stringify(model)},`,
        "    max_tokens=256,",
        '    messages=[{"role": "user", "content": "Hello"}],',
        ")",
        "print(response)",
      ].join("\n")
    : [
        "import os",
        "from openai import OpenAI",
        "",
        "client = OpenAI(",
        '    api_key=os.environ["BASTIO_API_KEY"],',
        `    base_url=${JSON.stringify(baseURL)},`,
        ...(native
          ? []
          : [
              `    default_headers=${JSON.stringify({ "X-Bastio-Proxy-Id": proxy.id, "X-Bastio-Provider": proxy.target_provider })},`,
            ]),
        ")",
        "",
        "response = client.chat.completions.create(",
        `    model=${JSON.stringify(model)},`,
        '    messages=[{"role": "user", "content": "Hello"}],',
        ")",
        "print(response)",
      ].join("\n");
  return {
    baseURL,
    sdkBaseURL: anthropic ? baseURL.replace(/\/v1$/, "") : baseURL,
    curl,
    python,
  };
}

export type GatewaySearch = { gateway?: string; tab?: "providers" };
export function validateGatewaySearch(
  search: Record<string, unknown>,
): GatewaySearch {
  return {
    ...(typeof search.gateway === "string" && search.gateway
      ? { gateway: search.gateway }
      : {}),
    ...(search.tab === "providers" ? { tab: "providers" as const } : {}),
  };
}
