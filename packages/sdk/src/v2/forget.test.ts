import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, forgetCarriesOf, refusalOf } from "../index.js";
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

const ids = {
  memory: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  space: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  tenant: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
  receipt: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71",
  person: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72",
  connection: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a73",
  target: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a74",
};

const tombstone: V2.Tombstone = {
  id: ids.memory,
  op_id: ids.memory,
  ref: "M-0219",
  kind: "memory",
  object_id: ids.memory,
  space_id: ids.space,
  tenant_id: ids.tenant,
  with: ["M-0220"],
  by: { kind: "person", id: ids.person },
  via: "web",
  receipt_id: ids.receipt,
  forgotten_at: "2026-10-07T09:30:00Z",
  reads_before: 41,
  gone: {
    versions: 2,
    sources: 1,
    embeddings: 2,
    verdicts: 1,
    model_verdicts: 0,
    gates: 0,
    files: 2,
  },
  status: "propagating",
  steps: [
    { kind: "asked", status: "done", at: "2026-10-07T09:30:00Z" },
    { kind: "removed", status: "done", count: 7 },
    {
      kind: "target",
      status: "waiting",
      reason: "delivery",
      target: {
        id: ids.target,
        kind: "agents_md",
        label: "AGENTS.md",
        delivery: "local",
      },
      compile: "C-0013",
    },
    {
      kind: "agent",
      status: "waiting",
      reason: "next_read",
      read_it: true,
      agent: { connection_id: ids.connection, agent: "codex" },
    },
  ],
  unreachable: [
    {
      kind: "git_history",
      files: ["AGENTS.md"],
      repositories: ["MemaxLabs/memax"],
    },
    { kind: "agent_memory", agents: ["Codex"] },
    { kind: "backups", days: 7 },
    {
      kind: "llm",
      processors: [
        { name: "Voyage AI", purpose: "embeddings", zero_retention: false },
      ],
    },
  ],
};

const memory = {
  id: ids.memory,
  ref: "M-0219",
  space_id: ids.space,
  tenant_id: ids.tenant,
  kind: "fact",
  section: "conventions",
  statement: "",
  lifecycle: "forgotten",
  flags: [],
  state: "forgotten",
  trust: "high",
  version: 1,
} as unknown as V2.Memory;

describe("memax.v2 Forget", () => {
  it("previews a Forget without changing anything", async () => {
    const preview: V2.ForgetPreview = {
      ref: "M-0219",
      version: 3,
      carries: [],
      files: [
        {
          id: ids.target,
          kind: "agents_md",
          label: "AGENTS.md",
          delivery: "local",
        },
      ],
      agents: 5,
      readers: 4,
      allowed: true,
    };
    const { memax, call } = client(jsonResponse({ data: preview }));
    await expect(
      memax.v2.memories.previewForget("M-0219", { space: "memax-v2" }),
    ).resolves.toEqual(preview);
    expect(call().url).toBe(
      "https://api.memax.app/v2/memories/M-0219/forget-preview?space=memax-v2",
    );
    expect(call().method).toBe("GET");
  });

  it("forgets with If-Match, the carried memories and the space", async () => {
    const result: V2.ForgetResult = {
      outcome: "applied",
      policy: { effect: "apply" },
      memory,
      receipts: [],
      tombstone,
      memories: [],
    };
    const { memax, call } = client(jsonResponse({ data: result }));

    await expect(
      memax.v2.memories.forget(
        "M-0219",
        { carries: ["M-0220"], note: "a personal detail" },
        {
          space: "memax-v2",
          ifMatch: 3,
          idempotencyKey: "forget-1",
          via: "web",
        },
      ),
    ).resolves.toEqual(result);
    expect(call().url).toBe(
      "https://api.memax.app/v2/memories/M-0219:forget?space=memax-v2",
    );
    expect(call().method).toBe("POST");
    expect(call().headers).toMatchObject({
      "Idempotency-Key": "forget-1",
      "If-Match": '"3"',
      "X-Memax-Via": "web",
    });
    expect(call().body).toEqual({
      carries: ["M-0220"],
      note: "a personal detail",
    });
  });

  it("reads forget_carries off the 409, and nothing else", async () => {
    const carries: V2.ForgetCarry[] = [
      {
        id: ids.memory,
        ref: "M-0220",
        reason: "cites",
        with: "M-0219",
        lifecycle: "kept",
        kind: "fact",
      },
    ];
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "forget_carries",
            message: "Forgetting M-0219 forgets M-0220 too.",
            details: { ref: "M-0219", carries },
          },
        },
        { status: 409 },
      ),
    );
    const err = await memax.v2.memories
      .forget(
        "M-0219",
        {},
        { space: "memax-v2", ifMatch: 1, idempotencyKey: "k" },
      )
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(forgetCarriesOf(err)).toEqual(carries);
    expect(refusalOf(err)).toBeUndefined();
    expect(forgetCarriesOf(new Error("x"))).toBeUndefined();
  });

  it("refuses an agent with the policy code", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message: "API keys can't forget. Forget it on the web.",
            details: {
              policy: { effect: "refuse", code: "key_cannot_forget" },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await memax.v2.memories
      .forget(
        "M-0219",
        {},
        { space: "memax-v2", ifMatch: 1, idempotencyKey: "k" },
      )
      .catch((e: unknown) => e);
    expect(refusalOf(err)?.code).toBe("key_cannot_forget");
    expect(forgetCarriesOf(err)).toBeUndefined();
  });

  it("asks a person to forget, and keeps it instead", async () => {
    const request: V2.ForgetRequestResult = {
      outcome: "applied",
      policy: { effect: "apply" },
      memory,
      receipts: [],
      forget_request: {
        id: ids.receipt,
        memory_id: ids.memory,
        ref: "M-0219",
        space_id: ids.space,
        agent: { connection_id: ids.connection, agent: "codex" },
        reason: "it was a test value",
        status: "waiting",
        receipt_id: ids.receipt,
        requested_at: "2026-10-07T09:30:00Z",
      },
    };
    const { memax, call } = client(jsonResponse({ data: request }));
    await expect(
      memax.v2.memories.requestForget(
        "M-0219",
        { reason: "it was a test value" },
        { space: "memax-v2", idempotencyKey: "ask-1" },
      ),
    ).resolves.toEqual(request);
    expect(call().url).toBe(
      "https://api.memax.app/v2/memories/M-0219:request-forget?space=memax-v2",
    );
    expect(call().headers["If-Match"]).toBeUndefined();
    expect(call().body).toEqual({ reason: "it was a test value" });

    const kept = client(
      jsonResponse({
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          memory,
          receipts: [],
        },
      }),
    );
    await kept.memax.v2.memories.declineForget(
      ids.memory,
      {},
      { idempotencyKey: "keep-1" },
    );
    expect(kept.call().url).toBe(
      `https://api.memax.app/v2/memories/${ids.memory}:decline-forget`,
    );
    expect(kept.call().headers["Idempotency-Key"]).toBe("keep-1");
  });

  it("reads a tombstone and a space's tombstones", async () => {
    const one = client(jsonResponse({ data: tombstone }));
    await expect(
      one.memax.v2.memories.tombstone("M-0219", { space: "memax-v2" }),
    ).resolves.toEqual(tombstone);
    expect(one.call().url).toBe(
      "https://api.memax.app/v2/memories/M-0219/tombstone?space=memax-v2",
    );
    expect(one.call().method).toBe("GET");

    const page: V2.TombstonePage = {
      tombstones: [{ ...tombstone, steps: [], unreachable: [] }],
      has_more: true,
      next_cursor: "t1791335730573966_0199a1b2",
    };
    const list = client(jsonResponse({ data: page }));
    await expect(
      list.memax.v2.memories.tombstones("memax-v2", {
        limit: 1,
        cursor: "t1",
      }),
    ).resolves.toEqual(page);
    expect(list.call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/tombstones?cursor=t1&limit=1",
    );
  });

  it("lists an agent's notices and acknowledges them", async () => {
    const notices: V2.NoticeList = {
      notices: [
        {
          id: ids.receipt,
          space_id: ids.space,
          op_id: ids.memory,
          kind: "forgotten",
          refs: ["M-0219"],
          read_it: true,
          at: "2026-10-07T09:30:00Z",
          space: "memax-v2",
        },
      ],
    };
    const list = client(jsonResponse({ data: notices }));
    await expect(list.memax.v2.notices.list()).resolves.toEqual(notices);
    expect(list.call().url).toBe("https://api.memax.app/v2/notices");

    const ack = client(jsonResponse({ data: { acknowledged: 1 } }));
    await expect(
      ack.memax.v2.notices.ack(
        { ids: [ids.receipt] },
        { idempotencyKey: "ack-1" },
      ),
    ).resolves.toEqual({ acknowledged: 1 });
    expect(ack.call().url).toBe("https://api.memax.app/v2/notices:ack");
    expect(ack.call().method).toBe("POST");
    expect(ack.call().body).toEqual({ ids: [ids.receipt] });
  });
});
