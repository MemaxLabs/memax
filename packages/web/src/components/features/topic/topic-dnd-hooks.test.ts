import { describe, it, expect } from "vitest";
import type { TopicTree } from "memax-sdk";
import {
  collectTopicDescendantIds,
  findTopicInForest,
} from "./topic-dnd-hooks";

function node(
  id: string,
  children: TopicTree[] = [],
  parentId: string | null = null,
): TopicTree {
  return {
    id,
    owner_id: "u1",
    hub_id: "h1",
    parent_id: parentId,
    name: id,
    description: "",
    icon: "folder",
    position: 0,
    pinned: false,
    user_modified: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    memory_count: 0,
    total_memory_count: 0,
    children,
  } as TopicTree;
}

// Forest shape for tests:
//   root
//    ├─ parent
//    │   └─ child
//    │       └─ grandchild
//    └─ cousin
//   other (sibling of root)
function makeForest(): TopicTree[] {
  return [
    node("root", [
      node(
        "parent",
        [node("child", [node("grandchild", [], "child")], "parent")],
        "root",
      ),
      node("cousin", [], "root"),
    ]),
    node("other"),
  ];
}

describe("collectTopicDescendantIds", () => {
  it("returns root + all descendants for a subtree root", () => {
    const ids = collectTopicDescendantIds(makeForest(), "parent");
    expect(ids).toEqual(new Set(["parent", "child", "grandchild"]));
  });

  it("returns just the leaf itself for a childless node", () => {
    const ids = collectTopicDescendantIds(makeForest(), "grandchild");
    expect(ids).toEqual(new Set(["grandchild"]));
  });

  it("returns the full subtree when the target is the top-level root", () => {
    const ids = collectTopicDescendantIds(makeForest(), "root");
    expect(ids).toEqual(
      new Set(["root", "parent", "child", "grandchild", "cousin"]),
    );
  });

  it("returns an empty set for an id that does not exist in the forest", () => {
    const ids = collectTopicDescendantIds(makeForest(), "nonexistent");
    expect(ids).toEqual(new Set());
  });

  it("does not confuse sibling trees", () => {
    // Dragging "root" must not mark "other" (a sibling of root) as invalid.
    const ids = collectTopicDescendantIds(makeForest(), "root");
    expect(ids.has("other")).toBe(false);
  });
});

describe("findTopicInForest", () => {
  it("finds a top-level root", () => {
    const result = findTopicInForest(makeForest(), "other");
    expect(result?.id).toBe("other");
  });

  it("finds a deeply nested node", () => {
    const result = findTopicInForest(makeForest(), "grandchild");
    expect(result?.id).toBe("grandchild");
  });

  it("returns null when the id is not present", () => {
    expect(findTopicInForest(makeForest(), "nope")).toBeNull();
  });

  it("returns null on an empty forest", () => {
    expect(findTopicInForest([], "anything")).toBeNull();
  });
});
