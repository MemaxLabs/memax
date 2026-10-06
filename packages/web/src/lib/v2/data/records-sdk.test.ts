import { MemaxError, type V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { CommandFailedError, isRetryable, toFailure } from "./command-error";
import { DEMO_SPACES } from "./demo-dataset";
import { createSdkMemories } from "./sdk-memories";
import { conditionsOf, recordOf } from "./sdk-record";
import { actorOf, listItemOf, reviewItemOf } from "./sdk-records";
import { createSdkReview } from "./sdk-review";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const ME = "user-zz";

function memory(fields: Partial<V2.Memory> = {}): V2.Memory {
  return {
    id: "m1",
    ref: "M-0430",
    space_id: "s1",
    tenant_id: "t1",
    statement: "MCP write tools must ask for confirmation with input_required.",
    section: "conventions",
    kind: "fact",
    state: "proposed",
    lifecycle: "proposed",
    flags: [],
    trust: "agent_own_work",
    version: 1,
    conditions: [],
    scope: {},
    created_receipt_id: "r1",
    last_receipt_id: "r1",
    created_at: "2026-10-05T20:40:00Z",
    updated_at: "2026-10-05T20:40:00Z",
    ...fields,
  };
}

function receipt(fields: Partial<V2.Receipt> = {}): V2.Receipt {
  return {
    id: "r1",
    seq: 1,
    tenant_id: "t1",
    space_id: "s1",
    object_kind: "memory",
    object_id: "m1",
    object_ref: "M-0430",
    action: "proposed",
    actor_kind: "agent",
    agent: "claude-code",
    via: "mcp",
    occurred_at: "2026-10-05T20:40:00Z",
    recorded_at: "2026-10-05T20:40:00Z",
    stream_id: "m1",
    stream_version: 1,
    ...fields,
  };
}

describe("mapping /v2 records", () => {
  it("names actors: agents by registry key, the viewer as self", () => {
    expect(actorOf(receipt({ agent: "gemini-cli" }), ME)).toEqual({
      kind: "agent",
      agent: "gemini",
    });
    expect(
      actorOf(receipt({ actor_kind: "person", actor_id: ME }), ME),
    ).toEqual({ kind: "person", self: true });
    expect(
      actorOf(receipt({ actor_kind: "person", actor_id: "jy" }), ME),
    ).toEqual({ kind: "person", self: false });
    expect(actorOf(receipt({ actor_kind: "dream" }), ME)).toEqual({
      kind: "dream",
    });
    expect(actorOf(undefined, ME)).toBeNull();
  });

  it("reads an update from its receipt, and quarantine from trust", () => {
    const created = receipt({
      source: { kind: "memory", ref: "M-0156" },
      session_ref: "session 3e1a",
    });
    const item = reviewItemOf(
      memory({ trust: "external" }),
      new Map([["r1", created]]),
      ME,
    );
    expect(item).toMatchObject({
      ref: "M-0430",
      version: 1,
      state: "proposed",
      lifecycle: "proposed",
      external: true,
      by: { kind: "agent", agent: "claude-code" },
      action: "updated",
      updates: "M-0156",
      session: "3e1a",
      // No links in /v2: nothing to compare.
      conflictsWith: null,
    });
  });

  it("shows a stale memory by the receipt that flagged it", () => {
    const flagged = receipt({
      id: "r9",
      seq: 9,
      action: "flagged",
      actor_kind: "dream",
      agent: undefined,
      occurred_at: "2026-10-05T10:12:00Z",
      source: { kind: "pr", ref: "PR #198" },
    });
    const stale = memory({
      state: "stale",
      lifecycle: "kept",
      flags: ["stale"],
      last_receipt_id: "r9",
    });
    const receipts = new Map([
      ["r1", receipt()],
      ["r9", flagged],
    ]);
    expect(reviewItemOf(stale, receipts, ME)).toMatchObject({
      state: "stale",
      lifecycle: "kept",
      action: "flagged",
      by: { kind: "dream" },
      at: "2026-10-05T10:12:00Z",
    });
    expect(listItemOf(stale, receipts, ME).note).toEqual({
      kind: "stale",
      changedAt: "2026-10-05T10:12:00Z",
      source: "PR #198",
    });
  });

  it("keeps a forgotten memory's words out", () => {
    const forgotten = memory({
      statement: "",
      state: "forgotten",
      lifecycle: "forgotten",
      last_receipt_id: "r2",
    });
    const item = listItemOf(
      forgotten,
      new Map([["r2", receipt({ id: "r2", action: "forgot" })]]),
      ME,
    );
    expect(item.statement).toBe("");
    expect(item.forgotten).toEqual({
      at: "2026-10-05T20:40:00Z",
      by: null,
      detail: null,
    });
  });

  it("reads conditions in the shapes a writer is likely to send", () => {
    expect(
      conditionsOf([
        "ADR 004 is unchanged",
        { subject: "github.com/riverqueue/river", rest: "is in go.mod" },
        { code: "pnpm-workspace.yaml", text: "has a catalog" },
        42,
        {},
      ]),
    ).toEqual([
      { key: "0", code: null, text: "ADR 004 is unchanged" },
      { key: "1", code: "github.com/riverqueue/river", text: "is in go.mod" },
      { key: "2", code: "pnpm-workspace.yaml", text: "has a catalog" },
    ]);
  });

  it("builds a memory's page: the Keep for the seal, lineage oldest first", () => {
    const kept = receipt({
      id: "r2",
      seq: 2,
      action: "kept",
      actor_kind: "person",
      actor_id: ME,
      agent: undefined,
      via: "web",
      occurred_at: "2026-10-02T17:58:00Z",
    });
    const record = recordOf(
      {
        memory: memory({
          ref: "M-0219",
          state: "kept",
          lifecycle: "kept",
          version: 2,
          sources: [
            {
              id: "s1",
              kind: "pr",
              ref: "PR #212 · Move jobs to River",
              uri: "https://github.com/MemaxLabs/memax/pull/212",
              locator: {},
              external: false,
              trust: "repository",
              created_at: "2026-10-02T17:58:00Z",
            },
          ],
        }),
        versions: [],
        receipts: { items: [kept, receipt()], has_more: false },
      },
      ME,
    );
    expect(record.kept).toEqual({
      by: { kind: "person", self: true },
      at: "2026-10-02T17:58:00Z",
    });
    expect(record.lineage.map((e) => e.action)).toEqual(["proposed", "kept"]);
    expect(record.sources[0]).toMatchObject({
      kind: "pr",
      url: "https://github.com/MemaxLabs/memax/pull/212",
    });
    // Not served yet: placeholders, never invented.
    expect(record.reaches).toBeNull();
    expect(record.merged).toBeNull();
    expect(record.reads).toBeNull();
  });
});

describe("command failures", () => {
  const refused = (code: string) =>
    new MemaxError("refused", "refused", 403, {
      policy: { effect: "refuse", code, message: "M-0430 comes from…" },
    });

  it.each([
    [
      refused("external_needs_review"),
      { kind: "refused", code: "external_needs_review" },
    ],
    [refused("viewer"), { kind: "refused", code: "viewer" }],
    [
      new MemaxError("changed", "edit_clash", 412, { current_version: 3 }),
      { kind: "clash", currentVersion: 3 },
    ],
    [new MemaxError("x", "invalid_transition", 409), { kind: "decided" }],
    [new MemaxError("x", "not_found", 404), { kind: "not-found" }],
    [new MemaxError("x", "network_error", 0), { kind: "unreachable" }],
    [new MemaxError("x", "busy", 503), { kind: "unreachable" }],
    [
      new MemaxError("x", "rate_limited", 429, undefined, 7),
      { kind: "rate-limited", retryAfter: 7 },
    ],
    [new CommandFailedError({ kind: "unavailable" }), { kind: "unavailable" }],
  ])("%s", (error, expected) => {
    expect(toFailure(error)).toMatchObject(expected);
  });

  it("retries only what the same key can safely repeat", () => {
    expect(isRetryable({ kind: "unreachable" })).toBe(true);
    expect(isRetryable({ kind: "rate-limited", retryAfter: 2 })).toBe(true);
    expect(
      isRetryable({ kind: "refused", code: "viewer", message: null }),
    ).toBe(false);
    expect(isRetryable({ kind: "clash", currentVersion: 2 })).toBe(false);
  });
});

function command(ref: string, outcome: "applied" | "proposed" = "applied") {
  return {
    outcome,
    policy: { effect: "apply" },
    memory: memory({ ref, version: 2 }),
    receipts: [receipt({ action: "kept" })],
  };
}

function fakeClient() {
  const proposal = memory({
    last_receipt_id: "r-old",
    created_receipt_id: "r-old",
  });
  return {
    v2: {
      review: {
        list: vi.fn().mockResolvedValue({
          items: [proposal],
          has_more: false,
          total: 1,
        }),
      },
      receipts: {
        list: vi
          .fn()
          .mockImplementation((_space: string, opts: { memory?: string }) =>
            Promise.resolve({
              items: opts.memory
                ? [
                    receipt({
                      id: "r-old",
                      source: { kind: "memory", ref: "M-0156" },
                    }),
                  ]
                : [],
              has_more: false,
            }),
          ),
      },
      memories: {
        get: vi.fn().mockImplementation((ref: string) =>
          Promise.resolve({
            memory: memory({
              ref,
              statement:
                ref === "M-0156"
                  ? "MCP write tools must ask for confirmation through elicitation."
                  : "MCP write tools must ask for confirmation with input_required.",
              sources: [
                {
                  id: "s1",
                  kind: "url",
                  ref: "modelcontextprotocol.io · spec 2026-07-28",
                  uri: "https://modelcontextprotocol.io/specification",
                  locator: {},
                  external: true,
                  trust: "external",
                  quote: "A server that needs input returns input_required.",
                  created_at: "2026-10-05T20:40:00Z",
                },
              ],
            }),
            versions: [],
            receipts: { items: [], has_more: false },
          }),
        ),
        list: vi.fn().mockResolvedValue({ items: [], has_more: false }),
        keep: vi.fn().mockResolvedValue(command("M-0430")),
        reject: vi.fn().mockResolvedValue(command("M-0430")),
        edit: vi.fn().mockResolvedValue(command("M-0430")),
      },
    },
  };
}

describe("the SDK's Review", () => {
  it("joins each proposal to its receipts, falling back to its own history", async () => {
    const client = fakeClient();
    const review = createSdkReview(client as never, () => ME);
    const queue = await review.queue({ space: v2 });
    expect(client.v2.receipts.list).toHaveBeenCalledWith("memax-v2", {
      limit: 200,
      signal: undefined,
    });
    expect(client.v2.receipts.list).toHaveBeenCalledWith("memax-v2", {
      memory: "m1",
      limit: 50,
      signal: undefined,
    });
    expect(queue.total).toBe(1);
    expect(queue.items[0]).toMatchObject({
      action: "updated",
      updates: "M-0156",
    });
  });

  it("keeps and rejects with If-Match, the space and the caller's key", async () => {
    const client = fakeClient();
    const review = createSdkReview(client as never, () => ME);
    const { items } = await review.queue({ space: v2 });
    const kept = await review.keep({
      space: v2,
      item: items[0],
      idempotencyKey: "k1",
    });
    expect(client.v2.memories.keep).toHaveBeenCalledWith(
      "M-0430",
      {},
      { space: "memax-v2", idempotencyKey: "k1", ifMatch: 1 },
    );
    expect(kept).toEqual({
      ref: "M-0430",
      outcome: "kept",
      version: 2,
      recompiled: null,
    });
    expect(kept.undo).toBeUndefined();
    await review.reject({
      space: v2,
      item: items[0],
      reason: "Not true for cloud agents.",
      idempotencyKey: "k2",
    });
    expect(client.v2.memories.reject).toHaveBeenCalledWith(
      "M-0430",
      { reason: "Not true for cloud agents." },
      { space: "memax-v2", idempotencyKey: "k2", ifMatch: 1 },
    );
  });

  it("builds the card from the memory and the one it updates", async () => {
    const client = fakeClient();
    const review = createSdkReview(client as never, () => ME);
    const { items } = await review.queue({ space: v2 });
    const card = await review.card({ space: v2, item: items[0] });
    expect(card.before).toEqual({
      ref: "M-0156",
      statement:
        "MCP write tools must ask for confirmation through elicitation.",
    });
    expect(card.evidence?.quote).toContain("input_required");
    expect(card.readFrom).toBe("modelcontextprotocol.io · spec 2026-07-28");
    expect(card.touches).toMatchObject({ basis: "links", targets: null });
    expect(await review.conflict({ space: v2, ref: "M-0430" })).toBeNull();
    await expect(
      review.resolveConflict({
        space: v2,
        ref: "M-0430",
        option: "both",
        decision: "x",
        idempotencyKey: "k3",
      }),
    ).rejects.toBeInstanceOf(CommandFailedError);
  });
});

describe("the SDK's Memories", () => {
  it("lists by state and edits with If-Match", async () => {
    const client = fakeClient();
    const memories = createSdkMemories(client as never, () => ME);
    const page = await memories.list({ space: v2, filter: "waiting" });
    expect(client.v2.memories.list).toHaveBeenCalledWith("memax-v2", {
      state: ["proposed", "conflict", "stale"],
      cursor: undefined,
      limit: 50,
      signal: undefined,
    });
    expect(page).toMatchObject({ sectionCounts: null, total: null });
    const result = await memories.edit({
      space: v2,
      ref: "M-0430",
      version: 1,
      statement: "New words.",
      reason: "Why",
      keep: true,
      idempotencyKey: "k4",
    });
    expect(client.v2.memories.edit).toHaveBeenCalledWith(
      "M-0430",
      { statement: "New words.", reason: "Why", keep: true },
      { space: "memax-v2", ifMatch: 1, idempotencyKey: "k4" },
    );
    expect(result.outcome).toBe("kept");
  });

  it("answers null for a memory the person can't open", async () => {
    const client = fakeClient();
    client.v2.memories.get.mockRejectedValueOnce(
      new MemaxError("no", "not_found", 404),
    );
    const memories = createSdkMemories(client as never, () => ME);
    expect(await memories.get({ space: v2, ref: "M-9999" })).toBeNull();
  });
});
