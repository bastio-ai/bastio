import { BastioError } from "./errors.js";
import type {
  BastioClientOptions,
  DetectRequest,
  DetectResponse,
} from "./types.js";

/**
 * Thin HTTP wrapper around POST /v1/detect. Framework adapters
 * (@bastio/mastra, @bastio/vercel-ai) compose this rather than re-implementing
 * the wire format.
 */
export class BastioClient {
  private readonly baseURL: string;
  private readonly apiKey?: string;
  private readonly fetchImpl: typeof fetch;
  private readonly timeoutMs: number;
  private readonly extraHeaders: Record<string, string>;

  constructor(opts: BastioClientOptions) {
    if (!opts.baseURL) {
      throw new BastioError("BastioClient: baseURL is required");
    }
    // Strip trailing slashes via a loop instead of /\/+$/ — the regex
    // form trips CodeQL's polynomial-redos rule. A while loop is O(n)
    // and equally clear at the call site.
    let base = opts.baseURL;
    while (base.endsWith("/")) base = base.slice(0, -1);
    this.baseURL = base;
    this.apiKey = opts.apiKey;
    this.fetchImpl = opts.fetch ?? globalThis.fetch;
    this.timeoutMs = opts.timeoutMs ?? 10_000;
    this.extraHeaders = opts.headers ?? {};

    if (typeof this.fetchImpl !== "function") {
      throw new BastioError(
        "BastioClient: no global fetch available — pass opts.fetch",
      );
    }
  }

  /**
   * Run the configured profile (or an inline step list) against messages.
   * Returns the raw DetectResponse; callers that want block-throws-error
   * semantics should use the framework adapters instead.
   */
  async detect(req: DetectRequest): Promise<DetectResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const res = await this.fetchImpl(`${this.baseURL}/v1/detect`, {
        method: "POST",
        headers: this.buildHeaders(),
        body: JSON.stringify(req),
        signal: controller.signal,
      });

      const text = await res.text();
      if (!res.ok) {
        throw new BastioError(
          `Bastio detect failed: ${res.status} ${res.statusText}`,
          { status: res.status, body: safeJSON(text) },
        );
      }
      const parsed = safeJSON(text) as DetectResponse | null;
      if (!parsed) {
        throw new BastioError("Bastio detect: invalid JSON response", {
          status: res.status,
          body: text,
        });
      }
      return parsed;
    } catch (err) {
      if (err instanceof BastioError) throw err;
      if (err instanceof Error && err.name === "AbortError") {
        throw new BastioError(
          `Bastio detect timed out after ${this.timeoutMs}ms`,
        );
      }
      throw new BastioError(
        `Bastio detect transport error: ${(err as Error).message}`,
      );
    } finally {
      clearTimeout(timer);
    }
  }

  private buildHeaders(): Record<string, string> {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      "User-Agent": "bastio-sdk-js",
      ...this.extraHeaders,
    };
    if (this.apiKey) h["Authorization"] = `Bearer ${this.apiKey}`;
    return h;
  }
}

function safeJSON(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}
