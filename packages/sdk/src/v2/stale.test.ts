import { describe, expect, it, vi } from "vitest";
import { Memax } from "../index.js";
import type { V2 } from "../index.js";

function client(body: unknown) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify({ data: body }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  );
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "mxk_test",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (i = 0) => {
    const [url, init] = fetchMock.mock.calls[i] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body)),
    };
  };
  return { memax, call };
}

const ids = {
  memory: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  space: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
};

describe("memax.v2.memories stale and verify", () => {
  it("reads why a memory is stale", async () => {
    const report: V2.StaleReport = {
      memory_id: ids.memory,
      ref: "M-0187",
      space_id: ids.space,
      version: 1,
      statement: "Ask memax answers with the Haiku tier.",
      kind: "fact",
      state: "stale",
      stale: true,
      stale_after: "2026-10-01T00:00:00Z",
      reads_since: 88,
      causes: [{ kind: "date", stale_after: "2026-10-01T00:00:00Z" }],
    };
    const { memax, call } = client(report);
    const got = await memax.v2.memories.stale("M-0187", { space: "memax-v2" });
    expect(got.causes[0]?.kind).toBe("date");
    expect(call().method).toBe("GET");
    expect(call().url).toBe(
      "https://api.memax.app/v2/memories/M-0187/stale?space=memax-v2",
    );
  });

  it("answers a stale flag with If-Match and an idempotency key", async () => {
    const { memax, call } = client({});
    await memax.v2.memories.verify(
      "M-0187",
      {
        answer: "update",
        statement: "Ask memax answers with the Sonnet tier.",
      },
      { space: "memax-v2", ifMatch: 1, idempotencyKey: "v-1" },
    );
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe(
      "https://api.memax.app/v2/memories/M-0187:verify?space=memax-v2",
    );
    expect(c.headers["If-Match"]).toBe('"1"');
    expect(c.headers["Idempotency-Key"]).toBe("v-1");
    expect(c.body).toEqual({
      answer: "update",
      statement: "Ask memax answers with the Sonnet tier.",
    });
  });
});
