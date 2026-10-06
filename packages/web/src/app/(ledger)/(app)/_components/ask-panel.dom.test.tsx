// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import type { AskSource } from "@/lib/v2/data/types";
import { LedgerDataProvider } from "../_lib/data";
import type { AskState } from "../_lib/use-ask";
import { AskPanel } from "./ask-panel";

// Ask's panel (Ask.png, States, States2): a citation shows its memory on
// hover or focus and opens it; the seal stamps once the answer is kept;
// answers off and the plan limit say so in one line.

afterEach(cleanup);

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

const river: AskSource = {
  n: 1,
  statement: "Background jobs run on River.",
  state: "kept",
  receipt: {
    person: "ZZ",
    action: "kept",
    at: "2026-10-02T17:12:00Z",
    ref: "M-0219",
  },
  memory: "M-0219",
  section: "decisions",
};
const temporal: AskSource = {
  n: 2,
  statement: "Temporal was dropped in August.",
  state: "kept",
  receipt: {
    agent: "claude-code",
    action: "kept",
    at: "2026-08-21T10:00:00Z",
    ref: "M-0230",
  },
  memory: "M-0230",
};

const answered: AskState = {
  status: "done",
  question: "Why River?",
  sources: [river, temporal],
  parts: [
    { kind: "text", text: "Jobs run on River." },
    { kind: "cite", n: 1 },
    { kind: "text", text: " Temporal was dropped." },
    { kind: "cite", n: 2 },
  ],
};

function renderPanel(
  state: AskState,
  extra: { kept?: { ref: string; at: Date } | null } = {},
) {
  const onOpen = vi.fn();
  const onKeep = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <LedgerDataProvider mode="demo">
            <AskPanel
              state={state}
              space={v2}
              viewer={{ initials: "ZZ", name: "Ziyang", timeZone: "UTC" }}
              keepKey="⌘↵"
              keeping={false}
              kept={extra.kept ?? null}
              onKeep={onKeep}
              onCopy={vi.fn()}
              onRemember={vi.fn()}
              onRetry={vi.fn()}
              onOpen={onOpen}
            />
          </LedgerDataProvider>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
  return { onOpen, onKeep };
}

describe("Ask's panel", () => {
  it("links each citation to its memory, and marks its source on hover and focus", () => {
    const { onOpen } = renderPanel(answered);
    const links = screen.getAllByRole("link", { name: /Source 1/ });
    const inAnswer = links[0]!;
    expect(inAnswer.getAttribute("href")).toBe("/memax-v2/memories/M-0219");
    expect(inAnswer.getAttribute("title")).toBe(
      "M-0219 · Background jobs run on River.",
    );
    const row = () => document.querySelectorAll("li")[0]!;
    expect(row().hasAttribute("data-active")).toBe(false);
    fireEvent.mouseEnter(inAnswer.closest("span.mx-cite-wrap")!.parentElement!);
    expect(row().hasAttribute("data-active")).toBe(true);
    fireEvent.mouseLeave(inAnswer.closest("span.mx-cite-wrap")!.parentElement!);
    expect(row().hasAttribute("data-active")).toBe(false);
    fireEvent.focus(inAnswer);
    expect(row().hasAttribute("data-active")).toBe(true);
    fireEvent.click(inAnswer);
    expect(onOpen).toHaveBeenCalled();
    // The label, and the same words for screen readers once it's done.
    expect(
      screen.getAllByText("Answer from memax-v2 · 2 sources"),
    ).toHaveLength(2);
  });

  it("keeps with ⌘↵ shown on the button, then stamps the seal", () => {
    const { onKeep } = renderPanel(answered);
    fireEvent.click(screen.getByRole("button", { name: /Keep as memory/ }));
    expect(onKeep).toHaveBeenCalled();
    cleanup();
    renderPanel(answered, {
      kept: { ref: "M-0500", at: new Date("2026-10-06T12:00:00Z") },
    });
    expect(screen.queryByRole("button", { name: /Keep as memory/ })).toBeNull();
    expect(screen.getByRole("img", { name: /M-0500/ }).className).toContain(
      "is-stamping",
    );
  });

  it("says when answers are off, and still shows what matched", () => {
    renderPanel({ status: "off", question: "Why?", sources: [river] });
    expect(screen.getByText(/Answers are off on this server/)).toBeTruthy();
    expect(screen.getByText("Background jobs run on River.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Keep as memory/ })).toBeNull();
  });

  it("says the plan limit and when asks start again", () => {
    renderPanel({
      status: "limit",
      question: "Why?",
      limit: 50,
      resetAt: "2026-11-01T00:00:00Z",
    });
    expect(screen.getByText(/asked 50 questions this month/)).toBeTruthy();
    expect(screen.getByText(/start again on Nov 1/)).toBeTruthy();
  });
});
