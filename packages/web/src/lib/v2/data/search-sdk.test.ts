import { describe, expect, it, vi } from "vitest";
import type { V2 } from "memax-sdk";
import { createDemoSource } from "./demo-source";
import { createSdkSource } from "./sdk-source";

// Search through the SDK source (memax.v2.search) and the demo's.

const receipt = (fields: Partial<V2.Receipt>): V2.Receipt => ({
  id: "r1",
  seq: 1,
  tenant_id: "t1",
  space_id: "s1",
  object_kind: "memory",
  object_id: "m1",
  object_ref: "M-0431",
  action: "proposed",
  actor_kind: "agent",
  agent: "codex",
  via: "mcp",
  occurred_at: "2026-10-05T21:26:00Z",
  recorded_at: "2026-10-05T21:26:00Z",
  stream_id: "m1",
  stream_version: 1,
  ...fields,
});

const memory = (fields: Partial<V2.Memory>): V2.Memory => ({
  id: "m1",
  ref: "M-0431",
  space_id: "s1",
  tenant_id: "t1",
  statement: "Deploy the v2 API to Fly.io in iad and ams.",
  section: "decisions",
  kind: "fact",
  state: "kept",
  lifecycle: "kept",
  flags: [],
  trust: "agent_own_work",
  version: 1,
  conditions: [],
  scope: {},
  created_receipt_id: "r1",
  last_receipt_id: "r2",
  created_at: "2026-10-05T21:26:00Z",
  updated_at: "2026-10-05T21:30:00Z",
  ...fields,
});

function setup(viewerId = "u1") {
  const client = {
    v2: {
      search: vi.fn().mockResolvedValue({
        items: [
          {
            memory: memory({}),
            receipts: [
              receipt({}),
              receipt({
                id: "r2",
                action: "kept",
                actor_kind: "person",
                actor_id: viewerId,
                agent: undefined,
                via: "web",
                occurred_at: "2026-10-05T21:30:00Z",
              }),
            ],
          },
          {
            memory: memory({
              id: "m2",
              ref: "M-0318",
              kind: "decision",
              decision: { status: "open" },
              created_receipt_id: "r3",
              last_receipt_id: "r3",
            }),
            receipts: [receipt({ id: "r3", object_id: "m2" })],
          },
        ],
        has_more: true,
        notes: [
          {
            id: "n1",
            ref: "N-0871",
            space_id: "s1",
            owner_id: viewerId,
            origin: "memory",
            title: "Fly deploy",
            excerpt: "Fly deploy failed on the health check.",
            author_kind: "person",
            state: "active",
            disposition: "note",
            length: 40,
            created_at: "2026-07-10T17:00:00Z",
            updated_at: "2026-07-10T17:00:00Z",
          },
        ],
        lexical_only: false,
      } satisfies V2.SearchPage),
    },
  };
  const source = createSdkSource({
    client: client as never,
    viewer: { id: viewerId, initials: "ZZ", name: "Ziyang", timeZone: "UTC" },
  });
  return { client, source };
}

describe("search", () => {
  it("asks for the first 50 with notes, and reads each hit's receipts", async () => {
    const { client, source } = setup();
    const got = await source.search.search({ query: "deploy" });
    expect(client.v2.search).toHaveBeenCalledWith("deploy", {
      includeNotes: true,
      limit: 50,
      signal: undefined,
    });
    expect(got.more).toBe(true);
    expect(got.lexicalOnly).toBe(false);
    const [fact, decision] = got.hits;
    // Written by Codex, kept by you: the rail shows the latest.
    expect(fact.author).toEqual({ kind: "agent", agent: "codex" });
    expect(fact.item.receipt).toEqual({
      by: { kind: "person", self: true },
      action: "kept",
      at: "2026-10-05T21:30:00Z",
    });
    expect(fact).toMatchObject({ spaceId: "s1", kind: "fact", decision: null });
    expect(decision).toMatchObject({ kind: "decision", decision: "open" });
    expect(got.notes).toEqual([
      {
        ref: "N-0871",
        spaceId: "s1",
        title: "Fly deploy",
        excerpt: "Fly deploy failed on the health check.",
        at: "2026-07-10T17:00:00Z",
      },
    ]);
  });
});

describe("the demo's search", () => {
  it("finds the board's rows by their words", async () => {
    const demo = createDemoSource({ commandDelayMs: 0 });
    const got = await demo.search.search({ query: "deploy" });
    expect(got.hits.slice(0, 4).map((h) => h.item.ref)).toEqual([
      "M-0431",
      "M-0174",
      "M-0083",
      "M-0052",
    ]);
    expect(got.notes).toHaveLength(9);
    expect(
      (await demo.search.search({ query: "river" })).hits[0].item.ref,
    ).toBe("M-0219");
    expect((await demo.search.search({ query: "kubernetes" })).hits).toEqual(
      [],
    );
  });
});
