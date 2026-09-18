import { describe, expect, it } from "vitest";
import type { Notification } from "memax-sdk";
import {
  buildFragmentConflictIndex,
  fragmentConflictAction,
} from "./fragment-conflicts";

function contradiction(
  id: string,
  a: string,
  b: string,
  status: Notification["status"] = "pending",
): Notification {
  return {
    id,
    audience: "user",
    kind: "review_contradiction",
    status,
    priority: 0,
    source_kind: "dream",
    created_at: "2026-09-17T00:00:00Z",
    payload: {
      memory_a: { id: a, title: `title ${a}` },
      memory_b: { id: b, title: `title ${b}` },
      similarity: 0.91,
      reason: "they disagree",
    },
  } as Notification;
}

describe("buildFragmentConflictIndex", () => {
  it("lands each pending contradiction under both memories with the right side", () => {
    const index = buildFragmentConflictIndex([contradiction("n1", "m1", "m2")]);
    expect(index.get("m1")).toEqual([
      expect.objectContaining({
        notificationId: "n1",
        side: "a",
        other: { id: "m2", title: "title m2" },
        reason: "they disagree",
      }),
    ]);
    expect(index.get("m2")?.[0]).toMatchObject({
      side: "b",
      other: { id: "m1" },
    });
  });

  it("ignores resolved rows, other kinds and broken payloads", () => {
    const resolved = contradiction("n2", "m1", "m2", "resolved");
    const otherKind = {
      ...contradiction("n3", "m1", "m2"),
      kind: "hub_invite",
    } as Notification;
    const broken = {
      ...contradiction("n4", "m1", "m2"),
      payload: { memory_a: { id: "m1" } },
    } as Notification;
    expect(buildFragmentConflictIndex([resolved, otherKind, broken]).size).toBe(
      0,
    );
    expect(buildFragmentConflictIndex(undefined).size).toBe(0);
  });

  it("stacks several pairs on one memory", () => {
    const index = buildFragmentConflictIndex([
      contradiction("n1", "m1", "m2"),
      contradiction("n2", "m3", "m1"),
    ]);
    expect(index.get("m1")?.map((c) => [c.notificationId, c.side])).toEqual([
      ["n1", "a"],
      ["n2", "b"],
    ]);
  });
});

describe("fragmentConflictAction", () => {
  it("maps side-relative verdicts onto keep_a / keep_b / keep_both", () => {
    expect(fragmentConflictAction({ side: "a" }, "keep_this")).toBe("keep_a");
    expect(fragmentConflictAction({ side: "a" }, "keep_other")).toBe("keep_b");
    expect(fragmentConflictAction({ side: "b" }, "keep_this")).toBe("keep_b");
    expect(fragmentConflictAction({ side: "b" }, "keep_other")).toBe("keep_a");
    expect(fragmentConflictAction({ side: "b" }, "keep_both")).toBe(
      "keep_both",
    );
  });
});
