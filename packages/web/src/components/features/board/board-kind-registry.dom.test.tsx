// @vitest-environment jsdom

import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";
import type { Translations } from "@/i18n/locales/en";
import {
  hasBoardKindRenderer,
  registerBoardKind,
  renderBoardSlotBody,
} from "./board-kind-registry";

const t = {} as unknown as Translations;

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
          t,
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
      <div>
        {renderBoardSlotBody(slot({ payload: { description: 42 } }), t)}
      </div>,
    );
    expect(screen.getByText("A card from the future")).toBeTruthy();
    expect(screen.queryByText("42")).toBeNull();
  });

  it("prefers a registered renderer over the fallback", () => {
    expect(hasBoardKindRenderer("trace")).toBe(false);
    registerBoardKind("trace", {
      body: (s) => <p>custom renderer for {s.kind}</p>,
    });
    expect(hasBoardKindRenderer("trace")).toBe(true);

    render(<div>{renderBoardSlotBody(slot({ kind: "trace" }), t)}</div>);
    expect(screen.getByText("custom renderer for trace")).toBeTruthy();
  });
});
