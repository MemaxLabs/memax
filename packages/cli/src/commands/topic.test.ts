import { describe, expect, it } from "vitest";
import {
  buildTopicPathMap,
  flattenTopics,
  resolveTopicReference,
  topicDisplayCount,
  topicDisplayID,
} from "./topic.js";

describe("topicDisplayCount", () => {
  it("prefers subtree totals over direct counts", () => {
    expect(topicDisplayCount({ memory_count: 0, total_memory_count: 7 })).toBe(
      7,
    );
  });

  it("falls back to direct count when subtree total is zero", () => {
    expect(topicDisplayCount({ memory_count: 3, total_memory_count: 0 })).toBe(
      0,
    );
  });
});

describe("buildTopicPathMap", () => {
  it("builds slash-delimited topic paths from the tree", () => {
    const map = buildTopicPathMap([
      {
        id: "root",
        owner_id: "u1",
        hub_id: "h1",
        parent_id: null,
        name: "Engineering",
        description: "",
        icon: "folder",
        position: 0,
        pinned: false,
        user_modified: false,
        created_at: "",
        updated_at: "",
        memory_count: 1,
        total_memory_count: 2,
        kind_dots: [],
        children: [
          {
            id: "child",
            owner_id: "u1",
            hub_id: "h1",
            parent_id: "root",
            name: "Caching",
            description: "",
            icon: "folder",
            position: 0,
            pinned: false,
            user_modified: false,
            created_at: "",
            updated_at: "",
            memory_count: 1,
            total_memory_count: 1,
            kind_dots: [],
            children: [],
          },
        ],
      },
    ]);

    expect(map.get("root")).toBe("Engineering");
    expect(map.get("child")).toBe("Engineering / Caching");
  });
});

describe("topicDisplayID", () => {
  it("shortens IDs by default", () => {
    expect(topicDisplayID("12345678-1234-1234-1234-123456789abc")).toBe(
      "12345678",
    );
  });

  it("shows full IDs in verbose mode", () => {
    expect(topicDisplayID("12345678-1234-1234-1234-123456789abc", true)).toBe(
      "12345678-1234-1234-1234-123456789abc",
    );
  });
});

describe("resolveTopicReference", () => {
  const topics = [
    {
      id: "12345678-1234-1234-1234-123456789abc",
      owner_id: "u1",
      hub_id: "h1",
      parent_id: null,
      name: "Engineering",
      description: "",
      icon: "folder",
      position: 0,
      pinned: false,
      user_modified: false,
      created_at: "",
      updated_at: "",
      memory_count: 1,
      total_memory_count: 2,
      kind_dots: [],
      children: [
        {
          id: "abcdef12-1234-1234-1234-123456789abc",
          owner_id: "u1",
          hub_id: "h1",
          parent_id: "12345678-1234-1234-1234-123456789abc",
          name: "Caching",
          description: "",
          icon: "folder",
          position: 0,
          pinned: false,
          user_modified: false,
          created_at: "",
          updated_at: "",
          memory_count: 1,
          total_memory_count: 1,
          kind_dots: [],
          children: [],
        },
      ],
    },
  ];

  it("resolves exact IDs", () => {
    expect(
      resolveTopicReference(topics, "12345678-1234-1234-1234-123456789abc"),
    ).toBe("12345678-1234-1234-1234-123456789abc");
  });

  it("resolves unique ID prefixes", () => {
    expect(resolveTopicReference(topics, "abcdef12")).toBe(
      "abcdef12-1234-1234-1234-123456789abc",
    );
  });

  it("flattens nested topics for resolution", () => {
    expect(flattenTopics(topics)).toHaveLength(2);
  });
});
