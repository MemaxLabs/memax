// @vitest-environment jsdom

import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";

// Renderers navigate on drill-down (memory detail / topic page); the
// test tree has no app router mounted.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

// Side-effect: registers the Lane A renderers (same import BoardView uses).
import "./board-kinds";
import {
  hasBoardKindRenderer,
  renderBoardSlotBody,
} from "./board-kind-registry";

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "z-activity",
    kind: "activity",
    title: "fallback title (must not render)",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("lane A board kind renderers", () => {
  afterEach(cleanup);

  it("registers the two lane A kinds and no longer the retired ones", () => {
    // Counts folded into one `activity` strip; only the capsule — which
    // surfaces real content rather than a number — still earns a card.
    expect(hasBoardKindRenderer("activity")).toBe(true);
    expect(hasBoardKindRenderer("capsule")).toBe(true);
    for (const retired of ["trace", "pulse", "week"]) {
      expect(hasBoardKindRenderer(retired)).toBe(false);
    }
  });

  it("activity folds agents, topics and the week diff into one body", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            payload: {
              window_hours: 24,
              agents: [
                {
                  slug: "claude-code",
                  count: 3,
                  latest_title: "Chose River over Redis",
                },
                { slug: "", count: 1, latest_title: "A manual note" },
              ],
              topics: [{ topic_id: "t1", name: "部署", recent_count: 5 }],
              this_week: 1,
              last_week: 4,
            },
          }),
        )}
      </div>,
    );
    // Agent attribution resolves through the identity tokens.
    expect(screen.getByText("Claude Code")).toBeTruthy();
    expect(screen.getByText("3 memories")).toBeTruthy();
    expect(screen.getByText("Latest: “Chose River over Redis”")).toBeTruthy();
    expect(screen.getByText("Captured by hand")).toBeTruthy();
    // Topic movement and the week diff live in the same body now.
    expect(screen.getByText(/部署 \(5\)/)).toBeTruthy();
    expect(screen.getByText(/1 memory this week/)).toBeTruthy();
    expect(screen.getByText(/Last week: 4/)).toBeTruthy();
    // The slot's fallback title never leaks into a dedicated renderer.
    expect(screen.queryByText("fallback title (must not render)")).toBeNull();
  });

  it("retired kinds fall through to the literal-text fallback", () => {
    // Boards written by the previous version keep rendering rather
    // than disappearing, until the producer replaces their slots.
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "d-week",
            kind: "week",
            title: "This week: 12 memories",
            payload: { description: "legacy card" },
          }),
        )}
      </div>,
    );
    expect(screen.getByText("This week: 12 memories")).toBeTruthy();
    expect(screen.getByText("legacy card")).toBeTruthy();
  });
});
