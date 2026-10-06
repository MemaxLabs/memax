import { describe, expect, it } from "vitest";
import { toFailure } from "./command-error";
import { DEMO_SPACES } from "./demo-dataset";
import { createDemoSource } from "./demo-source";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const fresh = () => createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });

describe("the demo's Review", () => {
  it("serves Review.png's queue, on hand for the first render", () => {
    const demo = fresh();
    const queue = demo.review.peekQueue?.("memax-v2");
    expect(queue?.items.map((i) => i.ref)).toEqual([
      "M-0430",
      "M-0431",
      "M-0432",
      "M-0433",
      "M-0187",
    ]);
    expect(queue?.items[0]).toMatchObject({
      external: true,
      updates: "M-0156",
      action: "updated",
    });
    expect(queue?.items[1]).toMatchObject({
      state: "conflict",
      conflictsWith: "M-0174",
    });
    expect(queue?.items[4]).toMatchObject({
      state: "stale",
      lifecycle: "kept",
    });
  });

  it("keeps: the item leaves the queue and the overview's count drops", async () => {
    const demo = fresh();
    const [first] = (await demo.review.queue({ space: v2 })).items;
    const result = await demo.review.keep({
      space: v2,
      item: first,
      idempotencyKey: "a",
    });
    expect(result).toMatchObject({
      ref: "M-0430",
      outcome: "kept",
      recompiled: 3,
    });
    expect(result.undo).toBeUndefined();
    const after = await demo.review.queue({ space: v2 });
    expect(after.items.map((i) => i.ref)).not.toContain("M-0430");
    const overview = await demo.overview(v2);
    expect(overview.waiting).toBe(4);
    expect(overview.reviewFilters).toEqual({
      conflicts: 1,
      external: 0,
      stale: 1,
    });
    expect(overview.memories.kept).toBe(215);
  });

  it("replays a retry with the same key instead of deciding twice", async () => {
    const demo = fresh();
    const [first] = (await demo.review.queue({ space: v2 })).items;
    const once = await demo.review.keep({
      space: v2,
      item: first,
      idempotencyKey: "same",
    });
    const again = await demo.review.keep({
      space: v2,
      item: first,
      idempotencyKey: "same",
    });
    expect(again).toEqual(once);
    // A new key is a new command, and it's already decided.
    await expect(
      demo.review.keep({ space: v2, item: first, idempotencyKey: "other" }),
    ).rejects.toSatisfy((err) => toFailure(err).kind === "decided");
  });

  it("refuses a decision on a version that changed", async () => {
    const demo = fresh();
    const [first] = (await demo.review.queue({ space: v2 })).items;
    const stale = { ...first, version: first.version - 1 };
    await expect(
      demo.review.keep({ space: v2, item: stale, idempotencyKey: "v" }),
    ).rejects.toSatisfy((err) => toFailure(err).kind === "clash");
  });

  it("edits then keeps, and Memories shows the new words kept", async () => {
    const demo = fresh();
    const [first] = (await demo.review.queue({ space: v2 })).items;
    const result = await demo.memories.edit({
      space: v2,
      ref: first.ref,
      version: first.version,
      statement:
        "MCP write tools ask for confirmation with input_required when a person is present.",
      reason: "Cloud agents run with nobody watching.",
      keep: true,
      idempotencyKey: "e",
    });
    expect(result).toMatchObject({ outcome: "kept", version: 2 });
    const page = await demo.memories.list({ space: v2, filter: "all" });
    const row = page.items.find((r) => r.ref === "M-0430");
    expect(row).toMatchObject({
      state: "kept",
      statement: expect.stringContaining("when a person is present"),
      receipt: { action: "kept", by: { kind: "person", self: true } },
    });
  });

  it("rejects, and a rejected memory isn't listed", async () => {
    const demo = fresh();
    const items = (await demo.review.queue({ space: v2 })).items;
    const pnpm = items.find((i) => i.ref === "M-0432")!;
    await demo.review.reject({ space: v2, item: pnpm, idempotencyKey: "r" });
    const all = (await demo.memories.list({ space: v2, filter: "waiting" }))
      .items;
    expect(all.map((r) => r.ref)).not.toContain("M-0432");
    expect((await demo.overview(v2)).lastReview).toMatchObject({
      kept: 0,
      rejected: 1,
    });
  });

  it("compares M-0431 with M-0174 and resolves it as a new decision", async () => {
    const demo = fresh();
    const conflict = await demo.review.conflict({ space: v2, ref: "M-0431" });
    expect(conflict).toMatchObject({
      question: "Fly.io or Railway for the v2 API?",
      kept: { ref: "M-0174" },
      proposal: { ref: "M-0431" },
      suggested: 2,
    });
    expect(await demo.review.conflict({ space: v2, ref: "M-0432" })).toBeNull();
    const result = await demo.review.resolveConflict({
      space: v2,
      ref: "M-0431",
      option: "both",
      decision: conflict!.options[2].decision,
      idempotencyKey: "c",
    });
    expect(result).toMatchObject({ outcome: "kept", recompiled: 4 });
    expect(await demo.review.conflict({ space: v2, ref: "M-0431" })).toBeNull();
  });
});

describe("the demo's Memories", () => {
  it("pages Memories.png's rows, then the rest", async () => {
    const demo = fresh();
    const first = await demo.memories.list({ space: v2, filter: "all" });
    expect(first.items).toHaveLength(11);
    expect(first.total).toBe(222);
    expect(first.sectionCounts).toEqual({
      decisions: 38,
      conventions: 61,
      preferences: 12,
    });
    expect(first.nextCursor).toBe("1");
    const second = await demo.memories.list({
      space: v2,
      filter: "all",
      cursor: first.nextCursor!,
    });
    expect(second.items.length).toBeGreaterThan(0);
    expect(second.nextCursor).toBeNull();
  });

  it("filters by state", async () => {
    const demo = fresh();
    const forgotten = await demo.memories.list({
      space: v2,
      filter: "forgotten",
    });
    expect(forgotten.items.map((r) => r.ref)).toEqual(["M-0201"]);
    expect(forgotten.items[0].statement).toBe("");
    const merged = await demo.memories.list({ space: v2, filter: "merged" });
    expect(merged.items.map((r) => r.ref)).toEqual(["N-1187"]);
  });

  it("opens M-0219 with Memory.png's lineage, notes, sources and conditions", async () => {
    const demo = fresh();
    const record = await demo.memories.get({ space: v2, ref: "M-0219" });
    expect(record?.lineage.map((e) => e.action)).toEqual([
      "proposed",
      "kept",
      "merged",
      "handed_off",
      "verified",
    ]);
    expect(record?.merged?.notes.slice(0, 2).map((n) => n.ref)).toEqual([
      "N-1187",
      "N-1203",
    ]);
    expect(record?.merged?.total).toBe(9);
    expect(record?.sources.map((s) => s.label)).toEqual([
      "PR #212 · Move jobs to River",
      "Session 3e1a, lines 212–260",
      "docs/adr/004-queues.md",
    ]);
    expect(record?.conditions[0]).toMatchObject({
      code: "github.com/riverqueue/river",
      text: "is in go.mod",
    });
    expect(record?.reaches).toHaveLength(4);
    expect(await demo.memories.get({ space: v2, ref: "M-9999" })).toBeNull();
  });

  it("raises a clash when an edit starts from an old version", async () => {
    const demo = fresh();
    await demo.memories.edit({
      space: v2,
      ref: "M-0098",
      version: 1,
      statement:
        "API errors are RFC 9457 problem+json, including from MCP tools.",
      idempotencyKey: "x1",
    });
    await expect(
      demo.memories.edit({
        space: v2,
        ref: "M-0098",
        version: 1,
        statement: "Something else.",
        idempotencyKey: "x2",
      }),
    ).rejects.toSatisfy((err) => {
      const failure = toFailure(err);
      return failure.kind === "clash" && failure.currentVersion === 2;
    });
    const latest = await demo.memories.latest({ space: v2, ref: "M-0098" });
    expect(latest).toMatchObject({
      version: 2,
      statement:
        "API errors are RFC 9457 problem+json, including from MCP tools.",
    });
  });
});
