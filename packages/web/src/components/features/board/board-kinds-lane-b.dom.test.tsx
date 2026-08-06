// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";
// Side-effect: registers the Lane B renderers (chained from the same
// board-kinds import BoardView uses; imported directly here so the test
// exercises exactly the module under test). The vi.mock calls below are
// hoisted above this import by vitest, so the renderers see the mocks.
import "./board-kinds-lane-b";
import {
  hasBoardKindRenderer,
  renderBoardSlotBody,
} from "./board-kind-registry";

const { resolveMutate } = vi.hoisted(() => ({
  resolveMutate: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-board", () => ({
  useResolveBoardSlot: () => ({ mutate: resolveMutate }),
}));

vi.mock("@/lib/auth", () => ({
  useActiveHub: () => ({
    activeHub: undefined,
    hubFilter: "h1",
    isTeamHub: false,
  }),
}));

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "0-dream",
    kind: "dreamlog",
    title: "fallback title (must not render)",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("lane B board kind renderers", () => {
  afterEach(() => {
    cleanup();
    resolveMutate.mockClear();
  });

  it("registers all seven lane B kinds", () => {
    for (const kind of [
      "dreamlog",
      "echo",
      "thread",
      "openq",
      "pattern",
      "musing",
      "decision_gate",
    ]) {
      expect(hasBoardKindRenderer(kind)).toBe(true);
    }
  });

  it("echo renders both quotes with then/now eyebrow suffixes", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "echo",
            payload: {
              body: "An old question found its answer.",
              then: {
                memory_id: "m-then",
                when: "2026-04-01T00:00:00Z",
                excerpt: "Should staging deploys stay manual?",
              },
              now: {
                memory_id: "m-now",
                when: "2026-08-04T00:00:00Z",
                excerpt: "CI deploys staging after every main merge.",
              },
            },
          }),
        )}
      </div>,
    );
    expect(
      screen.getByText("“Should staging deploys stay manual?”"),
    ).toBeTruthy();
    expect(
      screen.getByText("“CI deploys staging after every main merge.”"),
    ).toBeTruthy();
    expect(screen.getByText(/you asked yourself/)).toBeTruthy();
    expect(screen.getByText(/your answer/)).toBeTruthy();
    expect(screen.getByText("An old question found its answer.")).toBeTruthy();
    expect(screen.queryByText("fallback title (must not render)")).toBeNull();
  });

  it("decision_gate renders the question and fires choose with the option id", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "2g-abc",
            kind: "decision_gate",
            payload: {
              question: "Merge the schema now or after launch?",
              context: "Both branches are green.",
              source_agent: "claude-code",
              options: [
                { id: "opt-now", label: "Merge now" },
                { id: "opt-later", label: "Wait until after launch" },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(
      screen.getByText("Merge the schema now or after launch?"),
    ).toBeTruthy();
    expect(screen.getByText("Both branches are green.")).toBeTruthy();
    const later = screen.getByText("Wait until after launch");
    expect(screen.getByText("Merge now")).toBeTruthy();
    fireEvent.click(later);
    expect(resolveMutate).toHaveBeenCalledWith({
      slotKey: "2g-abc",
      action: "choose",
      choice: "opt-later",
    });
  });

  it("decision_gate options are disabled once the slot is terminal", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "2g-abc",
            kind: "decision_gate",
            state: "resolved",
            payload: {
              question: "Merge the schema now or after launch?",
              options: [{ id: "opt-now", label: "Merge now" }],
            },
          }),
        )}
      </div>,
    );
    const button = screen.getByText("Merge now");
    fireEvent.click(button);
    expect(resolveMutate).not.toHaveBeenCalled();
  });

  it("wow kinds render the body plus quoted receipts", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "pattern",
            payload: {
              body: "Your best decisions land on Tuesday mornings.",
              quotes: [
                {
                  memory_id: "m1",
                  when: "2026-07-07T09:00:00Z",
                  excerpt: "Chose River over Redis.",
                },
                {
                  memory_id: "m2",
                  when: "2026-07-14T09:30:00Z",
                  excerpt: "Killed the sidebar layout.",
                },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(
      screen.getByText("Your best decisions land on Tuesday mornings."),
    ).toBeTruthy();
    expect(screen.getByText("“Chose River over Redis.”")).toBeTruthy();
    expect(screen.getByText("“Killed the sidebar layout.”")).toBeTruthy();
  });
});
