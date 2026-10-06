// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { LedgerProvider } from "../i18n/provider";
import type { Autonomy } from "../lib/types";
import { AgentListHead, AgentRow } from "./agent-row";
import { DecisionGate } from "./decision-gate";
import { HandoffSlip } from "./handoff-slip";
import { SyncTarget } from "./sync-target";

const OPTIONS = [
  {
    label: "Fly.io, iad and ams",
    detail: "Matches the current API and workers.",
  },
  { label: "Railway", detail: "Simpler preview environments." },
  { label: "Decide later", detail: "Codex continues behind a flag." },
];

describe("DecisionGate", () => {
  it("asks, names the agent, and offers numbered options as a radio group", () => {
    render(
      <DecisionGate
        agent="codex"
        time="2 min ago"
        space="memax-v2"
        question="Which deploy target should the v2 API use?"
        options={OPTIONS}
      />,
    );
    expect(screen.getByText("Codex is waiting on you")).toBeTruthy();
    const group = screen.getByRole("radiogroup", {
      name: "Which deploy target should the v2 API use?",
    });
    const radios = within(group).getAllByRole("radio");
    expect(radios).toHaveLength(3);
    expect(radios.map((r) => r.getAttribute("aria-keyshortcuts"))).toEqual([
      "1",
      "2",
      "3",
    ]);
    expect(radios[0]!.tabIndex).toBe(0);
    // No stray space before the punctuation (design review §2).
    expect(
      screen.getByText("Your answer is kept in memax-v2, authored by you."),
    ).toBeTruthy();
  });

  it("keeps Answer unavailable, with the reason, until an option is chosen", () => {
    const onAnswer = vi.fn();
    render(
      <DecisionGate
        agent="codex"
        question="Which?"
        options={OPTIONS}
        onAnswer={onAnswer}
      />,
    );
    const answer = screen.getByRole("button", { name: "Answer" });
    expect(answer.getAttribute("aria-disabled")).toBe("true");
    expect(answer.title).toBe("Choose 1, 2 or 3");
    fireEvent.click(answer);
    expect(onAnswer).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("radio", { name: /Railway/ }));
    expect(answer.hasAttribute("aria-disabled")).toBe(false);
    fireEvent.click(answer);
    expect(onAnswer).toHaveBeenCalledWith(1);
  });

  it("chooses with arrows and digits, and never while an IME composes", () => {
    const onSelect = vi.fn();
    render(
      <DecisionGate
        agent="codex"
        question="Which?"
        options={OPTIONS}
        defaultSelected={0}
        onSelect={onSelect}
      />,
    );
    const radios = screen.getAllByRole("radio");
    radios[0]!.focus();
    fireEvent.keyDown(radios[0]!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(radios[1]);
    fireEvent.keyDown(radios[1]!, { key: "3" });
    expect(document.activeElement).toBe(radios[2]);
    expect(radios[2]!.getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(radios[2]!, { key: "1", isComposing: true });
    fireEvent.keyDown(radios[2]!, { key: "9" });
    expect(onSelect.mock.calls.map(([i]) => i)).toEqual([1, 2]);
  });

  it("speaks Chinese, including the list in the reason", () => {
    render(
      <LedgerProvider locale="zh">
        <DecisionGate agent="codex" question="用哪个？" options={OPTIONS} />
      </LedgerProvider>,
    );
    expect(screen.getByText("Codex 在等你")).toBeTruthy();
    expect(screen.getByRole("button", { name: "回答" }).title).toBe(
      "先选 1、2或3",
    );
    expect(screen.getByText("你的回答会以你的名义保留。")).toBeTruthy();
  });
});

describe("AgentRow", () => {
  it("shows who it is, its autonomy, and this week's numbers with their column names", () => {
    render(
      <div className="mx-panel">
        <AgentListHead />
        <AgentRow
          agent="claude-code"
          autonomy="write"
          reads={1204}
          writes={38}
          lastSeen="2 min ago"
          target="CLAUDE.md"
        />
      </div>,
    );
    const row = screen.getByRole("group", { name: "Claude Code" });
    expect(within(row).getByText("CLI").className).toBe("mx-stamp-surface");
    const autonomy = within(row).getByRole("radiogroup", {
      name: "Autonomy for Claude Code",
    });
    expect(
      within(autonomy)
        .getByRole("radio", { name: "Write" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(within(autonomy).getByRole("radio", { name: "Read" }).title).toBe(
      "Reads context. Never writes.",
    );
    const nums = row.querySelectorAll(".mx-agent-num");
    expect(nums[0]?.textContent).toBe("Reads in 7 days: 1,204");
    expect(nums[1]?.textContent).toBe("Writes in 7 days: 38");
    expect(row.querySelector(".mx-agent-target")?.textContent).toBe(
      "Compiles to CLAUDE.md",
    );
    // The head is visual only: each value labels itself.
    expect(
      document.querySelector(".mx-agent-head")?.getAttribute("aria-hidden"),
    ).toBe("true");
  });

  it("changes autonomy from the keyboard through the caller", () => {
    function Harness() {
      const [autonomy, setAutonomy] = useState<Autonomy>("propose");
      return (
        <AgentRow
          agent="codex"
          autonomy={autonomy}
          onAutonomyChange={setAutonomy}
          reads={512}
          writes={21}
        />
      );
    }
    render(<Harness />);
    const propose = screen.getByRole("radio", { name: "Propose" });
    propose.focus();
    fireEvent.keyDown(propose, { key: "ArrowRight" });
    expect(
      screen.getByRole("radio", { name: "Write" }).getAttribute("aria-checked"),
    ).toBe("true");
    expect(document.activeElement).toBe(
      screen.getByRole("radio", { name: "Write" }),
    );
  });

  it("shows a paused agent, an agent never seen, and MCP only", () => {
    const { container, rerender } = render(
      <AgentRow agent="gemini" autonomy="read" reads={0} writes={0} paused />,
    );
    expect(container.querySelector(".mx-agent")?.className).toContain(
      "is-paused",
    );
    expect(screen.getByText("Paused")).toBeTruthy();
    expect(screen.getByText("MCP only")).toBeTruthy();
    rerender(<AgentRow agent="gemini" autonomy="read" reads={0} writes={0} />);
    expect(container.querySelector(".mx-agent-seen")?.textContent).toBe(
      "—Not seen yet",
    );
  });

  it("greys out levels it can't take, with the reason, and skips them", () => {
    const onChange = vi.fn();
    render(
      <AgentRow
        agent="codex"
        autonomy="read"
        onAutonomyChange={onChange}
        unavailable={{ write: "API keys propose at most." }}
        reads={3}
        writes={1}
      />,
    );
    const write = screen.getByRole("radio", { name: "Write" });
    expect(write.getAttribute("aria-disabled")).toBe("true");
    expect(write.hasAttribute("disabled")).toBe(false);
    expect(write.title).toBe("API keys propose at most.");
    fireEvent.click(write);
    expect(onChange).not.toHaveBeenCalled();
    const read = screen.getByRole("radio", { name: "Read" });
    read.focus();
    fireEvent.keyDown(read, { key: "ArrowRight" });
    expect(document.activeElement).toBe(
      screen.getByRole("radio", { name: "Propose" }),
    );
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    // Past Propose the arrow wraps round to Read: Write is never focused or chosen.
    expect(document.activeElement).toBe(read);
    expect(onChange.mock.calls.map(([v]) => v)).toEqual(["propose"]);
  });

  it("links to the agent's page, names counts not recorded, and says why there's no file", () => {
    const { container } = render(
      <AgentRow
        agent="cursor"
        name="Cursor (laptop)"
        autonomy="read"
        reads={null}
        writes={0}
        href="/memax-v2/agents/c1"
        noTarget="Not compiling yet"
      />,
    );
    const link = screen.getByRole("link", { name: /Cursor \(laptop\)/ });
    expect(link.getAttribute("href")).toBe("/memax-v2/agents/c1");
    expect(link.className).toBe("mx-agent-link");
    expect(container.querySelector(".mx-agent-num")?.textContent).toBe(
      "Reads in 7 days: —Not recorded yet",
    );
    expect(screen.getByText("Not compiling yet")).toBeTruthy();
    expect(screen.queryByText("MCP only")).toBeNull();
  });
});

describe("SyncTarget", () => {
  it.each([
    ["synced", "In sync", "mx-state--kept"],
    ["drifted", "Drifted", "mx-state--proposed"],
    ["pending", "Compiling", "mx-state--working"],
    ["off", "Off", "mx-state--off"],
  ] as const)("shows %s as %s", (status, word, mark) => {
    const { container } = render(
      <SyncTarget
        path=".cursor/rules/memax.mdc"
        tool="Cursor"
        status={status}
      />,
    );
    expect(screen.getByText(word).closest(".mx-state")?.className).toContain(
      mark,
    );
    expect(container.querySelector(".mx-target")?.className).toContain(
      `is-${status}`,
    );
    expect(
      container.querySelector(".mx-target-path")?.getAttribute("title"),
    ).toBe(".cursor/rules/memax.mdc");
  });
});

describe("HandoffSlip", () => {
  const base = {
    id: "H-0093",
    from: "claude-code",
    to: "codex",
    toSurface: "cloud task",
    title: "Finish the MCP 2026-07-28 migration",
    done: ["Dropped the initialize handshake."],
    next: ["Return input_required from memax_push."],
    questions: ["Which deploy target for v2?"],
    carries: "7 memories",
    time: "sent 14:20",
  };

  it("routes from one agent to the next, with done, next and the open questions", () => {
    render(<HandoffSlip {...base} status="sent" />);
    expect(
      screen.getByRole("heading", {
        level: 3,
        name: "Finish the MCP 2026-07-28 migration",
      }),
    ).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 4 }).map((h) => h.textContent),
    ).toEqual(["Done", "Next", "Open questions"]);
    expect(screen.getByText("With Codex")).toBeTruthy();
    // Monograms are hidden from assistive technology; names and "to" are read.
    const route = document.querySelector(".mx-slip-route")!;
    expect(
      [...route.querySelectorAll(".mx-stamp")].every((s) =>
        s.getAttribute("aria-hidden"),
      ),
    ).toBe(true);
    expect(route.textContent).toBe("CCClaude CodetoCXCodexcloud task");
    expect(screen.getByText("H-0093 · 7 memories · sent 14:20")).toBeTruthy();
  });

  it.each([
    ["drafted", "Draft", "mx-state--off"],
    ["accepted", "Accepted", "mx-state--kept"],
  ] as const)("shows %s", (status, word, mark) => {
    render(<HandoffSlip {...base} status={status} />);
    expect(screen.getByText(word).closest(".mx-state")?.className).toContain(
      mark,
    );
  });
});
