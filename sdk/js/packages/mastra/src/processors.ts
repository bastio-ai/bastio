import {
  BastioBlockedError,
  BastioClient,
  type BastioClientOptions,
  type DetectMessage,
  type DetectResponse,
  type DetectStep,
} from "@bastio/core";

/**
 * Options accepted by both BastioInputProcessor and BastioOutputProcessor.
 *
 * `profile` names the Bastio security profile to run — omit for the
 * customer's default. `steps` overrides the profile's step list with an
 * inline pipeline, useful for tests and for per-agent specialization.
 * `onDecision` fires after every call; a place to log or emit telemetry
 * alongside Bastio's own tracing.
 */
export interface BastioProcessorOptions extends BastioClientOptions {
  profile?: string;
  steps?: DetectStep[];
  onDecision?: (result: DetectResponse) => void;
}

/**
 * Mastra-shaped minimal message type. Mastra's own CoreMessage is richer
 * (parts, tool calls, multi-modal) — we accept the subset we can safely
 * send to Bastio's text-oriented detectors and leave the rest untouched.
 */
interface MastraLikeMessage {
  role: string;
  content: unknown;
}

/**
 * BastioInputProcessor implements Mastra's inputProcessors contract:
 * receives the incoming user messages, forwards them to Bastio's
 * /v1/detect with direction="input", and:
 *
 *   - throws BastioBlockedError if any step chose `block`
 *   - rewrites the message content to the sanitized version on
 *     `mask` / `tokenize` / `redact`
 *   - returns messages unchanged on `warn` / `log_only` / `pass`
 *
 * Mastra treats a thrown error inside the processor as a failed run,
 * which is exactly what "block" should do from the agent's perspective.
 */
export class BastioInputProcessor {
  /** Stable id Mastra surfaces in its tracing. */
  readonly name = "bastio-input";
  private readonly client: BastioClient;
  private readonly opts: BastioProcessorOptions;

  constructor(opts: BastioProcessorOptions) {
    this.client = new BastioClient(opts);
    this.opts = opts;
  }

  async process(args: { messages: MastraLikeMessage[] }): Promise<{
    messages: MastraLikeMessage[];
  }> {
    const detectMessages = toDetectMessages(args.messages);
    if (detectMessages.length === 0) return { messages: args.messages };

    const result = await this.client.detect({
      messages: detectMessages,
      direction: "input",
      profile: this.opts.profile,
      steps: this.opts.steps,
    });

    this.opts.onDecision?.(result);

    if (result.should_block) throw new BastioBlockedError(result);

    return { messages: applySanitized(args.messages, result) };
  }
}

/**
 * BastioOutputProcessor mirrors BastioInputProcessor on the response
 * path: it scans model output before Mastra returns it to the caller.
 * Same strategy semantics — `block` throws, rewrites replace text.
 */
export class BastioOutputProcessor {
  readonly name = "bastio-output";
  private readonly client: BastioClient;
  private readonly opts: BastioProcessorOptions;

  constructor(opts: BastioProcessorOptions) {
    this.client = new BastioClient(opts);
    this.opts = opts;
  }

  async process(args: { messages: MastraLikeMessage[] }): Promise<{
    messages: MastraLikeMessage[];
  }> {
    const detectMessages = toDetectMessages(args.messages);
    if (detectMessages.length === 0) return { messages: args.messages };

    const result = await this.client.detect({
      messages: detectMessages,
      direction: "output",
      profile: this.opts.profile,
      steps: this.opts.steps,
    });

    this.opts.onDecision?.(result);

    if (result.should_block) throw new BastioBlockedError(result);

    return { messages: applySanitized(args.messages, result) };
  }
}

/**
 * Flatten Mastra messages to {role, content:string} for the detect
 * endpoint. Mastra's content can be a string or an array of parts; we
 * extract the text portions and concatenate them, because Bastio's
 * detectors reason over text. Non-text parts are passed through
 * unchanged in applySanitized.
 */
function toDetectMessages(messages: MastraLikeMessage[]): DetectMessage[] {
  const out: DetectMessage[] = [];
  for (const m of messages) {
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

/**
 * Map the per-message sanitized_content back onto the caller's messages.
 * We walk the original list and pair each text-bearing message with the
 * corresponding DetectMessageResult in order. Non-text messages pass
 * through unchanged so tool calls and multi-modal parts are preserved.
 */
function applySanitized(
  original: MastraLikeMessage[],
  result: DetectResponse,
): MastraLikeMessage[] {
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
  m: MastraLikeMessage,
  replacement: string,
): MastraLikeMessage {
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
