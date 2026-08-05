// @vitest-environment jsdom

import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";
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
    slot_key: "a-trace",
    kind: "trace",
    title: "fallback title (must not render)",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("lane A board kind renderers", () => {
  afterEach(cleanup);

  it("registers all four lane A kinds", () => {
    for (const kind of ["trace", "pulse", "capsule", "week"]) {
      expect(hasBoardKindRenderer(kind)).toBe(true);
    }
  });

  it("trace groups by agent with identity display names", () => {
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
            },
          }),
        )}
      </div>,
    );
    expect(screen.getByText("Claude Code")).toBeTruthy();
    expect(screen.getByText("3 memories")).toBeTruthy();
    expect(screen.getByText("Chose River over Redis")).toBeTruthy();
    // Unattributed rows get the manual-capture label, and the slot's
    // fallback title never leaks into a dedicated renderer.
    expect(screen.getByText("Captured by hand")).toBeTruthy();
    expect(screen.queryByText("fallback title (must not render)")).toBeNull();
  });

  it("week renders singular and comparison lines", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "d-week",
            kind: "week",
            payload: { this_week: 1, last_week: 4 },
          }),
        )}
      </div>,
    );
    expect(screen.getByText("1 memory this week")).toBeTruthy();
    expect(screen.getByText("Last week: 4")).toBeTruthy();
  });

  it("pulse lists topics with recent counts", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "b-pulse",
            kind: "pulse",
            payload: {
              window_days: 7,
              topics: [
                {
                  topic_id: "t1",
                  name: "部署",
                  recent_count: 5,
                  contributors: 2,
                },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(screen.getByText("部署")).toBeTruthy();
    expect(screen.getByText("5 new memories")).toBeTruthy();
    expect(screen.getByText("2 people")).toBeTruthy();
  });
});
