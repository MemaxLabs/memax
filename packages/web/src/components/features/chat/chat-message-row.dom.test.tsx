// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { ChatMessage } from "memax-sdk";
import { ChatMessageRow } from "./chat-message-row";

// Unmount the React tree before the jsdom env tears down so
// framer-motion's deferred animations can't reference `window`
// after teardown (otherwise vitest reports "ReferenceError:
// window is not defined" as an unhandled error and exits non-zero
// even though all assertions passed).
afterEach(() => {
  cleanup();
});

function assistantMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: "msg_1",
    session_id: "sess_1",
    sequence: 1,
    role: "assistant",
    author_user_id: "user_1",
    status: "completed",
    scope_hub_ids_used: ["hub_1"],
    content: "Here is the final answer.",
    // Tool result is JSON-stringified RecallResult[] on the wire,
    // so the pill can derive a "Searched N memories" summary.
    tool_calls: [
      {
        id: "tool_1",
        name: "recall_memories",
        args: { query: "demo" },
        result: JSON.stringify([
          { memory_id: "m_a", title: "Alpha", hub_id: "hub_1" },
          { memory_id: "m_b", title: "Beta", hub_id: "hub_1" },
        ]),
      },
    ],
    citations: [],
    usage: {},
    toolset_version_used: 1,
    created_at: "2026-04-29T00:00:00Z",
    ...overrides,
  };
}

describe("ChatMessageRow", () => {
  it("renders the thinking pill above the assistant content", () => {
    render(<ChatMessageRow message={assistantMessage()} />);

    // The morphing thinking pill summarizes the recall_memories
    // call as "Searched 2 memories" — and sits above the answer
    // body so the user reads "what work was done" before the
    // answer itself.
    const pill = screen.getByText("Searched 2 memories");
    const answer = screen.getByText("Here is the final answer.");

    expect(pill).not.toBeNull();
    expect(answer).not.toBeNull();

    const order =
      pill.compareDocumentPosition(answer) & Node.DOCUMENT_POSITION_FOLLOWING;
    expect(order).toBeTruthy();
  });

  it("renders linkable source pills beneath the assistant content", () => {
    const { container } = render(
      <ChatMessageRow message={assistantMessage()} />,
    );

    // Each source pill is an <a> whose href is the slug-less
    // /memories/<id> path — the app middleware handles cross-hub
    // routing from the memory id alone, so we don't need to
    // resolve the hub slug client-side.
    const links = Array.from(
      container.querySelectorAll<HTMLAnchorElement>('a[href^="/memories/"]'),
    );
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/memories/m_a",
      "/memories/m_b",
    ]);
    // The pill body shows the memory title.
    expect(links[0].textContent).toContain("Alpha");
    expect(links[1].textContent).toContain("Beta");
  });
});
