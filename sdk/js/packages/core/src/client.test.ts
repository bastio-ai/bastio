import { describe, expect, it } from "vitest";
import { BastioClient } from "./client.js";
import { BastioError } from "./errors.js";
import type { DetectResponse } from "./types.js";

function fakeFetch(
  body: unknown,
  init: { status?: number } = {},
): typeof fetch {
  return (async () =>
    new Response(JSON.stringify(body), {
      status: init.status ?? 200,
      headers: { "Content-Type": "application/json" },
    })) as unknown as typeof fetch;
}

const okResponse: DetectResponse = {
  profile: "default",
  direction: "input",
  action: "pass",
  should_block: false,
  messages: [
    {
      role: "user",
      original: "hi",
      sanitized_content: "hi",
      action: "pass",
      should_block: false,
      steps: [],
    },
  ],
};

describe("BastioClient", () => {
  it("sends Authorization header when apiKey is set", async () => {
    let sawAuth = "";
    const fetchSpy: typeof fetch = (async (
      _url: unknown,
      init: RequestInit | undefined,
    ) => {
      sawAuth = String(new Headers(init?.headers).get("authorization") ?? "");
      return new Response(JSON.stringify(okResponse), { status: 200 });
    }) as unknown as typeof fetch;

    const c = new BastioClient({
      baseURL: "https://example.test",
      apiKey: "sk-test",
      fetch: fetchSpy,
    });
    await c.detect({ messages: [{ role: "user", content: "hi" }] });
    expect(sawAuth).toBe("Bearer sk-test");
  });

  it("parses a successful detect response", async () => {
    const c = new BastioClient({
      baseURL: "https://example.test/",
      fetch: fakeFetch(okResponse),
    });
    const r = await c.detect({ messages: [{ role: "user", content: "hi" }] });
    expect(r.action).toBe("pass");
  });

  it("throws BastioError on non-2xx", async () => {
    const c = new BastioClient({
      baseURL: "https://example.test",
      fetch: fakeFetch({ error: "bad" }, { status: 500 }),
    });
    await expect(
      c.detect({ messages: [{ role: "user", content: "x" }] }),
    ).rejects.toBeInstanceOf(BastioError);
  });
});
