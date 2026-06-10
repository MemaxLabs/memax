// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import type { Memory } from "@/hooks/use-memories";

const memoryRowSpy = vi.fn();
const useDraggableMemorySpy = vi.fn();
let mockIsMobile = false;

vi.mock("./memory-row", () => ({
  MemoryRow: (props: Record<string, unknown>) => {
    memoryRowSpy(props);
    return <div data-testid="memory-row-stub" />;
  },
}));

vi.mock("../topic/topic-dnd-hooks", () => ({
  useDraggableMemory: (input: unknown) => {
    useDraggableMemorySpy(input);
    return {
      attributes: { role: "button" },
      listeners: { onKeyDown: () => {} },
      setNodeRef: (node: HTMLElement | null) => {
        (globalThis as { __lastDragRef?: HTMLElement | null }).__lastDragRef =
          node;
      },
      isDragging: false,
    };
  },
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => mockIsMobile,
}));

import { DraggableMemoryRow } from "./memory-row-draggable";

function makeMemory(overrides: Partial<Memory> = {}): Memory {
  return {
    id: "mem-1",
    title: "A memory",
    summary: "Summary",
    content: "",
    content_type: "text",
    source: "web",
    source_agent: null,
    author_id: "user-1",
    author_name: null,
    author_avatar_url: null,
    hub_id: "hub-1",
    hub_name: "Personal",
    topic_id: "topic-1",
    topic_name: null,
    confidence: null,
    access_count: 0,
    state: "ready",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  } as Memory;
}

describe("DraggableMemoryRow", () => {
  beforeEach(() => {
    memoryRowSpy.mockClear();
    useDraggableMemorySpy.mockClear();
    mockIsMobile = false;
  });
  afterEach(() => cleanup());

  it("forwards a memory with a topic_id to useDraggableMemory", () => {
    const memory = makeMemory({
      id: "mem-topic",
      hub_id: "hub-a",
      topic_id: "topic-abc",
      title: "With topic",
    });
    render(<DraggableMemoryRow memory={memory} surface="topic" />);

    expect(useDraggableMemorySpy).toHaveBeenCalledTimes(1);
    expect(useDraggableMemorySpy).toHaveBeenCalledWith({
      id: "mem-topic",
      title: "With topic",
      hubId: "hub-a",
      topicId: "topic-abc",
    });
  });

  it("passes topicId as undefined when memory has no topic_id (inbox/grid case)", () => {
    const memory = makeMemory({
      id: "mem-inbox",
      hub_id: "hub-a",
      topic_id: null as unknown as string,
      title: "Unassigned",
    });
    render(<DraggableMemoryRow memory={memory} surface="inbox" />);

    expect(useDraggableMemorySpy).toHaveBeenCalledWith({
      id: "mem-inbox",
      title: "Unassigned",
      hubId: "hub-a",
      topicId: undefined,
    });
  });

  it("forwards dragRef, dragAttributes, dragListeners, and isDragging to MemoryRow", () => {
    const memory = makeMemory();
    render(<DraggableMemoryRow memory={memory} surface="topic" />);

    expect(memoryRowSpy).toHaveBeenCalledTimes(1);
    const props = memoryRowSpy.mock.calls[0][0] as Record<string, unknown>;
    expect(props.memory).toBe(memory);
    expect(props.surface).toBe("topic");
    expect(typeof props.dragRef).toBe("function");
    expect(props.dragAttributes).toEqual({ role: "button" });
    expect(props.dragListeners).toMatchObject({
      onKeyDown: expect.any(Function),
    });
    expect(props.isDragging).toBe(false);
  });

  it("propagates extra props like showDivider and onClick", () => {
    const onClick = vi.fn();
    const memory = makeMemory();
    render(
      <DraggableMemoryRow
        memory={memory}
        surface="recent"
        showDivider
        onClick={onClick}
      />,
    );

    const props = memoryRowSpy.mock.calls[0][0] as Record<string, unknown>;
    expect(props.showDivider).toBe(true);
    expect(props.onClick).toBe(onClick);
  });

  // ── Gesture ownership (Codex review) ────────────────────────────────────

  it("does NOT forward drag props on mobile (grip should not render)", () => {
    mockIsMobile = true;
    const memory = makeMemory();
    render(<DraggableMemoryRow memory={memory} surface="topic" />);

    // Hook still runs unconditionally per Rules of Hooks...
    expect(useDraggableMemorySpy).toHaveBeenCalledTimes(1);

    // ...but MemoryRow must not receive any drag props on mobile, so the
    // absence-of-dragRef signal tells MemoryRow to skip rendering the grip.
    const props = memoryRowSpy.mock.calls[0][0] as Record<string, unknown>;
    expect(props.dragRef).toBeUndefined();
    expect(props.dragAttributes).toBeUndefined();
    expect(props.dragListeners).toBeUndefined();
    expect(props.isDragging).toBeUndefined();
  });

  it("forwards drag props on desktop (grip should render)", () => {
    mockIsMobile = false;
    const memory = makeMemory();
    render(<DraggableMemoryRow memory={memory} surface="topic" />);

    const props = memoryRowSpy.mock.calls[0][0] as Record<string, unknown>;
    expect(typeof props.dragRef).toBe("function");
    expect(props.dragAttributes).toEqual({ role: "button" });
    expect(props.dragListeners).toMatchObject({
      onKeyDown: expect.any(Function),
    });
    expect(props.isDragging).toBe(false);
  });
});
