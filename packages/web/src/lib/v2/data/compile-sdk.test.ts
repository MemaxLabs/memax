import { describe, expect, it, vi } from "vitest";
import { MemaxError, type V2 } from "memax-sdk";
import { createSdkBrief, marginsToRead } from "./brief-sdk";
import type { V2Client } from "./sdk-records";
import { createSdkTargets, targetOf } from "./targets-sdk";
import type { TargetView } from "./targets";
import type { SpaceSummary } from "./types";

// The Brief and targets sources over a fake memax.v2: what they ask for
// and how they read the answers.

const SPACE: SpaceSummary = {
  id: "s1",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  kept: null,
  agents: null,
  people: null,
  waiting: null,
};

const memory = (ref: string, over: Partial<V2.Memory> = {}): V2.Memory => ({
  id: `id-${ref}`,
  ref,
  space_id: "s1",
  tenant_id: "n1",
  statement: `${ref} words.`,
  section: "decisions",
  kind: "fact",
  state: "kept",
  lifecycle: "kept",
  flags: [],
  trust: "person",
  version: 1,
  conditions: [],
  scope: {},
  created_receipt_id: `r-${ref}`,
  last_receipt_id: `r-${ref}`,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  ...over,
});

const receipt = (ref: string): V2.Receipt => ({
  id: `r-${ref}`,
  seq: 1,
  tenant_id: "n1",
  space_id: "s1",
  object_kind: "memory",
  object_id: `id-${ref}`,
  object_ref: ref,
  action: "kept",
  actor_kind: "person",
  actor_id: "u1",
  via: "web",
  occurred_at: "2026-10-02T17:58:00Z",
  recorded_at: "2026-10-02T17:58:00Z",
  stream_id: `id-${ref}`,
  stream_version: 1,
});

const BRIEF: V2.Brief = {
  id: "b1",
  version_id: "v2",
  ref: "B-0002",
  version: 2,
  parent_version: 1,
  space_id: "s1",
  tenant_id: "n1",
  title: "The Brief",
  summary: "What agents read.",
  sections: [
    { key: "decisions", heading: "Decisions", items: [{ ref: "M-1" }] },
  ],
  facts: 1,
  current: true,
  receipt_id: "rb",
  created_at: "2026-10-05T10:00:00Z",
};

function fakeClient(over: Record<string, unknown> = {}) {
  const v2 = {
    briefs: {
      get: vi.fn().mockResolvedValue(BRIEF),
      versions: vi.fn().mockResolvedValue({ items: [BRIEF], has_more: false }),
      revise: vi.fn().mockResolvedValue({
        outcome: "applied",
        policy: { effect: "allow" },
        brief: { ...BRIEF, ref: "B-0003", version: 3 },
        receipts: [],
      }),
    },
    memories: {
      list: vi.fn().mockResolvedValue({
        items: [
          memory("M-1"),
          memory("M-2", { section: "conventions" }),
          memory("M-3", { state: "proposed", lifecycle: "proposed" }),
        ],
        has_more: false,
      }),
    },
    receipts: {
      list: vi.fn().mockResolvedValue({
        items: [receipt("M-1"), receipt("M-2"), receipt("M-3")],
        has_more: false,
      }),
    },
    targets: {},
    ...over,
  };
  return { v2 } as unknown as V2Client;
}

describe("the Brief through memax.v2.briefs", () => {
  it("reads the version and the memories it rests on, as the page shows them", async () => {
    const client = fakeClient();
    const brief = await createSdkBrief(client, () => "u1").get({
      space: SPACE,
    });
    expect(client.v2.memories.list).toHaveBeenCalledWith(
      "memax-v2",
      expect.objectContaining({
        state: ["kept", "stale", "conflict", "proposed"],
        limit: 200,
      }),
    );
    expect(brief?.ref).toBe("B-0002");
    expect(brief?.summary).toBe("What agents read.");
    expect(
      brief?.sections.map((s) => [s.key, s.rows.map((r) => [r.ref, r.kind])]),
    ).toEqual([
      [
        "decisions",
        [
          ["M-1", "memory"],
          ["M-3", "waiting"],
        ],
      ],
      ["conventions", [["M-2", "memory"]]],
    ]);
    expect(brief?.sections[0]!.rows[0]!.receipt).toMatchObject({
      by: { kind: "person", self: true },
      action: "kept",
    });
    // No fake numbers: reads and titles aren't served.
    expect(brief?.reads).toBeNull();
    expect(brief?.titles).toEqual({});
  });

  it("is null when the space has no Brief yet", async () => {
    const client = fakeClient({
      briefs: {
        get: vi.fn().mockRejectedValue(new MemaxError("no", "not_found", 404)),
        versions: vi.fn(),
      },
    });
    expect(
      await createSdkBrief(client, () => "u1").get({ space: SPACE }),
    ).toBeNull();
  });

  it("revises with If-Match on the version it started from", async () => {
    const client = fakeClient();
    const result = await createSdkBrief(client, () => "u1").revise({
      space: SPACE,
      base: 2,
      structure: {
        title: "The Brief",
        summary: null,
        sections: [
          {
            key: "decisions",
            heading: "Decisions",
            items: [{ ref: "M-1" }, { text: "Why.", cites: ["M-1"] }],
          },
        ],
      },
      reason: "Tidy",
      idempotencyKey: "k1",
    });
    expect(result).toEqual({ ref: "B-0003", version: 3 });
    expect(client.v2.briefs.revise).toHaveBeenCalledWith(
      "memax-v2",
      {
        title: "The Brief",
        reason: "Tidy",
        sections: [
          {
            key: "decisions",
            heading: "Decisions",
            items: [{ ref: "M-1" }, { text: "Why.", cites: ["M-1"] }],
          },
        ],
      },
      { idempotencyKey: "k1", ifMatch: 2 },
    );
  });

  it("joins the receipts of what the Brief places, cites and waits on first", () => {
    const placed = memory("M-1");
    const waiting = memory("M-9", { lifecycle: "proposed", state: "proposed" });
    const others = Array.from({ length: 200 }, (_, i) =>
      memory(`M-${100 + i}`),
    );
    const read = marginsToRead([...others, waiting, placed], {
      title: "T",
      summary: null,
      sections: [{ key: "d", heading: "D", items: [{ ref: "M-1" }] }],
    });
    expect(read).toHaveLength(120);
    expect(read.slice(0, 2).map((m) => m.ref)).toEqual(["M-9", "M-1"]);
  });
});

describe("targets through memax.v2.targets", () => {
  const target = {
    id: "t1",
    slug: "agents-md",
    kind: "agents_md",
    label: "AGENTS.md",
    path: "AGENTS.md",
    reads: null,
    syncState: "in_sync",
    delivery: "local",
    settings: { include: "kept_and_open", stale: "mark", sizeBudget: 25600 },
    version: 4,
    openDrift: 0,
    holding: [],
    lastCompile: null,
  } satisfies TargetView;
  const answer: V2.Target = {
    id: "t1",
    space_id: "s1",
    tenant_id: "n1",
    kind: "agents_md",
    path: "AGENTS.md",
    label: "AGENTS.md",
    settings: { include: "kept_and_open", stale: "omit", size_budget: 20480 },
    delivery: "local",
    sync_state: "compiling",
    version: 5,
    dirty_gen: 4,
    compiled_gen: 3,
    dirty_at: "x",
    open_drift: 0,
    created_receipt_id: "a",
    last_receipt_id: "b",
    created_at: "x",
    updated_at: "x",
  };

  it("sends a settings change with If-Match and the key, and keeps the URL segment", async () => {
    const update = vi.fn().mockResolvedValue({
      outcome: "applied",
      policy: { effect: "allow" },
      target: answer,
      receipts: [],
    });
    const client = fakeClient({ targets: { update } });
    const next = await createSdkTargets(client).configure({
      space: SPACE,
      target,
      change: { settings: { stale: "omit", sizeBudget: 20480 } },
      idempotencyKey: "k2",
    });
    expect(update).toHaveBeenCalledWith(
      "t1",
      { settings: { stale: "omit", size_budget: 20480 } },
      { idempotencyKey: "k2", ifMatch: 4 },
    );
    expect(next).toMatchObject({
      slug: "agents-md",
      version: 5,
      syncState: "compiling",
      settings: { stale: "omit", sizeBudget: 20480 },
    });
  });

  it("resolves a hand edit: the proposals a pull wrote, and what waits on a person", async () => {
    const pull = vi.fn().mockResolvedValue({
      outcome: "applied",
      policy: { effect: "allow" },
      target: { ...answer, sync_state: "pending_delivery" },
      observations: [
        {
          resolution: {
            receipt_id: "r",
            changes: [
              {
                kind: "edit",
                line: 9,
                outcome: "proposed",
                proposal: "M-0450",
              },
              { kind: "remove", line: 10, outcome: "review" },
              { kind: "new", line: 11, outcome: "skipped", reason: "too_long" },
            ],
          },
        },
      ],
      proposals: [
        memory("M-0450", { statement: "Run tests with `pnpm test -- --run`." }),
      ],
      receipts: [],
    });
    const client = fakeClient({ targets: { pull } });
    const resolved = await createSdkTargets(client).resolveDrift({
      space: SPACE,
      target,
      mode: "pull",
      idempotencyKey: "k3",
    });
    expect(pull).toHaveBeenCalledWith("t1", {}, { idempotencyKey: "k3" });
    expect(resolved).toMatchObject({
      proposals: [
        { ref: "M-0450", statement: "Run tests with `pnpm test -- --run`." },
      ],
      waiting: 1,
      skipped: 1,
      target: { syncState: "pending_delivery" },
    });
  });

  it("maps a pull's hold: held, and the proposals it waits on", async () => {
    const preview = vi.fn().mockResolvedValue({
      target: {
        ...answer,
        sync_state: "held",
        holds: [
          {
            observation: "o1",
            path: "AGENTS.md",
            proposals: ["M-0450", "M-0451"],
            since: "x",
          },
          {
            observation: "o2",
            path: "AGENTS.md",
            proposals: ["M-0451"],
            since: "x",
          },
        ],
      },
      files: [],
      copies: [],
    });
    const client = fakeClient({ targets: { preview } });
    const shown = await createSdkTargets(client).preview({
      space: SPACE,
      target,
    });
    expect(shown.target).toMatchObject({
      syncState: "held",
      holding: ["M-0450", "M-0451"],
    });
    expect(targetOf(answer, [answer]).holding).toEqual([]);
  });
});
