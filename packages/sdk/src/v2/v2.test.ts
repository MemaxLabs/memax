import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, refusalOf } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(response: Response | (() => Response)) {
  const fetchMock = vi.fn(async () =>
    typeof response === "function" ? response() : response.clone(),
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
  return { memax, fetchMock, call };
}

const memory: V2.Memory = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
  ref: "M-0219",
  space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  tenant_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  statement: "River is our queue, not Kafka.",
  section: "decisions",
  kind: "fact",
  state: "kept",
  lifecycle: "kept",
  flags: [],
  trust: "person",
  version: 3,
  conditions: [],
  scope: {},
  created_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5e",
  last_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5e",
  created_at: "2026-10-06T09:30:00Z",
  updated_at: "2026-10-06T09:30:00Z",
};

const result: V2.CommandResult = {
  outcome: "applied",
  policy: { effect: "apply" },
  memory,
  receipts: [],
};

describe("memax.v2.memories", () => {
  it("remembers with an Idempotency-Key and unwraps the envelope", async () => {
    const { memax, call } = client(
      jsonResponse({ data: result }, { status: 201 }),
    );
    const input: V2.RememberInput = {
      statement: "River is our queue, not Kafka.",
      section: "decisions",
      sources: [{ kind: "pr", ref: "PR #212", external: false }],
    };

    const got = await memax.v2.memories.remember("memax-v2", input, {
      idempotencyKey: "key-1",
      via: "cli",
    });

    expect(got).toEqual(result);
    const c = call();
    expect(c.url).toBe("https://api.memax.app/v2/spaces/memax-v2/memories");
    expect(c.method).toBe("POST");
    expect(c.headers).toMatchObject({
      Authorization: "Bearer mxk_test",
      "Idempotency-Key": "key-1",
      "X-Memax-Via": "cli",
    });
    expect(c.headers["If-Match"]).toBeUndefined();
    expect(c.body).toEqual(input);
  });

  it("lists with repeated filters and a cursor", async () => {
    const page: V2.MemoryPage = { items: [memory], has_more: false };
    const { memax, call } = client(jsonResponse({ data: page }));

    await expect(
      memax.v2.memories.list("memax v2", {
        state: ["kept", "stale"],
        section: "decisions",
        cursor: "bTEw",
        limit: 10,
      }),
    ).resolves.toEqual(page);
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax%20v2/memories?cursor=bTEw&limit=10&state=kept&state=stale&section=decisions",
    );
    expect(call().method).toBe("GET");
  });

  it("reads a display ID with its space, and a uuid without one", async () => {
    const detail: V2.MemoryDetail = {
      memory,
      versions: [],
      receipts: { items: [], has_more: false },
    };
    const { memax, call } = client(jsonResponse({ data: detail }));

    await memax.v2.memories.get("M-0219", { space: "memax-v2" });
    await memax.v2.memories.get(memory.id);

    expect(call(0).url).toBe(
      "https://api.memax.app/v2/memories/M-0219?space=memax-v2",
    );
    expect(call(1).url).toBe(`https://api.memax.app/v2/memories/${memory.id}`);
  });

  it("keeps and rejects with If-Match as a quoted ETag", async () => {
    const { memax, call } = client(jsonResponse({ data: result }));

    await memax.v2.memories.keep(
      "M-0219",
      {},
      { space: "memax-v2", idempotencyKey: "k", ifMatch: 3 },
    );
    await memax.v2.memories.reject(
      "M-0220",
      { reason: "duplicate of M-0156" },
      { space: "memax-v2", idempotencyKey: "r" },
    );

    expect(call(0).url).toBe(
      "https://api.memax.app/v2/memories/M-0219:keep?space=memax-v2",
    );
    expect(call(0).headers).toMatchObject({
      "Idempotency-Key": "k",
      "If-Match": '"3"',
    });
    expect(call(0).body).toEqual({});
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/memories/M-0220:reject?space=memax-v2",
    );
    expect(call(1).headers["If-Match"]).toBeUndefined();
    expect(call(1).body).toEqual({ reason: "duplicate of M-0156" });
  });

  it("edits with the version it started from", async () => {
    const { memax, call } = client(jsonResponse({ data: result }));

    await memax.v2.memories.edit(
      memory.id,
      { statement: "River is our only queue.", keep: true },
      { idempotencyKey: "e", ifMatch: 3 },
    );

    const c = call();
    expect(c.url).toBe(`https://api.memax.app/v2/memories/${memory.id}:edit`);
    expect(c.headers["If-Match"]).toBe('"3"');
    expect(c.body).toEqual({
      statement: "River is our only queue.",
      keep: true,
    });
  });

  it("reuses the Idempotency-Key when the transport retries", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          { error: { code: "not_ready", message: "warming up" } },
          { status: 503 },
        ),
      )
      .mockResolvedValueOnce(jsonResponse({ data: result }, { status: 201 }));
    const memax = new Memax({
      fetch: fetchMock,
      maxRetries: 1,
      retryDelayMs: 0,
    });

    await memax.v2.memories.remember(
      "memax-v2",
      { statement: "x", section: "decisions" },
      { idempotencyKey: "same" },
    );

    const keys = fetchMock.mock.calls.map(
      (args) =>
        (args[1] as { headers: Record<string, string> }).headers[
          "Idempotency-Key"
        ],
    );
    expect(keys).toEqual(["same", "same"]);
  });
});

describe("/v2 errors", () => {
  it("turns a refusal into a MemaxError carrying the policy decision", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message:
              "API keys can propose but never keep or reject. Review it on the web.",
            details: {
              policy: {
                effect: "refuse",
                code: "key_cannot_review",
                message:
                  "API keys can propose but never keep or reject. Review it on the web.",
              },
            },
          },
        },
        { status: 403 },
      ),
    );

    const err = await memax.v2.memories
      .keep("M-0219", {}, { space: "memax-v2", idempotencyKey: "k" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(MemaxError);
    expect((err as MemaxError).isForbidden).toBe(true);
    expect((err as MemaxError).code).toBe("refused");
    const policy = refusalOf(err);
    expect(policy?.code).toBe("key_cannot_review");
    expect(policy?.effect).toBe("refuse");
  });

  it("returns no refusal for other errors", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "edit_clash",
            message: "M-0219 changed since you opened it.",
            details: { ref: "M-0219", expected_version: 2, current_version: 3 },
          },
        },
        { status: 412 },
      ),
    );

    const err = await memax.v2.memories
      .edit(
        "M-0219",
        { statement: "y" },
        { space: "memax-v2", idempotencyKey: "e", ifMatch: 2 },
      )
      .catch((e: unknown) => e);

    expect((err as MemaxError).status).toBe(412);
    expect((err as MemaxError).code).toBe("edit_clash");
    expect((err as MemaxError).details?.current_version).toBe(3);
    expect(refusalOf(err)).toBeUndefined();
    expect(refusalOf(new Error("boom"))).toBeUndefined();
  });

  it("keeps Retry-After on busy", async () => {
    const { memax } = client(
      () =>
        new Response(
          JSON.stringify({
            error: { code: "busy", message: "Try again in a moment." },
          }),
          {
            status: 503,
            headers: { "Content-Type": "application/json", "Retry-After": "1" },
          },
        ),
    );

    const err = await memax.v2.memories
      .reject("M-0219", {}, { space: "memax-v2", idempotencyKey: "r" })
      .catch((e: unknown) => e);

    expect((err as MemaxError).code).toBe("busy");
    expect((err as MemaxError).retryAfterSeconds).toBe(1);
  });
});

describe("memax.v2 lists", () => {
  it("lists spaces, the Review queue and receipts", async () => {
    const { memax, call } = client(
      () =>
        new Response(
          JSON.stringify({ data: { items: [], has_more: false, total: 0 } }),
          { headers: { "Content-Type": "application/json" } },
        ),
    );

    await memax.v2.spaces.list();
    const review = await memax.v2.review.list("memax-v2", { limit: 20 });
    await memax.v2.receipts.list("memax-v2", {
      memory: "M-0219",
      cursor: "cjEw",
    });

    expect(review.total).toBe(0);
    expect(call(0).url).toBe("https://api.memax.app/v2/spaces");
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/review?limit=20",
    );
    expect(call(2).url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/receipts?cursor=cjEw&memory=M-0219",
    );
  });
});
