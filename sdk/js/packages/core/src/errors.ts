import type { DetectResponse } from "./types.js";

/**
 * Generic transport/HTTP error from the Bastio API. Thrown when the
 * call couldn't produce a DetectResponse — network failure, non-2xx
 * status, invalid JSON.
 */
export class BastioError extends Error {
  readonly status?: number;
  readonly body?: unknown;
  constructor(message: string, opts?: { status?: number; body?: unknown }) {
    super(message);
    this.name = "BastioError";
    this.status = opts?.status;
    this.body = opts?.body;
  }
}

/**
 * Thrown by the framework adapters (Mastra, Vercel AI) when Bastio's
 * detection pipeline chose to block. Carries the full DetectResponse
 * so callers can render the decision in their UI or logs without
 * re-calling the API.
 */
export class BastioBlockedError extends Error {
  readonly result: DetectResponse;
  constructor(result: DetectResponse) {
    super(summarize(result));
    this.name = "BastioBlockedError";
    this.result = result;
  }
}

function summarize(r: DetectResponse): string {
  const firstFired = r.messages
    .flatMap((m) => m.steps)
    .find((s) => s.fired && s.action === "block");
  if (!firstFired) return "Bastio blocked the request";
  return `Bastio blocked the request: ${firstFired.detector} (${firstFired.strategy})`;
}
