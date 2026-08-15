import {
  BastioBlockedError,
  BastioClient,
  type BastioClientOptions,
  type DetectMessage,
  type DetectResponse,
  type DetectStep,
} from "@bastio/core";
import {
  bastioMiddleware,
  type BastioMiddlewareOptions,
} from "./middleware.js";

/**
 * Configuration options for the Bastio Vercel AI SDK provider.
 */
export interface BastioProviderOptions extends Omit<BastioClientOptions, "baseURL"> {
  /**
   * Base URL for the Bastio gateway or security API.
   * Defaults to process.env.BASTIO_URL or "http://localhost:4000".
   */
  baseURL?: string;
  /**
   * Dashboard session token or proxy API key.
   * Defaults to process.env.BASTIO_API_KEY or process.env.BASTIO_KEY.
   */
  apiKey?: string;
  /**
   * Named Bastio security profile (e.g. "default", "strict", "production-guard").
   */
  profile?: string;
  /**
   * Alias for profile (e.g. 'strict-pii-masking').
   */
  securityProfile?: string;
  /**
   * Inline step list; overrides the profile when provided.
   */
  steps?: DetectStep[];
  /**
   * Called once per request with the input decision and once with the
   * output decision.
   */
  onDecision?: (
    stage: "input" | "output",
    result: DetectResponse,
  ) => void;
  /**
   * When false, skip the output scan. Defaults to true.
   */
  scanOutput?: boolean;
}

/**
 * Generic shape representing a Vercel AI SDK LanguageModel (V1, V2, or V3).
 */
export interface LanguageModelLike {
  specificationVersion?: string;
  provider?: string;
  modelId?: string;
  defaultObjectGenerationMode?: string;
  supportsImageUrls?: boolean;
  supportsStructuredOutputs?: boolean;
  doGenerate?: (options: any) => Promise<any>;
  doStream?: (options: any) => Promise<any>;
  [key: string]: unknown;
}

/**
 * The Bastio Provider interface returned by createBastio / createBastioProvider.
 */
export interface BastioProvider {
  /**
   * Wrap an existing LanguageModel instance with Bastio guardrails.
   *
   * @example
   * ```ts
   * const guardedModel = bastio(openai('gpt-4o'));
   * ```
   */
  <T extends LanguageModelLike>(model: T): T;
  (modelId: string): LanguageModelLike;

  /**
   * Wrap an existing LanguageModel instance with Bastio guardrails.
   */
  wrap<T extends LanguageModelLike>(model: T): T;

  /**
   * Wrap or create a language model.
   */
  languageModel<T extends LanguageModelLike>(model: T | string): T | LanguageModelLike;

  /**
   * Wrap or create a chat model.
   */
  chat<T extends LanguageModelLike>(model: T | string): T | LanguageModelLike;

  /**
   * Pre-configured Bastio middleware instance for use with wrapLanguageModel.
   */
  readonly middleware: unknown;

  /**
   * Underlying BastioClient instance.
   */
  readonly client: BastioClient;

  /**
   * Resolved provider options.
   */
  readonly options: BastioProviderOptions;
}

function resolveEnv(name: string): string | undefined {
  const globalProcess = (globalThis as unknown as { process?: { env?: Record<string, string | undefined> } }).process;
  if (globalProcess && globalProcess.env) {
    return globalProcess.env[name];
  }
  return undefined;
}

/**
 * Helper to extract text from various AI SDK prompt/message content shapes.
 */
function extractTextContent(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const part of content) {
    if (typeof part === "string") {
      parts.push(part);
    } else if (part && typeof part === "object") {
      const p = part as { type?: string; text?: string };
      if (p.type === "text" && typeof p.text === "string") {
        parts.push(p.text);
      }
    }
  }
  return parts.join("\n");
}

/**
 * Convert prompt messages to Bastio DetectMessage shape.
 */
function extractDetectMessages(prompt: unknown): DetectMessage[] {
  if (!Array.isArray(prompt)) return [];
  const out: DetectMessage[] = [];
  for (const m of prompt) {
    if (m && typeof m === "object") {
      const msg = m as { role?: string; content?: unknown };
      const role = typeof msg.role === "string" ? msg.role : "user";
      const content = extractTextContent(msg.content);
      if (content.length > 0) {
        out.push({ role, content });
      }
    }
  }
  return out;
}

/**
 * Rewrite text content in a prompt message.
 */
function rewriteMessageContent(m: any, replacement: string): any {
  if (typeof m.content === "string") {
    return { ...m, content: replacement };
  }
  if (!Array.isArray(m.content)) {
    return m;
  }
  let replaced = false;
  const newContent = m.content.map((part: any) => {
    if (replaced) return part;
    if (typeof part === "string") {
      replaced = true;
      return replacement;
    }
    if (part && typeof part === "object" && part.type === "text") {
      replaced = true;
      return { ...part, text: replacement };
    }
    return part;
  });
  return { ...m, content: newContent };
}

/**
 * Apply sanitized messages back onto the original prompt structure.
 */
function applySanitizedPrompt(originalPrompt: any[], result: DetectResponse): any[] {
  const queue = [...result.messages];
  return originalPrompt.map((m) => {
    if (!m || typeof m !== "object") return m;
    if (extractTextContent(m.content).length === 0) return m;
    const slot = queue.shift();
    if (!slot) return m;
    if (slot.sanitized_content === slot.original) return m;
    return rewriteMessageContent(m, slot.sanitized_content);
  });
}

/**
 * Wraps a LanguageModel instance with Bastio inline threat scanning,
 * prompt injection detection, PII masking, and output safety checks.
 */
export function wrapModel<T extends LanguageModelLike>(
  model: T,
  client: BastioClient,
  opts: BastioProviderOptions,
): T {
  const scanOutput = opts.scanOutput !== false;
  const profile = opts.profile ?? opts.securityProfile;

  const wrapped: LanguageModelLike = {
    ...model,
    specificationVersion: model.specificationVersion ?? "v1",
    provider: model.provider ? `bastio(${model.provider})` : "bastio",
    modelId: model.modelId ?? "unknown",

    doGenerate: async (callOptions: any) => {
      let sanitizedOptions = callOptions;

      // 1. Scan and sanitize input prompt messages
      if (callOptions?.prompt && Array.isArray(callOptions.prompt)) {
        const detectMessages = extractDetectMessages(callOptions.prompt);
        if (detectMessages.length > 0) {
          const inputResult = await client.detect({
            messages: detectMessages,
            direction: "input",
            profile,
            steps: opts.steps,
          });

          opts.onDecision?.("input", inputResult);

          if (inputResult.should_block) {
            throw new BastioBlockedError(inputResult);
          }

          sanitizedOptions = {
            ...callOptions,
            prompt: applySanitizedPrompt(callOptions.prompt, inputResult),
          };
        }
      }

      // 2. Call underlying model's doGenerate
      if (typeof model.doGenerate !== "function") {
        throw new Error(
          `Bastio: wrapped model '${model.modelId || "unknown"}' does not implement doGenerate`,
        );
      }
      const generateResult = await model.doGenerate(sanitizedOptions);

      // 3. Scan and sanitize model output
      if (scanOutput && generateResult) {
        const outputText =
          typeof generateResult.text === "string"
            ? generateResult.text
            : typeof generateResult.content === "string"
              ? generateResult.content
              : "";

        if (outputText.length > 0) {
          const outputResult = await client.detect({
            messages: [{ role: "assistant", content: outputText }],
            direction: "output",
            profile,
            steps: opts.steps,
          });

          opts.onDecision?.("output", outputResult);

          if (outputResult.should_block) {
            throw new BastioBlockedError(outputResult);
          }

          const sanitized = outputResult.messages[0]?.sanitized_content;
          if (typeof sanitized === "string" && sanitized !== outputText) {
            if (typeof generateResult.text === "string") {
              return { ...generateResult, text: sanitized };
            }
            if (typeof generateResult.content === "string") {
              return { ...generateResult, content: sanitized };
            }
          }
        }
      }

      return generateResult;
    },

    doStream: async (callOptions: any) => {
      let sanitizedOptions = callOptions;

      // 1. Scan and sanitize input prompt messages
      if (callOptions?.prompt && Array.isArray(callOptions.prompt)) {
        const detectMessages = extractDetectMessages(callOptions.prompt);
        if (detectMessages.length > 0) {
          const inputResult = await client.detect({
            messages: detectMessages,
            direction: "input",
            profile,
            steps: opts.steps,
          });

          opts.onDecision?.("input", inputResult);

          if (inputResult.should_block) {
            throw new BastioBlockedError(inputResult);
          }

          sanitizedOptions = {
            ...callOptions,
            prompt: applySanitizedPrompt(callOptions.prompt, inputResult),
          };
        }
      }

      // 2. Call underlying model's doStream
      if (typeof model.doStream !== "function") {
        throw new Error(
          `Bastio: wrapped model '${model.modelId || "unknown"}' does not implement doStream`,
        );
      }

      return model.doStream(sanitizedOptions);
    },
  };

  return wrapped as T;
}

/**
 * Creates a Bastio security provider for the Vercel AI SDK.
 *
 * @example
 * ```ts
 * import { createBastio } from '@bastio/vercel-ai';
 * import { openai } from '@ai-sdk/openai';
 * import { generateText } from 'ai';
 *
 * const bastio = createBastio({
 *   apiKey: process.env.BASTIO_API_KEY,
 *   profile: 'production-guard',
 * });
 *
 * const { text } = await generateText({
 *   model: bastio(openai('gpt-4o')),
 *   prompt: 'Summarize customer feedback',
 * });
 * ```
 */
export function createBastio(options: BastioProviderOptions = {}): BastioProvider {
  const baseURL =
    options.baseURL ??
    resolveEnv("BASTIO_URL") ??
    resolveEnv("BASTIO_BASE_URL") ??
    "http://localhost:4000";

  const apiKey =
    options.apiKey ??
    resolveEnv("BASTIO_API_KEY") ??
    resolveEnv("BASTIO_KEY");

  const resolvedOpts: BastioProviderOptions = {
    ...options,
    baseURL,
    apiKey,
    profile: options.profile ?? options.securityProfile,
  };

  const client = new BastioClient({
    baseURL: resolvedOpts.baseURL!,
    apiKey: resolvedOpts.apiKey,
    fetch: resolvedOpts.fetch,
    timeoutMs: resolvedOpts.timeoutMs,
    headers: resolvedOpts.headers,
  });

  const middleware = bastioMiddleware({
    baseURL: resolvedOpts.baseURL!,
    apiKey: resolvedOpts.apiKey,
    profile: resolvedOpts.profile,
    steps: resolvedOpts.steps,
    onDecision: resolvedOpts.onDecision,
    scanOutput: resolvedOpts.scanOutput,
    fetch: resolvedOpts.fetch,
    timeoutMs: resolvedOpts.timeoutMs,
    headers: resolvedOpts.headers,
  });

  function providerFunction(modelOrId: LanguageModelLike | string): LanguageModelLike {
    if (typeof modelOrId === "string") {
      // Create a dummy model descriptor pointing to the Bastio gateway
      return wrapModel(
        {
          specificationVersion: "v1",
          provider: "bastio",
          modelId: modelOrId,
          doGenerate: async () => {
            throw new Error(
              `Direct string model ID '${modelOrId}' requires wrapping a provider model e.g. bastio(openai('${modelOrId}')) or configuring gateway proxy.`,
            );
          },
          doStream: async () => {
            throw new Error(
              `Direct string model ID '${modelOrId}' requires wrapping a provider model e.g. bastio(openai('${modelOrId}')).`,
            );
          },
        },
        client,
        resolvedOpts,
      );
    }
    return wrapModel(modelOrId, client, resolvedOpts);
  }

  const provider = Object.assign(providerFunction, {
    wrap: <T extends LanguageModelLike>(model: T): T => wrapModel(model, client, resolvedOpts),
    languageModel: (model: LanguageModelLike | string) => providerFunction(model),
    chat: (model: LanguageModelLike | string) => providerFunction(model),
    middleware,
    client,
    options: resolvedOpts,
  });

  return provider as unknown as BastioProvider;
}

/**
 * Alias for `createBastio`.
 */
export const createBastioProvider = createBastio;
