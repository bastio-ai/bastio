import type { Proxy } from "../../api/client";

export const helloPrompt =
  "Say hello in one short, cheerful sentence. Include a tiny robot celebrating a successful connection.";

export function buildGatewayTestRequest(
  proxy: Proxy,
  model: string,
  origin: string,
) {
  return {
    url: `${origin.replace(/\/$/, "")}/v1/proxies/${encodeURIComponent(proxy.id)}/test`,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    },
    body: JSON.stringify({ model: model.trim() }),
  };
}

type JSONRecord = Record<string, unknown>;
function record(value: unknown): JSONRecord {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as JSONRecord)
    : {};
}
function textParts(value: unknown): string {
  if (typeof value === "string") return value;
  if (!Array.isArray(value)) return "";
  return value
    .map((part) =>
      typeof record(part).text === "string"
        ? (record(part).text as string)
        : "",
    )
    .join("");
}

export function gatewayTestReply(body: unknown): string {
  const data = record(body);
  const choice = record(
    Array.isArray(data.choices) ? data.choices[0] : undefined,
  );
  const candidate = record(
    Array.isArray(data.candidates) ? data.candidates[0] : undefined,
  );
  return (
    textParts(record(choice.message).content) ||
    textParts(data.content) ||
    textParts(record(record(data.answers).greeting).noul) ||
    textParts(record(candidate.content).parts) ||
    textParts(record(data.message).content) ||
    textParts(record(record(data.output).message).content)
  );
}

export function gatewayTestFailure(status: number, body: unknown) {
  const data = record(body);
  const error = record(data.error);
  const detail =
    typeof error.message === "string"
      ? error.message
      : typeof data.message === "string"
        ? data.message
        : typeof data.error === "string"
          ? data.error
          : typeof data.reason === "string"
            ? data.reason
            : `The gateway returned HTTP ${status}.`;
  const code =
    typeof error.type === "string"
      ? error.type
      : typeof data.error === "string"
        ? data.error
        : "";
  if (
    code.includes("security") ||
    code.includes("blocked") ||
    data.blocked === true
  )
    return { title: "Security policy blocked the test", detail };
  if (
    error.code === "model_not_found" ||
    /does not have access to model|model.*(?:not found|does not exist|not available|not permitted|no access)|access.*model/i.test(
      detail,
    )
  )
    return {
      title: "Your provider key cannot use this model",
      detail: `${detail} Choose another model. This is a provider project/model access restriction; creating a new Bastio API key will not change it.`,
    };
  if (status === 401)
    return {
      title: "Authentication failed",
      detail: `${detail} Check your dashboard session and the provider key configured for this gateway.`,
    };
  if (status === 402 || status === 429)
    return { title: "A balance or usage limit stopped the test", detail };
  if (code === "feature_not_in_tier")
    return { title: "Your plan does not include this feature", detail };
  if (status === 403) return { title: "Access was denied", detail };
  return { title: "The gateway test did not complete", detail };
}

export function gatewayTraceId(header: string | null, body: unknown) {
  const candidate = header || record(record(body).bastio).trace_id;
  return typeof candidate === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      candidate,
    )
    ? candidate
    : undefined;
}
