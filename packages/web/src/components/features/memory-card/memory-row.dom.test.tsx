// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import type { Memory } from "@/hooks/use-memories";

let mockIsMobile = false;

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => mockIsMobile,
}));

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      toast: { untitled: "Untitled" },
      memoryView: { remembering: "Remembering…" },
      attribution: {
        you: "you",
        youPushed: "you pushed",
        pushed: "pushed",
        savedWithAgent: "saved with",
        capturedByAgent: "captured by",
      },
      topics: { dragHandleAria: "Drag {name}" },
      batch: { done: "Done", select: "Select" },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    user: { id: "user-1", display_name: "Me" },
    hubs: [
      {
        hub: { id: "hub-team", name: "Team Hub", hub_type: "team" },
      },
      {
        hub: { id: "hub-personal", name: "Personal", hub_type: "personal" },
      },
    ],
  }),
}));

vi.mock("@/contexts/selection-context", () => ({
  useSelection: () => ({
    active: false,
    selected: new Set<string>(),
    enter: () => {},
    exit: () => {},
    toggle: () => {},
  }),
}));

vi.mock("@/lib/query-client", () => ({
  queryClient: {
    prefetchQuery: () => Promise.resolve(),
  },
}));

vi.mock("@/hooks/use-memories", () => ({
  getMemoryQueryOptions: (id: string) => ({ queryKey: ["memory", id] }),
}));

vi.mock("@/lib/format-age", () => ({
  formatAge: () => "2h",
}));

vi.mock("@/components/features/dream-action-popover-body", () => ({
  DreamActionPopoverBody: () => null,
}));

// eslint-disable-next-line import/first
import { MemoryRow } from "./memory-row";

function makeMemory(overrides: Partial<Memory> = {}): Memory {
  return {
    id: "mem-1",
    title: "A memory",
    summary: "",
    content: "",
    content_type: "text",
    source: "web",
    source_agent: null,
    author_id: "user-other",
    author_name: "Derek",
    author_avatar_url: null,
    hub_id: "hub-team",
    hub_name: "Team Hub",
    topic_id: "topic-1",
    topic_name: null,
    confidence: null,
    access_count: 0,
    state: "ready",
    owner_id: "user-1",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  } as Memory;
}

// Right cluster is anchored via `data-testid="memory-row-right-cluster"` in
// production — a stable marker independent of utility class churn. We walk
// that container and classify each direct child into one of three buckets:
// topic chip (text match), actor avatar (img or single-letter fallback div),
// time ("2h" from the formatAge mock). Anything else is filtered out so the
// assertion ignores content-type badges and pills that may or may not render
// for a given fixture.
function getRightClusterChildren(root: HTMLElement): HTMLElement[] {
  const rightCluster = root.querySelector(
    '[data-testid="memory-row-right-cluster"]',
  );
  if (!rightCluster) throw new Error("right cluster not mounted");
  return Array.from(rightCluster.children) as HTMLElement[];
}

function classifySlot(el: HTMLElement, topicName: string): string {
  if (el.textContent?.includes(topicName)) return "topic";
  if (el.querySelector("img")) return "actor";
  if (
    el.tagName === "DIV" &&
    el.children.length === 0 &&
    el.textContent &&
    /^[A-Z]$/.test(el.textContent.trim())
  ) {
    return "actor";
  }
  // Avatar fallback renders as a <div> with a single letter; catches the
  // DY avatar when no author_avatar_url is present.
  if (el.querySelector(":scope > div")) {
    const inner = el.firstElementChild as HTMLElement | null;
    if (inner && /^[A-Z]$/.test(inner.textContent?.trim() ?? "")) {
      return "actor";
    }
  }
  if (el.textContent?.trim() === "2h") return "time";
  return "other";
}

afterEach(() => {
  cleanup();
  mockIsMobile = false;
});

describe("MemoryRow right-cluster ordering", () => {
  it("mobile fresh row renders topic pin BEFORE actor avatar", () => {
    mockIsMobile = true;
    const memory = makeMemory({ author_name: "Derek", hub_id: "hub-team" });
    const { container } = render(
      <MemoryRow
        memory={memory}
        surface="recent"
        topicLabel={{
          path: [{ id: "topic-1", name: "Pricing" }],
        }}
      />,
    );

    const children = getRightClusterChildren(container);
    const slots = children
      .map((el) => classifySlot(el, "Pricing"))
      .filter((s) => s === "topic" || s === "actor" || s === "time");

    expect(slots).toEqual(["topic", "actor", "time"]);
  });

  it("desktop non-recent surface renders topic pin AFTER actor avatar (unchanged)", () => {
    mockIsMobile = false;
    const memory = makeMemory({ author_name: "Derek", hub_id: "hub-team" });
    const { container } = render(
      <MemoryRow
        memory={memory}
        surface="inbox"
        topicLabel={{
          path: [{ id: "topic-1", name: "Pricing" }],
        }}
      />,
    );

    const children = getRightClusterChildren(container);
    const slots = children
      .map((el) => classifySlot(el, "Pricing"))
      .filter((s) => s === "topic" || s === "actor" || s === "time");

    expect(slots).toEqual(["actor", "topic", "time"]);
  });
});
