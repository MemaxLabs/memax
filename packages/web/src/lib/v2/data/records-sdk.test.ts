import { MemaxError, type V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { CommandFailedError, isRetryable, toFailure } from "./command-error";
import { DEMO_SPACES } from "./demo-dataset";
import { choiceFor, conflictOf } from "./sdk-conflict";
import { createSdkMemories } from "./sdk-memories";
import { conditionsOf, recordOf } from "./sdk-record";
import {
  actorOf,
  conflictPartnerOf,
  listItemOf,
  reviewItemOf,
} from "./sdk-records";
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
      // Not flagged: nothing to compare, and the judge has had its say.
      conflictsWith: null,
      judge: null,
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
    [
      new MemaxError("x", "busy", 503, { retry_after: 2, ref: "M-0430" }, 2),
      { kind: "busy", retryAfter: 2, ref: "M-0430" },
    ],
    [
      new MemaxError("x", "busy", 503, { retry_after: 1 }),
      { kind: "busy", retryAfter: 1, ref: null },
    ],
    [
      new MemaxError("x", "undo_refused", 409, {
        reason: "later_changes",
        ref: "B-0043",
      }),
      { kind: "undo-refused", reason: "later_changes", ref: "B-0043" },
    ],
    [
      new MemaxError("x", "undo_refused", 409, { reason: "sideways" }),
      { kind: "undo-refused", reason: "not_undoable", ref: null },
    ],
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
    // Keep before the judge: the same command, after Retry-After.
    expect(isRetryable({ kind: "busy", retryAfter: 2, ref: null })).toBe(true);
    expect(
      isRetryable({ kind: "undo-refused", reason: "window_passed", ref: null }),
    ).toBe(false);
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
        // No conflict to compare (spec: 409 invalid_transition).
        conflict: vi
          .fn()
          .mockRejectedValue(
            new MemaxError("no conflict", "invalid_transition", 409),
          ),
        resolveConflict: vi.fn().mockResolvedValue({
          ...command("M-0431"),
          memories: [memory({ ref: "M-0431" }), memory({ ref: "M-0174" })],
          receipts: [receipt({ id: "r-resolved", action: "resolved" })],
        }),
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
      // Undo addresses the command by its receipt.
      receipt: "r1",
    });
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
    // A memory with no conflict has nothing to compare (409 → null).
    expect(await review.conflict({ space: v2, ref: "M-0430" })).toBeNull();
  });

  it("settles a conflict through the ledger: the answer as the server's choice", async () => {
    const client = fakeClient();
    const review = createSdkReview(client as never, () => ME);
    const result = await review.resolveConflict({
      space: v2,
      ref: "M-0431",
      other: "M-0174",
      version: 3,
      option: "both",
      statement: "The v2 API runs on Fly.io.",
      otherStatement: "Previews run on Railway.",
      idempotencyKey: "k3",
    });
    expect(client.v2.memories.resolveConflict).toHaveBeenCalledWith(
      "M-0431",
      {
        choice: "keep_both",
        other: "M-0174",
        statement: "The v2 API runs on Fly.io.",
        other_statement: "Previews run on Railway.",
      },
      { space: "memax-v2", idempotencyKey: "k3", ifMatch: 3 },
    );
    expect(result).toMatchObject({
      ref: "M-0431",
      outcome: "kept",
      receipt: "r-resolved",
    });
    for (const [option, choice] of [
      ["proposal", "keep_this"],
      ["kept", "keep_other"],
      ["open", "leave_open"],
    ] as const) {
      await review.resolveConflict({
        space: v2,
        ref: "M-0431",
        other: "M-0174",
        version: 3,
        option,
        // Ignored outside "both".
        statement: "x",
        idempotencyKey: option,
      });
      expect(client.v2.memories.resolveConflict).toHaveBeenLastCalledWith(
        "M-0431",
        { choice, other: "M-0174" },
        expect.objectContaining({ ifMatch: 3 }),
      );
    }
  });
});

describe("mapping what the judge said", () => {
  const link = (fields: Partial<V2.Link>): V2.Link => ({
    id: "l1",
    kind: "conflicts_with",
    direction: "out",
    memory_id: "m2",
    ref: "M-0174",
    receipt_id: "r-judge",
    created_at: "2026-10-05T21:26:30Z",
    ...fields,
  });

  it("puts the working mark on a proposal the judge hasn't checked, and not on one it couldn't", () => {
    const receipts = new Map([["r1", receipt()]]);
    const working = memory({ judge: { state: "working", version: 1 } });
    expect(reviewItemOf(working, receipts, ME).judge).toBe("working");
    const failed = memory({
      judge: { state: "failed", version: 1, outcome: "failed" },
    });
    expect(reviewItemOf(failed, receipts, ME)).toMatchObject({
      judge: "failed",
      state: "proposed",
      conflictsWith: null,
    });
    const judged = memory({
      judge: { state: "judged", version: 1, verdict: "unrelated" },
    });
    expect(reviewItemOf(judged, receipts, ME).judge).toBeNull();
  });

  it("reads a flagged proposal's partner from its link, and keeps its proposer", () => {
    const flagged = receipt({
      id: "r2",
      seq: 2,
      action: "flagged",
      actor_kind: "memax",
      agent: undefined,
      via: "system",
      source: { kind: "memory", ref: "M-0174" },
    });
    const item = reviewItemOf(
      memory({
        ref: "M-0431",
        state: "conflict",
        flags: ["conflict"],
        last_receipt_id: "r2",
        links: [link({})],
        judge: { state: "judged", version: 1, verdict: "contradicts" },
      }),
      new Map([
        ["r1", receipt({ agent: "codex" })],
        ["r2", flagged],
      ]),
      ME,
    );
    expect(item).toMatchObject({
      state: "conflict",
      conflictsWith: "M-0174",
      judge: null,
      // Review.png's M-0431: "CX proposed", not "Memax flagged".
      by: { kind: "agent", agent: "codex" },
      action: "proposed",
    });
    // Without a link, the verdict still names it.
    expect(
      conflictPartnerOf(
        memory({
          state: "conflict",
          judge: {
            state: "judged",
            version: 1,
            verdict: "contradicts",
            related: { id: "m2", ref: "M-0174" },
          },
        }),
      ),
    ).toBe("M-0174");
  });

  it("reads an update from the judge's link, with the words to diff", async () => {
    const updates = {
      id: "m0",
      ref: "M-0156",
      version: 1,
      statement: "Old.",
      lifecycle: "kept" as const,
    };
    const item = reviewItemOf(
      memory({ updates, links: [link({ kind: "supersedes", ref: "M-0156" })] }),
      new Map([["r1", receipt()]]),
      ME,
    );
    expect(item).toMatchObject({ updates: "M-0156", action: "updated" });
    const client = fakeClient();
    client.v2.memories.get.mockImplementation((ref: string) =>
      Promise.resolve({
        memory: memory({
          ref,
          updates: ref === "M-0430" ? updates : undefined,
        }),
        versions: [],
        receipts: { items: [], has_more: false },
      }),
    );
    const card = await createSdkReview(client as never, () => ME).card({
      space: v2,
      item,
    });
    expect(card.before).toEqual({ ref: "M-0156", statement: "Old." });
    expect(card.touches.basis).toBe("links");
    expect(card.touches.memories.map((m) => m.ref)).toEqual(["M-0156"]);
  });

  it("builds the card's conflict line from the decision it contradicts", async () => {
    const client = fakeClient();
    client.v2.memories.get.mockImplementation((ref: string) =>
      Promise.resolve({
        memory:
          ref === "M-0431"
            ? memory({
                ref,
                state: "conflict",
                flags: ["conflict"],
                links: [link({})],
              })
            : memory({
                ref,
                state: "kept",
                lifecycle: "kept",
                statement: "Deploy the v2 API to Railway.",
              }),
        versions: [],
        receipts: { items: [], has_more: false },
      }),
    );
    const review = createSdkReview(client as never, () => ME);
    const card = await review.card({
      space: v2,
      item: {
        ...reviewItemOf(
          memory({ ref: "M-0431", state: "conflict", links: [link({})] }),
          new Map([["r1", receipt()]]),
          ME,
        ),
      },
    });
    expect(card.conflict).toEqual({
      ref: "M-0174",
      statement: "Deploy the v2 API to Railway.",
    });
    expect(card.touches.memories.map((m) => m.ref)).toEqual(["M-0174"]);
  });

  it("maps the conflict endpoint onto ReviewConflict, from either side", () => {
    const fly = memory({
      id: "m1",
      ref: "M-0431",
      statement: "Deploy the v2 API to Fly.io in iad and ams.",
      state: "conflict",
      version: 2,
      kind: "decision",
      decision: { why: "The API already runs there.", area: "deploy target" },
      sources: [
        {
          id: "s1",
          kind: "file",
          ref: "infra/fly/api.toml",
          locator: {},
          external: false,
          trust: "repository",
          created_at: "2026-10-01T16:10:00Z",
        },
      ],
    });
    const railway = memory({
      id: "m2",
      ref: "M-0174",
      statement: "Deploy the v2 API to Railway for its preview environments.",
      state: "kept",
      lifecycle: "kept",
      kind: "decision",
      decision: { why: "Simpler previews.", area: "deploy target" },
      created_receipt_id: "r-railway",
    });
    const proposed = receipt({
      id: "r1",
      object_id: "m1",
      agent: "codex",
      session_ref: "9f1c",
    });
    const keptBy = receipt({
      id: "r-kept",
      object_id: "m2",
      action: "kept",
      actor_kind: "person",
      actor_id: "jy",
      agent: undefined,
      occurred_at: "2026-09-18T22:05:00Z",
    });
    const options: V2.ConflictOption[] = [
      {
        choice: "keep_this",
        effects: [
          { ref: "M-0431", change: "kept" },
          { ref: "M-0174", change: "superseded" },
        ],
        allowed: true,
      },
      {
        choice: "keep_other",
        effects: [
          { ref: "M-0431", change: "rejected" },
          { ref: "M-0174", change: "stays" },
        ],
        allowed: true,
      },
      {
        choice: "keep_both",
        effects: [
          { ref: "M-0431", change: "kept" },
          { ref: "M-0174", change: "stays" },
        ],
        allowed: false,
        policy: {
          effect: "refuse",
          code: "decision_needs_web",
          message: "Decisions in memax-v2 need a person on the web.",
        },
      },
      {
        choice: "leave_open",
        effects: [
          { ref: "M-0431", change: "open" },
          { ref: "M-0174", change: "open" },
        ],
        allowed: true,
      },
    ];
    const fromFlagged = conflictOf(
      {
        memory: fly,
        other: railway,
        flagged_ref: "M-0431",
        decision_ref: "M-0174",
        link: link({}),
        receipts: [proposed, keptBy],
        options,
      },
      ME,
    );
    expect(fromFlagged).toMatchObject({
      question: null,
      area: "deploy target",
      suggested: null,
      recompiles: null,
      kept: {
        ref: "M-0174",
        version: 1,
        why: "Simpler previews.",
        by: { kind: "person", self: false },
        at: "2026-09-18T22:05:00Z",
      },
      proposal: {
        ref: "M-0431",
        version: 2,
        by: { kind: "agent", agent: "codex" },
        evidence: { code: "infra/fly/api.toml" },
        session: "9f1c",
      },
    });
    expect(fromFlagged.options.map((o) => [o.kind, o.allowed])).toEqual([
      ["proposal", true],
      ["kept", true],
      ["both", false],
      ["open", true],
    ]);
    expect(fromFlagged.options[0].decision).toBe(fly.statement);
    expect(fromFlagged.options[2]).toMatchObject({
      refusal: { code: "decision_needs_web" },
      narrowed: { proposal: fly.statement, kept: railway.statement },
    });
    // Asked from the decision's side, keep_this means the decision stays.
    const fromKept = conflictOf(
      {
        memory: railway,
        other: fly,
        flagged_ref: "M-0431",
        decision_ref: "M-0174",
        link: link({ direction: "in", ref: "M-0431" }),
        receipts: [proposed, keptBy],
        options: [
          { ...options[1], choice: "keep_this" },
          { ...options[0], choice: "keep_other" },
          options[2],
          options[3],
        ],
      },
      ME,
    );
    expect(fromKept.kept.ref).toBe("M-0174");
    expect(fromKept.options[0]).toMatchObject({
      kind: "proposal",
      effects: [
        { ref: "M-0431", change: "kept" },
        { ref: "M-0174", change: "superseded" },
      ],
    });
    expect(choiceFor("proposal")).toBe("keep_this");
  });
});

describe("the judge's folds on a memory's page", () => {
  const NOW = new Date("2026-10-05T22:00:00Z");
  const fold = receipt({
    id: "r-fold",
    seq: 2,
    action: "merged",
    actor_kind: "memax",
    agent: undefined,
    via: "system",
    occurred_at: "2026-10-05T21:33:00Z",
    source: { kind: "memory", ref: "M-0310" },
    reason: "A near-verbatim repeat of M-0310.",
  });

  it("says what it was folded into, with Undo inside the 14 days", () => {
    const record = recordOf(
      {
        memory: memory({ ref: "M-0446", state: "merged", lifecycle: "merged" }),
        versions: [],
        receipts: { items: [fold, receipt()], has_more: false },
      },
      ME,
      { now: NOW },
    );
    expect(record.lineage.at(-1)).toMatchObject({
      action: "merged",
      into: "M-0310",
      by: { kind: "memax" },
      undo: { receipt: "r-fold", until: "2026-10-19T21:33:00.000Z" },
    });
    // Fourteen days on, it can't be undone.
    const late = recordOf(
      {
        memory: memory({ ref: "M-0446", state: "merged", lifecycle: "merged" }),
        versions: [],
        receipts: { items: [fold], has_more: false },
      },
      ME,
      { now: new Date("2026-10-20T00:00:00Z") },
    );
    expect(late.lineage.at(-1)?.undo).toBeNull();
  });

  it("offers no Undo once the fold was undone", () => {
    const undid = receipt({
      id: "r-undid",
      seq: 3,
      action: "undid",
      actor_kind: "person",
      actor_id: ME,
      source: { kind: "receipt", ref: "r-fold" },
    });
    const record = recordOf(
      {
        memory: memory({ ref: "M-0446" }),
        versions: [],
        receipts: { items: [undid, fold, receipt()], has_more: false },
      },
      ME,
      { now: NOW },
    );
    expect(record.lineage.find((e) => e.action === "merged")?.undo).toBeNull();
  });

  it("lists what the judge folded into a kept memory, from its links", async () => {
    const client = fakeClient();
    client.v2.memories.get.mockImplementation((ref: string) =>
      Promise.resolve(
        ref === "M-0310"
          ? {
              memory: memory({
                ref,
                state: "kept",
                lifecycle: "kept",
                links: [
                  {
                    id: "l1",
                    kind: "merged_into",
                    direction: "in",
                    memory_id: "m-fold",
                    ref: "M-0446",
                    receipt_id: "r-fold",
                    created_at: new Date().toISOString(),
                  },
                ],
              }),
              versions: [],
              receipts: { items: [], has_more: false },
            }
          : {
              memory: memory({
                ref,
                statement: "Reviews need a member who isn't the author.",
                state: "merged",
                lifecycle: "merged",
              }),
              versions: [],
              receipts: {
                items: [receipt({ agent: "codex" }), fold],
                has_more: false,
              },
            },
      ),
    );
    const memories = createSdkMemories(client as never, () => ME);
    const record = await memories.get({ space: v2, ref: "M-0310" });
    expect(record?.merged).toMatchObject({
      total: 1,
      notes: [
        {
          ref: "M-0446",
          statement: "Reviews need a member who isn't the author.",
          by: { kind: "agent", agent: "codex" },
          undo: { receipt: "r-fold" },
        },
      ],
    });
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
