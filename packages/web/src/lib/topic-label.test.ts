import { describe, expect, it } from "vitest";
import {
  buildTopicPathLookup,
  getCollapsedTopicLabel,
} from "@/lib/topic-label";
import type { TopicTree } from "@/hooks/use-topics";

function makeTopic(
  overrides: Partial<TopicTree>,
  children: TopicTree[] = [],
): TopicTree {
  return {
    id: "topic-root",
    owner_id: "user_1",
    hub_id: "hub_1",
    parent_id: null,
    name: "Root",
    description: "",
    icon: "folder",
    position: 0,
    pinned: false,
    user_modified: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    memory_count: 0,
    total_memory_count: 0,
    children,
    ...overrides,
  };
}

describe("topic label", () => {
  it("builds a full path lookup from the topic tree", () => {
    const leaf = makeTopic({
      id: "leaf",
      parent_id: "child",
      name: "Migrations",
    });
    const child = makeTopic(
      { id: "child", parent_id: "root", name: "Postgres" },
      [leaf],
    );
    const root = makeTopic({ id: "root", name: "Engineering" }, [child]);

    const lookup = buildTopicPathLookup([root]);

    expect(lookup.get("leaf")).toEqual([
      { id: "root", name: "Engineering" },
      { id: "child", name: "Postgres" },
      { id: "leaf", name: "Migrations" },
    ]);
  });

  it("collapses deep paths to the tail segments", () => {
    const collapsed = getCollapsedTopicLabel([
      { name: "Engineering" },
      { name: "Backend" },
      { name: "Database" },
      { name: "Postgres" },
      { name: "Migrations" },
    ]);

    expect(collapsed.ellipsis).toBe(true);
    expect(collapsed.visible).toEqual([
      { name: "Postgres" },
      { name: "Migrations" },
    ]);
    expect(collapsed.fullPath).toBe(
      "Engineering \u203a Backend \u203a Database \u203a Postgres \u203a Migrations",
    );
  });
});
