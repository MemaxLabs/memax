// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, cleanup, fireEvent, screen } from "@testing-library/react";
import type { TopicTree } from "memax-sdk";

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      topics: {
        moveTopic: "Move topic",
        noOtherTopics: "No other topics",
      },
    },
  }),
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("./topic-icon", () => ({
  TopicIcon: ({ className }: { className?: string }) => (
    <span data-testid="topic-icon" className={className} />
  ),
}));

// Defer import so mocks apply.
// eslint-disable-next-line import/first
import { TopicMovePicker } from "./topic-move-picker";

function node(
  id: string,
  name: string,
  children: TopicTree[] = [],
  parent_id: string | null = null,
): TopicTree {
  return {
    id,
    owner_id: "u1",
    hub_id: "h1",
    parent_id,
    name,
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

// Forest:
//   root
//    ├─ parent
//    │   └─ child
//    └─ cousin
//   other (sibling of root)
function makeForest(): TopicTree[] {
  return [
    node("root", "Root", [
      node("parent", "Parent", [node("child", "Child", [], "parent")], "root"),
      node("cousin", "Cousin", [], "root"),
    ]),
    node("other", "Other"),
  ];
}

describe("TopicMovePicker", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    cleanup();
  });

  const HUB_NAME = "Primary";

  function renderPicker(opts: {
    movingId: string;
    excluded: string[];
    onSelect?: (parentId: string | null, name?: string) => void;
    startsAtRoot?: boolean;
    hubName?: string;
  }) {
    const forest = makeForest();
    const moving = findNode(forest, opts.movingId)!;
    // Force the "already at root" fixture by nulling parent_id even if the
    // forest wired it up as root already.
    if (opts.startsAtRoot) {
      moving.parent_id = null;
    }
    const onSelect = opts.onSelect ?? vi.fn();
    render(
      <TopicMovePicker
        topic={moving}
        hubId="h1"
        hubName={opts.hubName ?? HUB_NAME}
        forest={forest}
        excludedIds={new Set(opts.excluded)}
        onSelect={onSelect}
      />,
    );
    return { onSelect };
  }

  function findNode(forest: TopicTree[], id: string): TopicTree | null {
    for (const n of forest) {
      if (n.id === id) return n;
      const deeper = findNode(n.children, id);
      if (deeper) return deeper;
    }
    return null;
  }

  it("shows the current hub name as the root option when the moving topic is nested", () => {
    renderPicker({ movingId: "child", excluded: ["child"] });
    expect(screen.getByText(HUB_NAME)).toBeTruthy();
  });

  it("hides the hub-root option when the moving topic is already at root", () => {
    renderPicker({
      movingId: "other",
      excluded: ["other"],
      startsAtRoot: true,
    });
    expect(screen.queryByText(HUB_NAME)).toBeNull();
  });

  it("hides the hub-root option when hubName is empty (auth not yet hydrated)", () => {
    // Defensive case per Codex review: empty hubName should suppress the
    // root entry rather than emit an unlabelled destination.
    renderPicker({ movingId: "child", excluded: ["child"], hubName: "" });
    expect(screen.queryByText(HUB_NAME)).toBeNull();
  });

  it("excludes self and descendants from the destination list", () => {
    // Moving Parent — excluded set = {Parent, Child}
    renderPicker({ movingId: "parent", excluded: ["parent", "child"] });
    // Destinations should include Root, Cousin, Other — NOT Parent or Child.
    expect(screen.getByText("Root")).toBeTruthy();
    expect(screen.getByText("Cousin")).toBeTruthy();
    expect(screen.getByText("Other")).toBeTruthy();
    expect(screen.queryByText("Parent")).toBeNull();
    expect(screen.queryByText("Child")).toBeNull();
  });

  it("filters destinations by the search input", () => {
    renderPicker({ movingId: "child", excluded: ["child"] });
    const input = screen.getByPlaceholderText("Move topic") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "cous" } });
    expect(screen.getByText("Cousin")).toBeTruthy();
    expect(screen.queryByText("Other")).toBeNull();
    expect(screen.queryByText("Parent")).toBeNull();
  });

  it("calls onSelect with parentId and name when a destination is clicked", () => {
    const onSelect = vi.fn();
    renderPicker({ movingId: "child", excluded: ["child"], onSelect });
    fireEvent.click(screen.getByText("Cousin"));
    expect(onSelect).toHaveBeenCalledWith("cousin", "Cousin");
  });

  it("calls onSelect(null, hubName) when the hub-root option is clicked", () => {
    // Root destination carries the hub name through as parentName so
    // the caller's success-message logic can format "Moved {name} to
    // {hub}." without re-resolving it.
    const onSelect = vi.fn();
    renderPicker({ movingId: "child", excluded: ["child"], onSelect });
    fireEvent.click(screen.getByText(HUB_NAME));
    expect(onSelect).toHaveBeenCalledWith(null, HUB_NAME);
  });

  // Note: Escape / click-outside dismissal are owned by the Popover
  // primitive that wraps this picker in TopicTreeNode (Codex review).
  // They're no longer the picker's responsibility, so the previous
  // "closes on Escape" test was removed with the listeners. The
  // integration-level popover behavior is covered in the tree-node
  // smoke flow.

  it("shows a friendly empty state when the filter matches nothing", () => {
    renderPicker({ movingId: "child", excluded: ["child"] });
    const input = screen.getByPlaceholderText("Move topic") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "zzzz" } });
    expect(screen.getByText("No other topics")).toBeTruthy();
  });
});
