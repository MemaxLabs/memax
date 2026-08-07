// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";
// Side-effect: registers the Lane B renderers (chained from the same
// board-kinds import BoardView uses; imported directly here so the test
// exercises exactly the module under test). The vi.mock calls below are
// hoisted above this import by vitest, so the renderers see the mocks.
import "./board-kinds-lane-b";
import {
  boardKindOptions,
  hasBoardKindRenderer,
  renderBoardSlotBody,
} from "./board-kind-registry";

const { resolveMutate, trackEvent, toastSuccess, toastError } = vi.hoisted(
  () => ({
    resolveMutate: vi.fn(),
    trackEvent: vi.fn(),
    toastSuccess: vi.fn(),
    toastError: vi.fn(),
  }),
);

vi.mock("@/lib/posthog", () => ({ trackEvent }));

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError },
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

/** Two grounded items — the shape buildNextUpSlot ships server-side. */
function nextupSlot(): BoardSlot {
  return slot({
    slot_key: "0n-next",
    kind: "nextup",
    title: "Pick a backup strategy",
    payload: {
      items: [
        {
          title: "Pick a backup strategy",
          why: "You asked yourself and never answered.",
          quotes: [
            {
              memory_id: "m1",
              when: "2026-07-20T00:00:00Z",
              excerpt: "Where do backups actually live?",
            },
          ],
        },
        {
          title: "Finish the migration script",
          why: "You said tomorrow; that was three weeks ago.",
          quotes: [
            {
              memory_id: "m2",
              excerpt: "Writing the migration script tomorrow.",
            },
          ],
        },
      ],
    },
  });
}

const writeText = vi.fn((_text: string) => Promise.resolve());

describe("lane B board kind renderers", () => {
  beforeEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
  });

  afterEach(() => {
    cleanup();
    resolveMutate.mockClear();
    trackEvent.mockClear();
    toastSuccess.mockClear();
    toastError.mockClear();
    writeText.mockClear();
  });

  it("registers the live lane B kinds and not the retired ones", () => {
    for (const kind of [
      "dreamlog",
      "echo",
      "thread",
      "pattern",
      "decision_gate",
      "nextup",
    ]) {
      expect(hasBoardKindRenderer(kind)).toBe(true);
    }
    // openq collapsed into nextup (same open-loop signal, actionable);
    // musing was the loosest-defined kind and the likeliest Barnum source.
    for (const retired of ["openq", "musing"]) {
      expect(hasBoardKindRenderer(retired)).toBe(false);
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

  it("nextup renders numbered items with why lines and quoted receipts", () => {
    render(<div>{renderBoardSlotBody(nextupSlot())}</div>);
    // Numbered list of imperative titles.
    expect(screen.getByText("1")).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByText("Pick a backup strategy")).toBeTruthy();
    expect(screen.getByText("Finish the migration script")).toBeTruthy();
    // Why lines.
    expect(
      screen.getByText("You asked yourself and never answered."),
    ).toBeTruthy();
    // Per-item quotes render as clickable memory quotes.
    expect(screen.getByText("“Where do backups actually live?”")).toBeTruthy();
    expect(
      screen.getByText("“Writing the migration script tomorrow.”"),
    ).toBeTruthy();
  });

  it("nextup copies a per-item agent handoff prompt to the clipboard", async () => {
    render(<div>{renderBoardSlotBody(nextupSlot())}</div>);
    const buttons = screen.getAllByText("Copy handoff prompt");
    // One handoff per item — each is its own brief.
    expect(buttons).toHaveLength(2);

    fireEvent.click(buttons[1]);
    expect(trackEvent).toHaveBeenCalledWith("board_nextup_handoff_copied", {
      kind: "nextup",
      item_index: 1,
    });
    const prompt = writeText.mock.calls[0][0];
    expect(prompt).toContain("<task>\nFinish the migration script");
    expect(prompt).toContain('"Writing the migration script tomorrow."');
    expect(prompt).toContain("<success_criteria>");
    // A per-item copy hands over only that item.
    expect(prompt).not.toContain("Pick a backup strategy");
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
  });

  it("nextup offers a whole-card handoff only when there is more than one item", () => {
    render(<div>{renderBoardSlotBody(nextupSlot())}</div>);
    fireEvent.click(screen.getByText("Copy all 2 as one prompt"));
    expect(trackEvent).toHaveBeenCalledWith("board_nextup_handoff_copied", {
      kind: "nextup",
      item_index: -1,
    });
    const prompt = writeText.mock.calls[0][0];
    expect(prompt).toContain('<task index="1" of="2">');
    expect(prompt).toContain("Pick a backup strategy");
    expect(prompt).toContain("Finish the migration script");

    cleanup();
    const single = nextupSlot();
    const items = (single.payload!.items as unknown[]).slice(0, 1);
    render(
      <div>
        {renderBoardSlotBody({ ...single, payload: { items } } as BoardSlot)}
      </div>,
    );
    expect(screen.queryByText(/Copy all/)).toBeNull();
    expect(screen.getAllByText("Copy handoff prompt")).toHaveLength(1);
  });

  it("nextup is registered with feedback verbs and a relabeled ack", () => {
    const options = boardKindOptions("nextup");
    expect(options?.feedback).toBe(true);
    expect(
      options?.actions?.ack?.({ board: { nextupAck: "Done · got it" } }),
    ).toBe("Done · got it");
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

// Team-native kinds. These only reach a board because several people
// share the hub, so the DOM assertions are about attribution: who said
// which half of a 共识缺口, and who to go ask on a 谁知道这个.
describe("team-native board kind renderers", () => {
  afterEach(cleanup);

  it("registers the three team kinds with feedback verbs", () => {
    for (const kind of ["consensus_gap", "team_echo", "who_knows"]) {
      expect(hasBoardKindRenderer(kind)).toBe(true);
      expect(boardKindOptions(kind)?.feedback).toBe(true);
      expect(
        boardKindOptions(kind)?.strip?.(slot({ kind, title: "card title" }), {
          board: {
            kindConsensus: "Consensus gap",
            kindTeamEcho: "Team echo",
            kindWhoKnows: "Who knows this",
          },
        }).detail,
      ).toBe("card title");
    }
  });

  it("consensus_gap renders both sides attributed to their authors", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "consensus_gap",
            payload: {
              body: "You two are describing the same rollout differently.",
              sides: [
                {
                  memory_id: "m-wei",
                  when: "2026-06-01T00:00:00Z",
                  excerpt: "The rollout is behind a flag until Q4.",
                  author: "Wei",
                },
                {
                  memory_id: "m-lin",
                  when: "2026-07-02T00:00:00Z",
                  excerpt: "The rollout shipped to everyone last week.",
                  author: "Lin",
                },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(
      screen.getByText("“The rollout is behind a flag until Q4.”"),
    ).toBeTruthy();
    expect(
      screen.getByText("“The rollout shipped to everyone last week.”"),
    ).toBeTruthy();
    expect(screen.getByText(/Wei/)).toBeTruthy();
    expect(screen.getByText(/Lin/)).toBeTruthy();
  });

  it("consensus_gap falls back to generic side labels without a roster", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "consensus_gap",
            payload: {
              body: "Same decision, two readings.",
              sides: [
                { memory_id: "m1", excerpt: "Locked." },
                { memory_id: "m2", excerpt: "Still open." },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(screen.getByText(/one member/)).toBeTruthy();
    expect(screen.getByText(/another member/)).toBeTruthy();
  });

  it("team_echo renders the question and the other member's answer", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "team_echo",
            payload: {
              body: "The hub already held this answer.",
              then: {
                memory_id: "m-q",
                when: "2026-02-01T00:00:00Z",
                excerpt: "Does anyone know why the worker retries twice?",
                author: "Wei",
              },
              now: {
                memory_id: "m-a",
                when: "2026-05-01T00:00:00Z",
                excerpt: "River retries once on its own before our handler.",
                author: "Lin",
              },
            },
          }),
        )}
      </div>,
    );
    expect(
      screen.getByText("“Does anyone know why the worker retries twice?”"),
    ).toBeTruthy();
    expect(
      screen.getByText("“River retries once on its own before our handler.”"),
    ).toBeTruthy();
    expect(screen.getByText(/Wei · asked/)).toBeTruthy();
    expect(screen.getByText(/Lin · answered/)).toBeTruthy();
  });

  it("who_knows names the holder above the evidence", () => {
    render(
      <div>
        {renderBoardSlotBody(
          slot({
            slot_key: "1-wow",
            kind: "who_knows",
            payload: {
              holder: "Wei",
              body: "Almost everything this hub knows about deploys is Wei's.",
              quotes: [
                { memory_id: "m1", excerpt: "Staging deploys run from CI." },
                { memory_id: "m2", excerpt: "Prod needs a manual approval." },
              ],
            },
          }),
        )}
      </div>,
    );
    expect(screen.getByText("Ask Wei")).toBeTruthy();
    expect(screen.getByText("“Staging deploys run from CI.”")).toBeTruthy();
    expect(screen.getByText("“Prod needs a manual approval.”")).toBeTruthy();
  });
});
