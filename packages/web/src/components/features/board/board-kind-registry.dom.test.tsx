// @vitest-environment jsdom

import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";
import {
  hasBoardKindRenderer,
  registerBoardKind,
  renderBoardSlotBody,
} from "./board-kind-registry";

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "hero",
    kind: "mystery_kind",
    title: "A card from the future",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("board kind registry", () => {
  afterEach(cleanup);

  it("renders unknown kinds through the literal-text fallback", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            payload: { description: "Plain text survives old clients." },
          }),
        )}
      </div>,
    );
    // The plan-18 contract: title + description print literally, and
    // the raw kind is shown as the label so the card stays attributable.
    expect(screen.getByText("A card from the future")).toBeTruthy();
    expect(screen.getByText("Plain text survives old clients.")).toBeTruthy();
    expect(screen.getByText("mystery_kind")).toBeTruthy();
  });

  it("ignores non-string payload description in the fallback", () => {
    render(
      <div>{renderBoardSlotBody(slot({ payload: { description: 42 } }))}</div>,
    );
    expect(screen.getByText("A card from the future")).toBeTruthy();
    expect(screen.queryByText("42")).toBeNull();
  });

  it("prefers a registered renderer over the fallback", () => {
    // "custom_kind", not "trace": the Lane A renderers own the real
    // kind names via the board-kinds side-effect module.
    expect(hasBoardKindRenderer("custom_kind")).toBe(false);
    registerBoardKind("custom_kind", ({ slot: s }) => (
      <p>custom renderer for {s.kind}</p>
    ));
    expect(hasBoardKindRenderer("custom_kind")).toBe(true);

    render(<div>{renderBoardSlotBody(slot({ kind: "custom_kind" }))}</div>);
    expect(screen.getByText("custom renderer for custom_kind")).toBeTruthy();
  });
});
