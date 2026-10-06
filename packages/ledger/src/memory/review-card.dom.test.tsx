// @vitest-environment jsdom
import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { LedgerProvider } from "../i18n/provider";
import { ReviewCard, type ReviewCardProps } from "./review-card";

const BASE = {
  agent: "codex",
  time: "22 min ago",
  space: "memax-v2",
  source: "session 8f2c",
  id: "M-0432",
  statement: "Pin shared dependency versions with pnpm catalog:.",
  keptBy: "ZZ",
  keptDate: "Oct 5",
} satisfies Partial<ReviewCardProps>;

/** A deferred promise, so a test can settle onKeep when it wants. */
function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const seal = () => screen.queryByRole("img", { name: /^Kept, Oct 5/ });

describe("ReviewCard", () => {
  it("shows a proposal: state, proposer, statement, receipt and the three verbs", () => {
    render(<ReviewCard {...BASE} kept={false} />);
    expect(screen.getByText("Proposed")).toBeTruthy();
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(
      screen.getByText("M-0432 · into memax-v2 · session 8f2c"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Reject" })
        .getAttribute("aria-keyshortcuts"),
    ).toBe("X");
    expect(
      screen
        .getByRole("button", { name: "Edit" })
        .getAttribute("aria-keyshortcuts"),
    ).toBe("E");
    expect(
      screen
        .getByRole("button", { name: "Keep" })
        .getAttribute("aria-keyshortcuts"),
    ).toBe("K");
    expect(seal()).toBeNull();
    const proposed = document.querySelector(
      ".mx-review-statement.is-proposed",
    )!;
    const kept = document.querySelector(".mx-review-statement.is-kept")!;
    expect(proposed.hasAttribute("aria-hidden")).toBe(false);
    expect(kept.getAttribute("aria-hidden")).toBe("true");
    // The statement carries its state for assistive technology.
    expect(proposed.textContent).toContain("Proposed by Codex");
    expect(
      document.getElementById(proposed.getAttribute("aria-describedby")!)
        ?.textContent,
    ).toBe("Proposed");
  });

  it("asks the caller to keep and never keeps itself", () => {
    const onKeep = vi.fn();
    render(<ReviewCard {...BASE} kept={false} onKeep={onKeep} />);
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    expect(onKeep).toHaveBeenCalledTimes(1);
    expect(seal()).toBeNull();
    expect(screen.getByRole("button", { name: "Keep" })).toBeTruthy();
  });

  it("stamps the seal and shows the person's receipt when kept changes to true", () => {
    const { rerender, container } = render(
      <ReviewCard {...BASE} kept={false} onUndo={() => {}} />,
    );
    rerender(<ReviewCard {...BASE} kept onUndo={() => {}} />);
    const stamp = seal();
    expect(stamp?.getAttribute("aria-label")).toBe("Kept, Oct 5, M-0432");
    expect(stamp?.className).toContain("is-stamping");
    expect(container.querySelector(".mx-review")?.className).toContain(
      "is-kept",
    );
    expect(screen.getByText("Kept by you")).toBeTruthy();
    const receipt = container.querySelector(".mx-review-foot .mx-receipt")!;
    expect(receipt.textContent).toBe("ZZYoukept·just now·M-0432");
    expect(
      screen
        .getByRole("button", { name: "Undo" })
        .getAttribute("aria-keyshortcuts"),
    ).toBe("Meta+Z");
    expect(screen.queryByRole("button", { name: "Keep" })).toBeNull();
    expect(
      document
        .querySelector(".mx-review-statement.is-proposed")
        ?.getAttribute("aria-hidden"),
    ).toBe("true");
  });

  it("does not stamp when it is first drawn kept", () => {
    render(<ReviewCard {...BASE} kept />);
    expect(seal()?.className).not.toContain("is-stamping");
    // No onUndo, no Undo button.
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
  });

  it("stays pending while onKeep's promise runs, then follows the caller", async () => {
    const pending = deferred();
    function Harness() {
      const [kept, setKept] = useState(false);
      return (
        <ReviewCard
          {...BASE}
          kept={kept}
          onKeep={() => pending.promise.then(() => setKept(true))}
          onUndo={() => setKept(false)}
        />
      );
    }
    const { container } = render(<Harness />);
    const keep = screen.getByRole("button", { name: "Keep" });
    fireEvent.click(keep);
    expect(keep.getAttribute("aria-busy")).toBe("true");
    expect(
      container.querySelector(".mx-review")?.getAttribute("aria-busy"),
    ).toBe("true");
    expect(screen.getByRole("button", { name: "Reject" })).toHaveProperty(
      "disabled",
      true,
    );
    fireEvent.click(keep); // Ignored while pending.
    await act(async () => {
      pending.resolve();
      await pending.promise;
    });
    expect(seal()?.className).toContain("is-stamping");
    expect(
      container.querySelector(".mx-review")?.hasAttribute("aria-busy"),
    ).toBe(false);
  });

  it("stays unkept when onKeep's promise rejects", async () => {
    const failing = deferred();
    const onKeep = vi.fn(() => failing.promise);
    render(<ReviewCard {...BASE} kept={false} onKeep={onKeep} />);
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    expect(
      screen.getByRole("button", { name: "Keep" }).getAttribute("aria-busy"),
    ).toBe("true");
    await act(async () => {
      failing.reject(new Error("offline"));
      await failing.promise.catch(() => {});
    });
    expect(seal()).toBeNull();
    const keep = screen.getByRole("button", { name: "Keep" });
    expect(keep.hasAttribute("aria-busy")).toBe(false);
    fireEvent.click(keep);
    expect(onKeep).toHaveBeenCalledTimes(2);
  });

  it("follows a controlled pending flag", () => {
    const onKeep = vi.fn();
    render(<ReviewCard {...BASE} kept={false} pending onKeep={onKeep} />);
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    expect(onKeep).not.toHaveBeenCalled();
  });

  it("rolls back: the seal goes and the proposal returns", () => {
    const { rerender } = render(<ReviewCard {...BASE} kept={false} />);
    rerender(<ReviewCard {...BASE} kept />);
    expect(seal()).not.toBeNull();
    rerender(<ReviewCard {...BASE} kept={false} />);
    expect(seal()).toBeNull();
    expect(screen.getByRole("button", { name: "Keep" })).toBeTruthy();
  });

  it("moves focus from Keep to Undo and back", () => {
    function Harness() {
      const [kept, setKept] = useState(false);
      return (
        <ReviewCard
          {...BASE}
          kept={kept}
          onKeep={() => setKept(true)}
          onUndo={() => setKept(false)}
        />
      );
    }
    render(<Harness />);
    const keep = screen.getByRole("button", { name: "Keep" });
    keep.focus();
    fireEvent.click(keep);
    const undo = screen.getByRole("button", { name: "Undo" });
    expect(document.activeElement).toBe(undo);
    fireEvent.click(undo);
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Keep" }),
    );
  });

  it("quarantines external content until it is kept", () => {
    const { rerender } = render(
      <ReviewCard
        {...BASE}
        kept={false}
        external="Claude Code read this in the MCP specification."
      />,
    );
    const note = screen.getByRole("note");
    expect(note.textContent).toBe(
      "From external content. Claude Code read this in the MCP specification.",
    );
    rerender(
      <ReviewCard
        {...BASE}
        kept
        external="Claude Code read this in the MCP specification."
      />,
    );
    expect(screen.queryByRole("note")).toBeNull();
  });

  it("shows a conflict with the kept memory and offers to replace it", () => {
    render(
      <ReviewCard
        {...BASE}
        kept={false}
        conflictWith="Deploy the v2 API to Railway."
      />,
    );
    expect(
      screen.getByText("Conflicts with a kept memory").closest(".mx-state")
        ?.className,
    ).toContain("mx-state--conflict");
    expect(screen.getByText("Kept now")).toBeTruthy();
    expect(screen.getByText("Deploy the v2 API to Railway.")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Keep, replace old" }),
    ).toBeTruthy();
  });

  it("diffs an update against the kept memory and names it", () => {
    const { container, rerender } = render(
      <ReviewCard
        {...BASE}
        kept={false}
        before="MCP write tools must ask for confirmation through elicitation."
        beforeId="M-0156"
        statement="MCP write tools must ask for confirmation with input_required."
      />,
    );
    expect(container.querySelector("del")?.textContent).toBe(
      "Removed: through",
    );
    expect(container.querySelector("ins")?.textContent).toBe("Added: with");
    expect(container.querySelector(".mx-review-updates")?.textContent).toBe(
      "Updates M-0156",
    );
    rerender(
      <ReviewCard
        {...BASE}
        kept
        before="MCP write tools must ask for confirmation through elicitation."
        beforeId="M-0156"
        statement="MCP write tools must ask for confirmation with input_required."
      />,
    );
    expect(container.querySelector(".mx-review-updates")?.textContent).toBe(
      "Replaced M-0156",
    );
  });

  it("shows evidence while proposed", () => {
    render(
      <ReviewCard
        {...BASE}
        kept={false}
        evidence="“A server that needs input…”"
        evidenceSource="spec 2026-07-28"
      />,
    );
    expect(screen.getByText("“A server that needs input…”").tagName).toBe(
      "BLOCKQUOTE",
    );
    expect(screen.getByText("spec 2026-07-28").tagName).toBe("FIGCAPTION");
  });

  it("names another keeper, and speaks Chinese", () => {
    render(
      <LedgerProvider locale="zh">
        <ReviewCard
          {...BASE}
          kept
          keptByName="Jiahao"
          keptBy="JY"
          keptDate="10月5日"
          onUndo={() => {}}
        />
      </LedgerProvider>,
    );
    expect(screen.getByText("由 Jiahao 保留")).toBeTruthy();
    expect(screen.getByRole("button", { name: "撤销" })).toBeTruthy();
    expect(
      screen.getByRole("img", { name: "已保留，10月5日，M-0432" }),
    ).toBeTruthy();
    expect(screen.getByText("刚刚")).toBeTruthy();
  });
});
