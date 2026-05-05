import {
  BastioBlockedError,
  BastioClient,
  type BastioClientOptions,
  type DetectMessage,
  type DetectResponse,
  type DetectStep,
} from "@bastio/core";

export interface BastioMiddlewareOptions extends BastioClientOptions {
  /** Named Bastio profile. Omit for the customer default. */
  profile?: string;
  /** Inline step list; overrides the profile when provided. */
  steps?: DetectStep[];
  /**
   * Called once per request with the input decision and once with the
   * output decision. Useful for logging, metrics, or side-effects
   * alongside Bastio's tracing.
   */
  onDecision?: (
    stage: "input" | "output",
    result: DetectResponse,
  ) => void;
  /**
   * When false, skip the output scan. Defaults to true — symmetry is
   * the safer default because leakage is bidirectional.
   */
  scanOutput?: boolean;
}

/**
 * Minimal shape of the Vercel AI SDK prompt entries we can scan. The
 * SDK's actual types are rich; we narrow to what matters for text
 * detection and leave everything else alone.
 */
interface ProviderPromptMessage {
  role: string;
  content: unknown;
}

type ProviderParams = {
  prompt: ProviderPromptMessage[] | unknown;
  [k: string]: unknown;
};

/**
 * Construct a Vercel AI SDK LanguageModelV2Middleware that hands every
 * call through Bastio's detect pipeline.
 *
 *   const guardedModel = wrapLanguageModel({
 *     model: openai('gpt-4'),
 *     middleware: bastioMiddleware({ baseURL, apiKey }),
 *   })
 *
 * The return type is intentionally loose (`unknown`) so this package
 * doesn't force a dependency on a specific version of the `ai` package.
 * The runtime shape matches whatever `wrapLanguageModel` accepts.
 */
export function bastioMiddleware(opts: BastioMiddlewareOptions): unknown {
  const client = new BastioClient(opts);
  const scanOutput = opts.scanOutput !== false;

  return {
    middlewareVersion: "v2" as const,

    transformParams: async (args: { params: ProviderParams }) => {
      const prompt = args.params.prompt;
      if (!Array.isArray(prompt)) return args.params;

      const detectMessages = toDetectMessages(prompt);
      if (detectMessages.length === 0) return args.params;

      const result = await client.detect({
        messages: detectMessages,
        direction: "input",
        profile: opts.profile,
        steps: opts.steps,
      });

      opts.onDecision?.("input", result);

      if (result.should_block) throw new BastioBlockedError(result);

      return {
        ...args.params,
        prompt: applySanitized(prompt, result),
      };
    },

    wrapGenerate: async (args: {
      doGenerate: () => Promise<{ text?: string; [k: string]: unknown }>;
    }) => {
      const out = await args.doGenerate();
      if (!scanOutput || typeof out.text !== "string" || out.text.length === 0) {
        return out;
      }

      const result = await client.detect({
        messages: [{ role: "assistant", content: out.text }],
        direction: "output",
        profile: opts.profile,
        steps: opts.steps,
      });

      opts.onDecision?.("output", result);

      if (result.should_block) throw new BastioBlockedError(result);

      const sanitized = result.messages[0]?.sanitized_content;
      if (typeof sanitized === "string" && sanitized !== out.text) {
        return { ...out, text: sanitized };
      }
      return out;
    },

    wrapStream: async (args: {
      doStream: () => Promise<{
        stream: ReadableStream<unknown>;
        [k: string]: unknown;
      }>;
    }) => {
      // Streaming scanning is intentionally deferred to v0.2: Bastio's
      // detectors reason on complete text and partial-token windows
      // produce high false-positive rates. For v0.1 we let the stream
      // pass through; scanOutput only affects generate().
      return args.doStream();
    },
  };
}

function toDetectMessages(prompt: ProviderPromptMessage[]): DetectMessage[] {
  const out: DetectMessage[] = [];
  for (const m of prompt) {
    const content = extractText(m.content);
    if (content.length > 0) out.push({ role: m.role, content });
  }
  return out;
}

function extractText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const part of content) {
    if (typeof part === "string") parts.push(part);
    else if (part && typeof part === "object") {
      const p = part as { type?: string; text?: string };
      if (p.type === "text" && typeof p.text === "string") parts.push(p.text);
    }
  }
  return parts.join("\n");
}

function applySanitized(
  original: ProviderPromptMessage[],
  result: DetectResponse,
): ProviderPromptMessage[] {
  const queue = [...result.messages];
  return original.map((m) => {
    if (extractText(m.content).length === 0) return m;
    const slot = queue.shift();
    if (!slot) return m;
    if (slot.sanitized_content === slot.original) return m;
    return rewriteContent(m, slot.sanitized_content);
  });
}

function rewriteContent(
  m: ProviderPromptMessage,
  replacement: string,
): ProviderPromptMessage {
  if (typeof m.content === "string") return { ...m, content: replacement };
  if (!Array.isArray(m.content)) return m;
  let replaced = false;
  const newContent = m.content.map((part) => {
    if (replaced) return part;
    if (typeof part === "string") {
      replaced = true;
      return replacement;
    }
    if (
      part &&
      typeof part === "object" &&
      (part as { type?: string }).type === "text"
    ) {
      replaced = true;
      return { ...(part as object), text: replacement };
    }
    return part;
  });
  return { ...m, content: newContent };
}
