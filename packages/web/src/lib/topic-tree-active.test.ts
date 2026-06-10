import { describe, expect, it } from "vitest";
import {
  buildAncestorTopicIds,
  getActiveTopicIdForPath,
  getMemoryIdForPath,
} from "@/lib/topic-tree-active";
import type { TopicTree } from "@/hooks/use-topics";

describe("topic tree active helpers", () => {
  it("extracts a memory id from a memory detail route", () => {
    expect(getMemoryIdForPath("/memories/memory-123")).toBe("memory-123");
  });

  it("does not treat reserved topic routes as memory ids", () => {
    expect(getMemoryIdForPath("/memories/topics")).toBeUndefined();
  });

  it("uses the topic route id when browsing a topic page", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories/topics/topic-123",
        memoryTopicId: "topic-from-memory",
      }),
    ).toBe("topic-123");
  });

  it("uses the memory topic id when browsing a memory detail page", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories/memory-123",
        memoryTopicId: "topic-456",
      }),
    ).toBe("topic-456");
  });

  it("returns undefined for non-topic memory detail without a topic", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories/memory-123",
        memoryTopicId: null,
      }),
    ).toBeUndefined();
  });

  it("returns undefined for unrelated routes", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/settings",
        memoryTopicId: "topic-456",
      }),
    ).toBeUndefined();
  });

  it("returns undefined for the memories root route", () => {
    expect(
      getActiveTopicIdForPath({
        pathname: "/memories",
        memoryTopicId: "topic-456",
      }),
    ).toBeUndefined();
  });

  it("builds ancestor ids for a nested topic", () => {
    expect(
      buildAncestorTopicIds(
        [
          {
            id: "root",
            children: [
              {
                id: "parent",
                children: [{ id: "leaf", children: [] }],
              },
            ],
          },
        ] as unknown as TopicTree[],
        "leaf",
      ),
    ).toEqual(["root", "parent"]);
  });

  it("returns an empty array for a root topic", () => {
    expect(
      buildAncestorTopicIds(
        [{ id: "root", children: [] }] as unknown as TopicTree[],
        "root",
      ),
    ).toEqual([]);
  });

  it("returns null when the topic is missing", () => {
    expect(
      buildAncestorTopicIds(
        [{ id: "root", children: [] }] as unknown as TopicTree[],
        "missing",
      ),
    ).toBeNull();
  });
});
