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

function client(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
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

const read: V2.Read = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  ref: "R-5512",
  space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  reader_kind: "agent",
  connection_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a61",
  person_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  agent: "claude-code",
  kind: "compile_load",
  via: "cli",
  session_ref: "7c2f",
  compile: "C-0874",
  memories: 12,
  memory_refs: [],
  read_at: "2026-10-05T14:02:00Z",
  recorded_at: "2026-10-05T14:02:01Z",
};

describe("memax.v2.receipts.checkpoints", () => {
  it("lists the sealed chain with its keys", async () => {
    const page: V2.CheckpointPage = {
      items: [],
      has_more: false,
      seal: { sealed_receipts: 1284, checkpoints: 40, unsealed: 2 },
      keys: [
        {
          key_id: "ed25519:fe812c12f3ab4ce6",
          algorithm: "ed25519",
          public_key: "AAAA",
        },
      ],
    };
    const { memax, call } = client(jsonResponse({ data: page }));
    const got = await memax.v2.receipts.checkpoints("memax-v2", { limit: 5 });
    expect(got.seal.sealed_receipts).toBe(1284);
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/checkpoints?limit=5",
    );
  });
});

describe("memax.v2.reads", () => {
  it("lists a space's reads, with the week's count", async () => {
    const page: V2.ReadPage = { items: [read], has_more: false, reads_7d: 41 };
    const { memax, call } = client(jsonResponse({ data: page }));
    const got = await memax.v2.reads.list("memax-v2", { limit: 20 });
    expect(got.reads_7d).toBe(41);
    expect(got.items[0].ref).toBe("R-5512");
    const c = call();
    expect(c.method).toBe("GET");
    expect(c.url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/reads?limit=20",
    );
  });

  it("reports a compile load with its key", async () => {
    const res: V2.CompileLoadResult = { read };
    const { memax, call } = client(
      jsonResponse({ data: res }, { status: 201 }),
    );
    const got = await memax.v2.reads.recordCompileLoad(
      "memax-v2",
      { compile: "C-0874", agent: "claude-code", session_ref: "7c2f" },
      { idempotencyKey: "load-7c2f", via: "cli" },
    );
    expect(got.read.memories).toBe(12);
    const c = call();
    expect(c.method).toBe("POST");
    expect(c.url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/compile-loads",
    );
    expect(c.headers["Idempotency-Key"]).toBe("load-7c2f");
    expect(c.headers["X-Memax-Via"]).toBe("cli");
    expect(c.body).toEqual({
      compile: "C-0874",
      agent: "claude-code",
      session_ref: "7c2f",
    });
  });

  it("surfaces a space the agent isn't connected to as a refusal", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message:
              "Codex isn't connected to elsewhere, so it can't read it. Connect it in Agents.",
            details: {
              policy: { effect: "refuse", code: "agent_not_connected" },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await memax.v2.reads
      .recordCompileLoad(
        "elsewhere",
        { compile: "C-0874" },
        { idempotencyKey: "k" },
      )
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(refusalOf(err)?.code).toBe("agent_not_connected");
  });
});
