import { describe, expect, it } from "vitest";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import {
  INITIAL_REVIEW,
  nextAfter,
  reviewReducer,
  type ReviewAction,
  type ReviewState,
} from "./review-state";

const run = (actions: ReviewAction[], from: ReviewState = INITIAL_REVIEW) =>
  actions.reduce(reviewReducer, from);
const ORDER = ["M-0430", "M-0431", "M-0432"];
const base = { version: 1, statement: "MCP write tools must ask." };
const theirs = {
  version: 2,
  statement: "MCP write tools ask with input_required.",
  by: { kind: "person" as const, self: false, name: "Jiahao" },
  at: "2026-10-05T21:39:00Z",
};

describe("Review's state machine", () => {
  it("seals on Keep, then moves to the next card when decided", () => {
    const sealed = run([{ type: "seal", ref: "M-0430" }]);
    expect(sealed.sealed).toMatchObject({ ref: "M-0430", statement: null });
    expect(sealed.busy).toBe("M-0430");
    // The seal holds its card: no selecting away mid-command.
    expect(reviewReducer(sealed, { type: "select", ref: "M-0432" })).toBe(
      sealed,
    );
    const next = reviewReducer(sealed, {
      type: "decided",
      ref: "M-0430",
      outcome: "kept",
      order: ORDER,
    });
    expect(next).toMatchObject({
      selected: "M-0431",
      sealed: null,
      busy: null,
      done: { "M-0430": "kept" },
    });
  });

  it("rolls the seal back to where it was on failure", () => {
    const editing = run([
      { type: "edit", ref: "M-0430", base },
      { type: "draft", draft: "New words." },
    ]);
    const sealed = reviewReducer(editing, {
      type: "seal",
      ref: "M-0430",
      statement: "New words.",
    });
    expect(sealed.mode.kind).toBe("browse");
    const back = reviewReducer(sealed, { type: "unseal", ref: "M-0430" });
    expect(back.sealed).toBeNull();
    expect(back.mode).toEqual(editing.mode);
  });

  it("turns a clash into the clash card, then Combine edits from theirs", () => {
    const clash = run([
      { type: "edit", ref: "M-0430", base },
      { type: "draft", draft: "Mine.", reason: "Because." },
      { type: "seal", ref: "M-0430", statement: "Mine." },
      { type: "clash", ref: "M-0430", theirs },
    ]);
    expect(clash.mode).toMatchObject({
      kind: "clash",
      mine: "Mine.",
      reason: "Because.",
      base,
      theirs,
    });
    expect(clash.busy).toBeNull();
    const combined = reviewReducer(clash, { type: "combine" });
    expect(combined.mode).toMatchObject({
      kind: "editing",
      base: { version: 2, statement: theirs.statement },
      draft: "Mine.",
      reason: "Because.",
    });
    expect(reviewReducer(clash, { type: "keepTheirs" }).mode.kind).toBe(
      "browse",
    );
  });

  it("keeps the reject reason as it's typed, and Esc goes back", () => {
    const rejecting = run([
      { type: "reject", ref: "M-0432" },
      { type: "draft", reason: "Not ours." },
    ]);
    expect(rejecting.mode).toEqual({
      kind: "rejecting",
      ref: "M-0432",
      reason: "Not ours.",
    });
    expect(reviewReducer(rejecting, { type: "browse" }).mode.kind).toBe(
      "browse",
    );
  });

  it("selects the next card, or the one before at the end", () => {
    expect(nextAfter(ORDER, "M-0430")).toBe("M-0431");
    expect(nextAfter(ORDER, "M-0432")).toBe("M-0431");
    expect(nextAfter(["M-0430"], "M-0430")).toBeNull();
  });
});

describe("idempotency keys", () => {
  it("reuses one key per intent until it settles", () => {
    let n = 0;
    const keys = new IntentKeys(() => `key-${++n}`);
    const keep = intentOf("keep", "M-0430", 1);
    expect(keys.keyFor(keep)).toBe("key-1");
    // A retry is the same intent: the same key.
    expect(keys.keyFor(keep)).toBe("key-1");
    keys.settle(keep);
    expect(keys.keyFor(keep)).toBe("key-2");
  });

  it("makes new words or a newer version a new intent", () => {
    expect(intentOf("edit", "M-0430", 1, "a")).toBe(
      intentOf("edit", "M-0430", 1, "a"),
    );
    expect(intentOf("edit", "M-0430", 1, "a")).not.toBe(
      intentOf("edit", "M-0430", 1, "b"),
    );
    expect(intentOf("edit", "M-0430", 1, "a")).not.toBe(
      intentOf("edit", "M-0430", 2, "a"),
    );
    expect(intentOf("reject", "M-0430", 1, undefined)).not.toBe(
      intentOf("reject", "M-0430", 1, "why"),
    );
  });
});
