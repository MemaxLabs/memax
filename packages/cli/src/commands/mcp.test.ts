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
    },
    review: {
      list: vi.fn(async () => ({ items: [], has_more: false, total: 2 })),
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
  resetV2StateForTest();
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

  it("never forgets in a space on V2", async () => {
    const client = await connect();
    const res = await client.callTool({
      name: "memax_forget",
      arguments: { id: "M-0219", space_id: "memax-v2" },
    });
    expect(textOf(res)).toContain("wasn't forgotten");
    expect(textOf(res)).toContain("https://memax.app/memax-v2/memories/M-0219");
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
