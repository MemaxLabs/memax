// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { en } from "../i18n/en";
import { LedgerProvider } from "../i18n/provider";
import { zh } from "../i18n/zh";
import type { MarkState } from "../lib/types";
import { Button } from "../primitives/button";
import { Cite } from "../provenance/cite";
import { Highlight } from "../provenance/highlight";
import { Receipt } from "../provenance/receipt";
import { Diff } from "./diff";
import { DreamCard } from "./dream-card";
import { Lineage } from "./lineage";
import { MemoryList } from "./memory-list";
import { MemoryRow } from "./memory-row";
import { MemoryText } from "./memory-text";
import { Redaction } from "./redaction";
import { StateMark } from "./state-mark";

const STATES: MarkState[] = [
  "proposed",
  "kept",
  "merged",
  "stale",
  "faded",
  "conflict",
  "forgotten",
  "working",
  "off",
];

describe("StateMark", () => {
  it.each(STATES)("shows the word for %s in English and Chinese", (state) => {
    const { unmount } = render(<StateMark state={state} />);
    expect(screen.getByText(en.state[state]).className).toBe("mx-state-label");
    unmount();
    render(
      <LedgerProvider locale="zh">
        <StateMark state={state} />
      </LedgerProvider>,
    );
    expect(screen.getByText(zh.state[state])).toBeTruthy();
  });

  it.each(STATES)("keeps the word as the name of a bare %s glyph", (state) => {
    render(<StateMark state={state} label={false} />);
    const mark = screen.getByRole("img", { name: en.state[state] });
    expect(mark.getAttribute("title")).toBe(en.state[state]);
    expect(mark.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  it("takes a custom word", () => {
    render(<StateMark state="kept" label="In sync" />);
    expect(screen.getByText("In sync")).toBeTruthy();
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("has a distinct shape for every state", () => {
    const shapes = STATES.map((state) => {
      const { container, unmount } = render(
        <StateMark state={state} label={false} />,
      );
      const svg = container.querySelector("svg")!.innerHTML;
      unmount();
      return svg;
    });
    expect(new Set(shapes).size).toBe(STATES.length);
  });
});

describe("MemoryRow", () => {
  it("is a list item with the statement and a two-line receipt on the rail", () => {
    const ref = createRef<HTMLElement>();
    render(
      <MemoryList aria-label="Memories">
        <MemoryRow
          ref={ref}
          person="ZZ"
          name="You"
          action="kept"
          time="Oct 2"
          space="memax-v2"
          id="M-0219"
        >
          Background jobs run on River, not Temporal.
        </MemoryRow>
      </MemoryList>,
    );
    const list = screen.getByRole("list", { name: "Memories" });
    const item = within(list).getByRole("listitem");
    expect(ref.current).toBe(item);
    expect(item.className).toBe("mx-row is-kept");
    expect(
      within(item).getByText("Background jobs run on River, not Temporal.")
        .className,
    ).toBe("mx-row-text");
    const rail = item.querySelector(".mx-row-rail")!;
    const [first, second] = rail.querySelectorAll(".mx-receipt");
    expect(first?.textContent).toBe("kept·Oct 2");
    expect(second?.textContent).toBe("M-0219 · memax-v2");
    expect(second?.querySelector(".mx-row-rail-id")?.textContent).toBe(
      "M-0219",
    );
    expect(rail.getAttribute("title")).toBe(
      "You · kept · Oct 2 · M-0219 · memax-v2",
    );
    // Kept rows carry no mark.
    expect(item.querySelector(".mx-row-mark")?.childElementCount).toBe(0);
  });

  it("keeps the ID whole and lets the source truncate first (D4)", () => {
    const { container } = render(
      <MemoryRow
        agent="codex"
        action="proposed"
        id="M-10432"
        space="memax-v2"
        source="session 8f2c-long"
      >
        x
      </MemoryRow>,
    );
    const second = container.querySelector(".mx-row-rail-2")!;
    // The ID is its own box; space and source share one box that ellipsizes from the end.
    expect(
      [...second.children].map((c) => [c.className, c.textContent]),
    ).toEqual([
      ["mx-row-rail-id", "M-10432"],
      ["mx-row-rail-rest", " · memax-v2 · session 8f2c-long"],
    ]);
    expect(container.querySelector(".mx-row-rail")?.getAttribute("title")).toBe(
      "Codex · proposed · M-10432 · memax-v2 · session 8f2c-long",
    );
  });

  it("marks a state that isn't kept and describes the statement with it", () => {
    const { container } = render(
      <MemoryRow
        state="stale"
        agent="dream"
        action="flagged"
        time="03:12"
        id="M-0187"
        note="Its source changed"
      >
        Ask memax answers with the Haiku tier.
      </MemoryRow>,
    );
    const mark = screen.getByRole("img", { name: "Stale" });
    const text = container.querySelector(".mx-row-text")!;
    expect(text.getAttribute("aria-describedby")).toBe(mark.id);
    expect(container.querySelector(".mx-row")?.className).toContain("is-stale");
    expect(screen.getByText("Its source changed").className).toBe(
      "mx-row-note",
    );
  });

  it("sets proposals unconfirmed, and conflicting proposals when asked", () => {
    const { container, rerender } = render(
      <MemoryRow state="proposed">x</MemoryRow>,
    );
    expect(container.querySelector(".mx-row")?.className).toContain(
      "is-unconfirmed",
    );
    rerender(<MemoryRow state="conflict">x</MemoryRow>);
    expect(container.querySelector(".mx-row")?.className).not.toContain(
      "is-unconfirmed",
    );
    rerender(
      <MemoryRow state="conflict" unconfirmed>
        x
      </MemoryRow>,
    );
    expect(container.querySelector(".mx-row")?.className).toContain(
      "is-unconfirmed",
    );
  });

  it("uses the state's word when no action is given", () => {
    const { container } = render(<MemoryRow state="merged">x</MemoryRow>);
    expect(container.querySelector(".mx-receipt-action")?.textContent).toBe(
      "merged",
    );
  });

  it("makes the statement one link with the receipt as its description, and keeps actions reachable", () => {
    const onEdit = vi.fn();
    render(
      <MemoryList>
        <MemoryRow
          href="/memax-v2/memories/M-0432"
          state="proposed"
          agent="codex"
          action="proposed"
          time="22 min ago"
          id="M-0432"
          actions={
            <Button
              size="sm"
              variant="quiet"
              icon="pencil"
              aria-label="Edit"
              onClick={onEdit}
            />
          }
        >
          Pin shared dependency versions with pnpm catalog:.
        </MemoryRow>
      </MemoryList>,
    );
    const link = screen.getByRole("link", {
      name: "Pin shared dependency versions with pnpm catalog:.",
    });
    expect(link.getAttribute("href")).toBe("/memax-v2/memories/M-0432");
    const description = link
      .getAttribute("aria-describedby")!
      .split(" ")
      .map((id) => document.getElementById(id)!);
    expect(description[0]?.getAttribute("aria-label")).toBe("Proposed");
    expect(description[1]?.textContent).toContain("M-0432");
    // The actions are real buttons in the tab order, in their own labelled group.
    const group = screen.getByRole("group", { name: "Actions for M-0432" });
    const edit = within(group).getByRole("button", { name: "Edit" });
    expect(edit.tabIndex).toBe(0);
    edit.focus();
    expect(document.activeElement).toBe(edit);
    fireEvent.click(edit);
    expect(onEdit).toHaveBeenCalled();
  });

  it("makes the statement a button with onClick and marks the selected row current", () => {
    const onClick = vi.fn();
    render(
      <MemoryRow selected stacked onClick={onClick} id="M-0430">
        MCP write tools must ask for confirmation with input_required.
      </MemoryRow>,
    );
    const button = screen.getByRole("button", {
      name: "MCP write tools must ask for confirmation with input_required.",
    });
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalled();
    const row = button.closest(".mx-row")!;
    expect(row.getAttribute("aria-current")).toBe("true");
    expect(row.className).toContain("is-selected");
    expect(row.className).toContain("is-stacked");
    expect(row.className).toContain("is-clickable");
  });

  it("can stand alone as a div and carries the statement's language", () => {
    const { container } = render(
      <MemoryRow as="div" lang="en">
        x
      </MemoryRow>,
    );
    expect(container.firstElementChild?.tagName).toBe("DIV");
    expect(container.querySelector(".mx-row-text")?.getAttribute("lang")).toBe(
      "en",
    );
  });
});

describe("MemoryText", () => {
  it("applies the state type and says the state", () => {
    const { container } = render(
      <MemoryText state="proposed">River, not Temporal.</MemoryText>,
    );
    const p = container.querySelector("p")!;
    expect(p.className).toBe("mx-mem is-proposed is-unconfirmed");
    expect(p.textContent).toBe("River, not Temporal. (Proposed)");
  });

  it("says nothing extra when kept", () => {
    const { container } = render(
      <MemoryText as="span" size="lg">
        River.
      </MemoryText>,
    );
    expect(container.querySelector("span")?.className).toBe(
      "mx-mem is-lg is-kept",
    );
    expect(container.textContent).toBe("River.");
  });
});

describe("Redaction", () => {
  it("never shows words: a bar, the caption and the forgotten mark", () => {
    render(
      <MemoryList>
        <Redaction
          date="Oct 3"
          id="M-0388"
          detail="removed from 4 files and 5 agents"
        />
        <Redaction date="Sep 29" by="Jiahao" id="M-0301" width="44%" />
      </MemoryList>,
    );
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(
      within(items[0]!).getByRole("img", { name: "Forgotten memory" }),
    ).toBeTruthy();
    expect(items[0]!.querySelector(".mx-redaction-cap")?.textContent).toBe(
      "Forgotten Oct 3 at your request · M-0388 · removed from 4 files and 5 agents",
    );
    expect(items[1]!.querySelector(".mx-redaction-cap")?.textContent).toBe(
      "Forgotten Sep 29 by Jiahao · M-0301",
    );
    expect(
      (items[1]!.querySelector(".mx-redaction-bar") as HTMLElement).style.width,
    ).toBe("44%");
  });

  it("speaks Chinese", () => {
    render(
      <LedgerProvider locale="zh">
        <Redaction as="div" date="10月3日" id="M-0388" />
      </LedgerProvider>,
    );
    expect(screen.getByText("10月3日 应你的要求忘记 · M-0388")).toBeTruthy();
    expect(screen.getByRole("img", { name: "已忘记的记忆" })).toBeTruthy();
  });
});

describe("Diff", () => {
  it("strikes removed words and underlines added ones, and says which is which", () => {
    const { container } = render(
      <Diff before="Deploy to Railway." after="Deploy to Fly.io." />,
    );
    expect(container.querySelector("del.mx-diff-del")?.textContent).toBe(
      "Removed: Railway.",
    );
    expect(container.querySelector("ins.mx-diff-add")?.textContent).toBe(
      "Added: Fly.io.",
    );
    expect(container.querySelector(".mx-diff")?.firstChild?.textContent).toBe(
      "Deploy to ",
    );
  });
});

describe("Cite and Highlight", () => {
  it("cites with a link named by its source, glued to the word before", () => {
    const { container } = render(
      <p>
        dropped.
        <Cite n={2} title="M-0144" href="#M-0144" />
      </p>,
    );
    const link = screen.getByRole("link", { name: "Source 2: M-0144" });
    expect(link.textContent).toBe("2");
    expect(link.className).toBe("mx-cite");
    expect(
      container.querySelector(".mx-cite-wrap")?.firstChild?.textContent,
    ).toBe("⁠");
  });

  it("cites without a link when there is nowhere to go", () => {
    const { container } = render(<Cite n={1} />);
    expect(screen.queryByRole("link")).toBeNull();
    expect(container.querySelector(".mx-cite")?.textContent).toBe("Source 11");
  });

  it("highlights with a mark", () => {
    render(
      <Highlight>River runs on the Postgres we already operate</Highlight>,
    );
    expect(
      screen.getByText("River runs on the Postgres we already operate").tagName,
    ).toBe("MARK");
  });
});

describe("Receipt", () => {
  it("reads stamp · action · time · source · ID with hidden dots", () => {
    const { container } = render(
      <Receipt
        agent="codex"
        action="proposed"
        time="14:02"
        source="session 8f2c"
        id="M-0431"
      />,
    );
    const receipt = container.querySelector(".mx-receipt")!;
    expect(
      receipt.querySelector(".mx-stamp")?.getAttribute("aria-hidden"),
    ).toBe("true");
    expect(receipt.querySelector(".mx-sr")?.textContent).toBe("Codex");
    expect(
      [...receipt.querySelectorAll(".mx-receipt-dot")].every((d) =>
        d.getAttribute("aria-hidden"),
      ),
    ).toBe(true);
    expect(receipt.querySelector(".mx-receipt-id")?.textContent).toBe("M-0431");
  });
});

describe("Lineage", () => {
  it("lists events oldest first, with marks and times", () => {
    render(
      <Lineage
        events={[
          {
            state: "proposed",
            agent: "claude-code",
            title: "Proposed by Claude Code",
            time: "Oct 2, 10:41",
          },
          {
            state: "kept",
            person: "ZZ",
            title: "Kept by you",
            time: "Oct 2, 10:58",
            dateTime: "2026-10-02T10:58",
          },
          {
            title: "Checked against the code",
            time: "Oct 5, 14:31",
            detail: "PR #212 is merged.",
          },
        ]}
      />,
    );
    const items = screen.getAllByRole("listitem");
    expect(
      items.map((i) => i.querySelector(".mx-lineage-title")?.textContent),
    ).toEqual([
      "Proposed by Claude Code",
      "Kept by you",
      "Checked against the code",
    ]);
    expect(
      within(items[0]!).getByRole("img", { name: "Proposed" }),
    ).toBeTruthy();
    expect(items[1]!.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-10-02T10:58",
    );
    expect(items[2]!.querySelector(".mx-lineage-dot")).not.toBeNull();
  });
});

describe("DreamCard", () => {
  const props = {
    issue: 214,
    date: "Mon 5 Oct",
    time: "03:12",
    duration: "41s",
    notes: 34,
    facts: 6,
    noteIds: Array.from({ length: 40 }, (_, i) => `N-${1180 + i}`),
    factIds: ["M-0219", "M-0434"],
  };

  it("prints the edition: masthead, headline and the fold", () => {
    const { container } = render(<DreamCard {...props} />);
    expect(container.querySelector(".mx-dream-name")?.textContent).toBe(
      "Dream",
    );
    expect(container.querySelector(".mx-receipt")?.textContent).toBe(
      "No. 214 · Mon 5 Oct · 03:12 · 41s",
    );
    const title = screen.getByRole("heading", { level: 3 });
    expect(title.textContent).toBe("34 notes became 6 facts.");
    expect(title.querySelector(".mx-dream-n")?.textContent).toBe("6 facts");
    expect(
      screen.getByRole("figure", { name: "34 notes folded into 6 facts" }),
    ).toBeTruthy();
    expect(container.querySelectorAll(".mx-dream-notes span")).toHaveLength(35);
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(
      ["M-0219", "M-0434"],
    );
  });

  it("shows at most three items, conflicts first", () => {
    const { container } = render(
      <DreamCard
        {...props}
        items={[
          { kind: "merged", text: "a" },
          { kind: "faded", text: "b" },
          { kind: "conflict", text: "c", meta: "needs you" },
          { kind: "kept", text: "d" },
        ]}
      />,
    );
    const kinds = [...container.querySelectorAll(".mx-dream-item")].map(
      (li) => li.className,
    );
    expect(kinds).toEqual([
      "mx-dream-item is-conflict",
      "mx-dream-item is-merged",
      "mx-dream-item is-faded",
    ]);
  });

  it("plays once: animate={false} draws it at rest", () => {
    const { container } = render(
      <DreamCard {...props} animate={false} headingLevel={2} />,
    );
    expect(container.querySelector(".mx-dream")?.className).toContain(
      "is-static",
    );
    expect(screen.getByRole("heading", { level: 2 })).toBeTruthy();
  });

  it("uses singular forms and Chinese", () => {
    render(
      <LedgerProvider locale="zh">
        <DreamCard {...props} notes={1} facts={1} />
      </LedgerProvider>,
    );
    expect(screen.getByRole("heading").textContent).toBe(
      "1 条笔记变成了 1 条事实。",
    );
    expect(
      screen.getByText("第 214 期 · Mon 5 Oct · 03:12 · 41s"),
    ).toBeTruthy();
  });
});
