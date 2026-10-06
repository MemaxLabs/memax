import { describe, expect, it, vi } from "vitest";
import { DEMO_SPACES } from "./data/demo-dataset";
import { createUndoStack, type UndoInput } from "./undo-stack";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const team = DEMO_SPACES.find((s) => s.slug === "memax-team")!;

function setup() {
  let time = 0;
  let n = 0;
  const stack = createUndoStack({
    now: () => time,
    newId: () => `id-${++n}`,
  });
  const push = (fields: Partial<UndoInput> = {}) =>
    stack.push({
      source: "sdk",
      space: v2,
      command: "keep",
      ref: "M-0430",
      receipt: "r1",
      restore: null,
      ...fields,
    });
  return { stack, push, tick: (ms: number) => (time += ms) };
}

describe("the tab's undo stack", () => {
  it("answers ⌘Z with the newest decision in the space, inside its window", () => {
    const { stack, push, tick } = setup();
    push({ ref: "M-0430", receipt: "r1" });
    tick(1000);
    const second = push({ ref: "M-0431", receipt: "r2", command: "reject" });
    push({ ref: "M-0444", receipt: "r3", space: team });
    push({ ref: "M-0432", receipt: "r4", source: "demo" });
    expect(stack.latest("sdk", "memax-v2")).toBe(second);
    // Undone (or undoing): ⌘Z moves on to the one before.
    stack.update(second.id, { status: "undoing" });
    expect(stack.latest("sdk", "memax-v2")?.receipt).toBe("r1");
    // Ten minutes after it landed, it's past the server's window.
    tick(10 * 60 * 1000);
    expect(stack.latest("sdk", "memax-v2")).toBeNull();
  });

  it("gives each entry its own idempotency key, kept across retries", () => {
    const { stack, push } = setup();
    const entry = push();
    expect(entry.key).not.toBe(entry.id);
    stack.update(entry.id, { status: "undoing" });
    stack.update(entry.id, { status: "done" });
    expect(stack.get(entry.id)?.key).toBe(entry.key);
  });

  it("drops a refused entry and tells watchers and subscribers", () => {
    const { stack, push } = setup();
    const listener = vi.fn();
    const watcher = vi.fn();
    stack.subscribe(listener);
    const stop = stack.watch(watcher);
    const entry = push();
    expect(listener).toHaveBeenCalledTimes(1);
    stack.emit({ type: "restore", entry });
    expect(watcher).toHaveBeenCalledWith({ type: "restore", entry });
    stop();
    stack.emit({ type: "rollback", entry });
    expect(watcher).toHaveBeenCalledTimes(1);
    stack.drop(entry.id);
    expect(stack.snapshot()).toEqual([]);
    expect(stack.latest("sdk", "memax-v2")).toBeNull();
  });

  it("forgets what's past every window as it grows", () => {
    const { stack, push, tick } = setup();
    push({ receipt: "old" });
    tick(11 * 60 * 1000);
    push({ receipt: "new" });
    expect(stack.snapshot().map((e) => e.receipt)).toEqual(["new"]);
  });
});
