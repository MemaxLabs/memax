import { beforeEach, describe, expect, it, vi } from "vitest";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

// A fake memax client: one V1 team hub and two spaces on V2, one of which
// this agent is connected to (at an autonomy each test sets).
const state = {
  apiKey: true,
  autonomy: "propose",
  remembered: [] as unknown[],
  pushed: [] as unknown[],
  compiled: false,
  asked: [] as unknown[],
  gateStatus: "waiting",
  forgetRequests: [] as unknown[],
  notices: [] as { id: string; [k: string]: unknown }[],
  acked: [] as string[],
};

const V1_HUB = "11111111-1111-4111-8111-111111111111";
const V2_CONNECTED = "22222222-2222-4222-8222-222222222222";
const V2_OTHER = "33333333-3333-4333-8333-333333333333";

const space = (id: string, slug: string) => ({
  id,
  tenant_id: id,
  slug,
  name: slug,
  kind: "project",
  role: "owner",
  v2_enabled_at: "2026-10-01T00:00:00Z",
});

const kept = (ref: string, statement: string, section = "conventions") => ({
  id: `0190${ref.replace("M-", "").padStart(4, "0")}-0000-7000-8000-000000000000`,
  ref,
  space_id: V2_CONNECTED,
  statement,
  section,
  kind: section === "decisions" ? "decision" : "fact",
  state: "kept",
  lifecycle: "kept",
  trust: "person",
  version: 1,
  created_at: "2026-10-01T00:00:00Z",
});

const GATE_ID = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a60";

const gate = () => ({
  id: GATE_ID,
  ref: "G-0012",
  space_id: V2_CONNECTED,
  question: "Which deploy target should the v2 API use?",
  options: [{ label: "Fly.io" }, { label: "Railway" }],
  status: state.gateStatus,
  expires_at: "2026-10-13T09:30:00Z",
  needs_web: false,
  version: state.gateStatus === "waiting" ? 1 : 2,
  ...(state.gateStatus === "answered"
    ? {
        answer: {
          option: 2,
          label: "Railway",
          memory: { id: "m-0432", ref: "M-0432" },
        },
      }
    : {}),
});

const fakeClient = {
  hubs: {
    list: vi.fn(async () => [
      {
        hub: { id: V1_HUB, name: "v1-team", slug: "v1-team", hub_type: "team" },
        role: "owner",
        memory_count: 3,
      },
      {
        hub: {
          id: V2_CONNECTED,
          name: "memax-v2",
          slug: "memax-v2",
          hub_type: "team",
        },
        role: "owner",
        memory_count: 9,
      },
      {
        hub: { id: V2_OTHER, name: "other", slug: "other", hub_type: "team" },
        role: "owner",
        memory_count: 4,
      },
    ]),
  },
  push: vi.fn(async (content: string, opts: unknown) => {
    state.pushed.push({ content, opts });
    return {
      id: "44444444-4444-4444-8444-444444444444",
      title: "T",
      hub_id: V1_HUB,
    };
  }),
  recall: vi.fn(async () => ({
    memories: [
      {
        id: "m1",
        title: "V1 lighthouse",
        chunk_content: "lighthouse in v1",
        heading_chain: "",
        relevance_score: 0.9,
        kind: "semantic",
        stability: "evolving",
        source: "mcp",
        age: "1d",
        hub_id: V1_HUB,
      },
      {
        id: "m2",
        title: "A note",
        chunk_content: "lighthouse note in a v2 space",
        heading_chain: "",
        relevance_score: 0.8,
        kind: "semantic",
        stability: "evolving",
        source: "mcp",
        age: "1d",
        hub_id: V2_CONNECTED,
      },
    ],
  })),
  v2: {
    spaces: {
      list: vi.fn(async () => ({
        items: [
          { ...space(V1_HUB, "v1-team"), v2_enabled_at: undefined },
          space(V2_CONNECTED, "memax-v2"),
          space(V2_OTHER, "other"),
        ],
      })),
    },
    agents: {
      list: vi.fn(async () => ({
        items: [
          {
            id: "conn",
            state: "active",
            spaces: [
              {
                space_id: V2_CONNECTED,
                slug: "memax-v2",
                name: "memax-v2",
                kind: "project",
                autonomy: state.autonomy,
              },
            ],
          },
        ],
      })),
    },
    memories: {
      remember: vi.fn(
        async (
          spaceId: string,
          input: { statement: string },
          opts: unknown,
        ) => {
          state.remembered.push({ spaceId, input, opts });
          return {
            outcome: "proposed",
            policy: { effect: "propose", code: "autonomy_propose" },
            memory: {
              ...kept("M-0431", input.statement),
              state: "proposed",
              lifecycle: "proposed",
            },
            receipts: [],
          };
        },
      ),
      list: vi.fn(async (spaceId: string, opts?: { state?: unknown }) => ({
        items:
          spaceId === V2_CONNECTED && opts?.state !== "proposed"
            ? [
                kept("M-0219", "Lighthouse lamps are checked every morning"),
                kept("M-0220", "We chose Postgres", "decisions"),
              ]
            : [],
        has_more: false,
      })),
      get: vi.fn(),
      requestForget: vi.fn(
        async (ref: string, input: unknown, opts: unknown) => {
          state.forgetRequests.push({ ref, input, opts });
          return {
            outcome: "applied",
            policy: { effect: "apply" },
            memory: kept(
              "M-0219",
              "Lighthouse lamps are checked every morning",
            ),
            receipts: [],
            forget_request: { status: "waiting", ref: "M-0219" },
          };
        },
      ),
    },
    notices: {
      list: vi.fn(async () => ({
        notices: state.notices.filter((n) => !state.acked.includes(n.id)),
      })),
      ack: vi.fn(async (input: { ids: string[] }) => {
        state.acked.push(...input.ids);
        return { acknowledged: input.ids.length };
      }),
    },
    review: {
      list: vi.fn(async () => ({ items: [], has_more: false, total: 2 })),
    },
    gates: {
      request: vi.fn(async (spaceId: string, input: unknown, opts: unknown) => {
        state.asked.push({ spaceId, input, opts });
        return {
          outcome: "applied",
          policy: { effect: "apply" },
          gate: gate(),
          receipts: [],
        };
      }),
      get: vi.fn(async () => gate()),
    },
    targets: {
      list: vi.fn(async (spaceId: string) => ({
        items:
          state.compiled && spaceId === V2_CONNECTED
            ? [
                {
                  id: "target-1",
                  kind: "agents_md",
                  label: "AGENTS.md",
                  delivery: "local",
                  sync_state: "in_sync",
                },
              ]
            : [],
      })),
      preview: vi.fn(async () => ({
        target: {},
        compile: { ref: "C-0881", compiled_at: "2026-10-06T14:31:00Z" },
        files: [
          {
            path: "AGENTS.md",
            content: "# memax-v2\n- We chose Postgres [M-0220]",
          },
        ],
        copies: [],
      })),
    },
  },
};

vi.mock("../lib/client.js", () => ({
  getClient: () => fakeClient,
  setClientAgent: () => {},
  usesAPIKey: () => state.apiKey,
}));
vi.mock("../lib/config.js", () => ({
  getActiveHubID: () => undefined,
  loadConfig: () => ({ api_url: "https://api.memax.app" }),
}));

const { createMcpServerForTest } = await import("./mcp.js");
const { MCP_INSTRUCTIONS, servedTools } = await import("./mcp-tools.js");
const { rankLocally, resetV2StateForTest } = await import("./mcp-v2.js");
const { resetGatesForTest } = await import("./mcp-v2-gates.js");

async function connect(): Promise<Client> {
  const server = createMcpServerForTest("claude-code");
  const [a, b] = InMemoryTransport.createLinkedPair();
  await server.connect(a);
  const client = new Client({ name: "test", version: "1" });
  await client.connect(b);
  return client;
}

function textOf(res: { content?: unknown }): string {
  return ((res.content ?? []) as { text?: string }[])
    .map((c) => c.text ?? "")
    .join("\n");
}

beforeEach(() => {
  state.apiKey = true;
  state.autonomy = "propose";
  state.remembered = [];
  state.pushed = [];
  state.compiled = false;
  state.asked = [];
  state.gateStatus = "waiting";
  state.forgetRequests = [];
  state.notices = [];
  state.acked = [];
  fakeClient.v2.memories.get.mockReset();
  resetV2StateForTest();
  resetGatesForTest();
});

describe("the stdio MCP server", () => {
  it("serves the shared catalogue and instructions", async () => {
    const client = await connect();
    const { tools } = await client.listTools();
    expect(tools.map((t) => t.name)).toEqual(servedTools().map((t) => t.name));
    const recall = tools.find((t) => t.name === "memax_recall");
    expect(recall?.annotations).toMatchObject({
      readOnlyHint: true,
      openWorldHint: false,
    });
    expect(recall?.outputSchema).toEqual(
      servedTools().find((t) => t.name === "memax_recall")?.outputSchema,
    );
    expect(client.getInstructions()).toBe(MCP_INSTRUCTIONS);
    expect(client.getServerVersion()?.version).not.toBe("0.0.1");
  });

  it("keeps V1 pushes on V1 hubs", async () => {
    const client = await connect();
    const res = await client.callTool({
      name: "memax_push",
      arguments: { content: "V1 push", hub_id: "v1-team" },
    });
    expect(textOf(res)).toContain("Saved: T");
    expect(res.structuredContent).toMatchObject({ status: "saved" });
    expect(state.pushed).toHaveLength(1);
    expect(state.remembered).toHaveLength(0);
  });

  it("proposes through memax.v2 in a space on V2, via mcp", async () => {
    const client = await connect();
    const res = await client.callTool({
      name: "memax_push",
      arguments: {
        content: "Deploys go out on Fridays",
        hub_id: "memax-v2",
        section: "decisions",
      },
    });
    expect(res.isError).toBeFalsy();
    expect(res.structuredContent).toMatchObject({
      status: "proposed",
      id: "M-0431",
      review_url: "https://memax.app/memax-v2/review?ref=M-0431",
    });
    const call = state.remembered[0] as {
      spaceId: string;
      input: Record<string, unknown>;
      opts: Record<string, unknown>;
    };
    expect(call.spaceId).toBe(V2_CONNECTED);
    expect(call.input).toMatchObject({
      statement: "Deploys go out on Fridays",
      section: "decisions",
      kind: "decision",
    });
    expect(call.opts.via).toBe("mcp");
    expect(String(call.opts.idempotencyKey)).toMatch(/^mcp-push:/);
    expect(state.pushed).toHaveLength(0);
  });

  it("refuses a Read agent's write and an unconnected space", async () => {
    state.autonomy = "read";
    const client = await connect();
    let res = await client.callTool({
      name: "memax_push",
      arguments: { content: "x", hub_id: "memax-v2" },
    });
    expect(res.isError).toBe(true);
    expect(textOf(res)).toContain(
      "read-only in memax-v2. Change it in Agents.",
    );
    res = await client.callTool({
      name: "memax_push",
      arguments: { content: "x", hub_id: "other" },
    });
    expect(res.isError).toBe(true);
    expect(textOf(res)).toContain("isn't connected to other");
    expect(state.remembered).toHaveLength(0);
  });

  it("recalls kept memories on V2 and V1 results without V2 notes", async () => {
    const client = await connect();
    const res = await client.callTool({
      name: "memax_recall",
      arguments: { query: "lighthouse" },
    });
    const text = textOf(res);
    expect(text).toContain("M-0219");
    expect(text).toContain("lighthouse in v1");
    expect(text).not.toContain("note in a v2 space");
    const out = res.structuredContent as { results: { record: string }[] };
    expect(out.results.map((r) => r.record)).toEqual(["v2", "v1"]);
  });

  it("returns a digest without a query", async () => {
    const client = await connect();
    const res = await client.callTool({ name: "memax_recall", arguments: {} });
    expect(textOf(res)).toContain("## memax-v2");
    expect(textOf(res)).toContain("2 waiting in Review");
    expect(textOf(res)).toContain("### Decisions");
  });

  it("serves the latest compile as the digest once the space has one", async () => {
    state.compiled = true;
    const client = await connect();
    const res = await client.callTool({ name: "memax_recall", arguments: {} });
    expect(textOf(res)).toContain("Compiled C-0881 · AGENTS.md");
    expect(textOf(res)).toContain("We chose Postgres [M-0220]");
    expect(textOf(res)).not.toContain("### Decisions");
    const out = res.structuredContent as {
      digest: { compiled?: { ref: string }; sections: unknown[] }[];
    };
    expect(out.digest[0].compiled?.ref).toBe("C-0881");
    expect(out.digest[0].sections).toEqual([]);
  });

  it("asks a decision gate through memax.v2 in a space on V2, via mcp", async () => {
    const client = await connect();
    const res = await client.callTool({
      name: "memax_request_decision",
      arguments: {
        question: "Which deploy target should the v2 API use?",
        options: ["Fly.io", " Railway ", ""],
        context: "M-0174 and M-0431 disagree.",
        space_id: "memax-v2",
      },
    });
    expect(res.isError).toBeFalsy();
    expect(res.structuredContent).toMatchObject({
      id: "G-0012",
      space_id: V2_CONNECTED,
      status: "waiting",
      url: "https://memax.app/memax-v2/review?ref=G-0012",
    });
    expect(textOf(res)).toContain("Asked in memax-v2 as G-0012.");
    expect(textOf(res)).toContain("next memax_recall");
    const call = state.asked[0] as {
      spaceId: string;
      input: Record<string, unknown>;
      opts: Record<string, unknown>;
    };
    expect(call.spaceId).toBe(V2_CONNECTED);
    expect(call.input).toEqual({
      question: "Which deploy target should the v2 API use?",
      options: [{ label: "Fly.io" }, { label: "Railway" }],
      context: "M-0174 and M-0431 disagree.",
    });
    expect(call.opts.via).toBe("mcp");
    expect(String(call.opts.idempotencyKey)).toMatch(/^mcp-gate:/);
  });

  it("tells recall how this session's gates ended, once", async () => {
    const client = await connect();
    await client.callTool({
      name: "memax_request_decision",
      arguments: {
        question: "Which deploy target should the v2 API use?",
        options: ["Fly.io", "Railway"],
        space_id: "memax-v2",
      },
    });
    let res = await client.callTool({ name: "memax_recall", arguments: {} });
    expect(res.structuredContent).toMatchObject({
      gates: [{ id: "G-0012", status: "waiting" }],
    });
    expect(textOf(res)).toContain("G-0012 (memax-v2) is still waiting");
    state.gateStatus = "answered";
    res = await client.callTool({
      name: "memax_recall",
      arguments: { query: "deploy" },
    });
    expect(res.structuredContent).toMatchObject({
      gates: [
        {
          id: "G-0012",
          status: "answered",
          option: 2,
          answer: "Railway",
          memory: "M-0432",
        },
      ],
    });
    expect(textOf(res)).toContain(
      "G-0012 (memax-v2) answered: Railway. Kept as M-0432.",
    );
    res = await client.callTool({ name: "memax_recall", arguments: {} });
    expect(
      (res.structuredContent as { gates?: unknown[] }).gates,
    ).toBeUndefined();
  });

  it("refuses a Read agent's question", async () => {
    state.autonomy = "read";
    const client = await connect();
    const res = await client.callTool({
      name: "memax_request_decision",
      arguments: {
        question: "Which way?",
        options: ["a", "b"],
        space_id: "memax-v2",
      },
    });
    expect(res.isError).toBe(true);
    expect(textOf(res)).toContain("read-only in memax-v2");
    expect(state.asked).toHaveLength(0);
  });

  it("says a write the judge returned to Review isn't kept", async () => {
    const memory = {
      ...kept("M-0433", "Preview builds run on Fly.io machines."),
      state: "conflict",
      lifecycle: "proposed",
      flags: ["conflict"],
    };
    fakeClient.v2.memories.get.mockResolvedValueOnce({
      memory,
      receipts: {
        items: [
          {
            id: "r2",
            seq: 2,
            action: "returned",
            actor_kind: "memax",
            via: "system",
            source: { kind: "memory", ref: "M-0156" },
            occurred_at: "2026-10-06T14:31:00Z",
          },
          {
            id: "r1",
            seq: 1,
            action: "kept",
            actor_kind: "agent",
            agent: "codex",
            via: "mcp",
            occurred_at: "2026-10-06T14:30:00Z",
          },
        ],
        has_more: false,
      },
    });
    const client = await connect();
    const res = await client.callTool({
      name: "memax_get",
      arguments: { id: "M-0433", space_id: "memax-v2" },
    });
    expect(res.isError).toBe(true);
    expect(textOf(res)).toBe(
      "M-0433 is back in Review in memax-v2: it was kept at once, then Memax found it contradicts M-0156, a decision in force. It isn't kept now, so don't act on it; a person keeps it or settles the conflict.",
    );
  });

  it("never forgets in a space on V2: an agent asks a person", async () => {
    const memory = kept("M-0219", "Lighthouse lamps are checked every morning");
    fakeClient.v2.memories.get.mockResolvedValue({ memory });
    const client = await connect();
    const res = await client.callTool({
      name: "memax_forget",
      arguments: {
        id: "M-0219",
        space_id: "memax-v2",
        reason: "it was a test value",
      },
    });
    expect(textOf(res)).toContain(
      "M-0219 wasn't forgotten yet: an agent can't forget. It now waits for a person to forget it on the web: https://memax.app/memax-v2/memories/M-0219",
    );
    expect(state.forgetRequests).toHaveLength(1);
    const req = state.forgetRequests[0] as {
      ref: string;
      input: unknown;
      opts: { idempotencyKey: string; via: string };
    };
    expect(req.ref).toBe(memory.id);
    expect(req.input).toEqual({ reason: "it was a test value" });
    expect(req.opts.via).toBe("mcp");
    expect(req.opts.idempotencyKey).toMatch(/^mcp-forget:/);
    // Asking again the same day for the same reason is the same request.
    await client.callTool({
      name: "memax_forget",
      arguments: {
        id: "M-0219",
        space_id: "memax-v2",
        reason: "it was a test value",
      },
    });
    const again = state.forgetRequests[1] as typeof req;
    expect(again.opts.idempotencyKey).toBe(req.opts.idempotencyKey);
  });

  it("points a person's own session to the web to forget", async () => {
    state.apiKey = false;
    fakeClient.v2.memories.get.mockResolvedValue({
      memory: kept("M-0219", "Lighthouse lamps are checked every morning"),
    });
    const client = await connect();
    const res = await client.callTool({
      name: "memax_forget",
      arguments: { id: "M-0219", space_id: "memax-v2" },
    });
    expect(textOf(res)).toContain(
      "M-0219 wasn't forgotten: forget it yourself on the web at https://memax.app/memax-v2/memories/M-0219",
    );
    expect(state.forgetRequests).toHaveLength(0);
  });

  it("tells the agent once what was forgotten, on its next response", async () => {
    state.notices = [
      {
        id: "n1",
        space_id: V2_CONNECTED,
        op_id: "op1",
        kind: "forgotten",
        refs: ["M-0219"],
        read_it: true,
        at: "2026-10-07T09:30:00Z",
      },
    ];
    const client = await connect();
    const res = await client.callTool({
      name: "memax_recall",
      arguments: { query: "lighthouse" },
    });
    const told =
      "Forgotten in memax-v2: M-0219. Drop anything you took from it, including what you saved in your own memory; it is gone from Memax and every compiled file.";
    expect(textOf(res)).toContain(told);
    expect(res._meta?.["app.memax/notices"]).toEqual([
      {
        kind: "forgotten",
        space_id: V2_CONNECTED,
        space: "memax-v2",
        refs: ["M-0219"],
        message: told,
      },
    ]);
    expect((res.structuredContent as { notices?: unknown[] }).notices).toEqual([
      {
        kind: "forgotten",
        space_id: V2_CONNECTED,
        refs: ["M-0219"],
        message: told,
      },
    ]);
    expect(state.acked).toEqual(["n1"]);
    // Told once: the next response, whatever the tool, doesn't repeat it.
    const next = await client.callTool({ name: "memax_hubs", arguments: {} });
    expect(textOf(next)).not.toContain("Forgotten in");
    expect(next._meta?.["app.memax/notices"]).toBeUndefined();
  });

  it("tells a person's session nothing (notices are agents')", async () => {
    state.apiKey = false;
    state.notices = [
      {
        id: "n1",
        space_id: V2_CONNECTED,
        op_id: "op1",
        kind: "space_forgotten",
        refs: [],
        read_it: false,
        at: "2026-10-07T09:30:00Z",
      },
    ];
    const client = await connect();
    const before = fakeClient.v2.notices.list.mock.calls.length;
    const res = await client.callTool({ name: "memax_hubs", arguments: {} });
    expect(textOf(res)).not.toContain("was forgotten");
    expect(fakeClient.v2.notices.list.mock.calls.length).toBe(before);
  });
});

describe("rankLocally", () => {
  it("scores by shared words and prefixes", () => {
    const scores = rankLocally("deploy target", [
      { statement: "Deploys go to the sjc target" },
      { statement: "Nothing related" },
      { statement: "deploy target is fly" },
    ]);
    expect(scores[2]).toBe(1);
    expect(scores[0]).toBeGreaterThan(0);
    expect(scores[1]).toBe(0);
  });
});
