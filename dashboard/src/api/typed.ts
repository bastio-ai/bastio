import createClient from "openapi-fetch";
import type { paths } from "./schema";

const baseUrl = import.meta.env.VITE_API_URL ?? "";

// Typed HTTP client. Generated from ../cmd/server/openapi.yaml via
// `npm run generate:api`. Backend changes flow into dashboard types at
// codegen time, so out-of-sync drifts fail the build.
export const http = createClient<paths>({ baseUrl });

// unwrap lifts the { data, error } shape of openapi-fetch into a promise
// that resolves with data or throws with the error body.
export async function unwrap<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await p;
  if (error !== undefined || !response.ok) {
    const detail = typeof error === "string" ? error : JSON.stringify(error ?? {});
    throw new Error(`API error ${response.status}: ${detail}`);
  }
  return data as T;
}
