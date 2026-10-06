// @vitest-environment jsdom
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { createRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { LedgerProvider, type LedgerLinkProps } from "../i18n/provider";
import { CommandBar } from "./command-bar";
import { CommandDialog } from "./command-dialog";
import { NavRail, type NavRailProps } from "./nav-rail";
import { PageHeader } from "./page-header";
import { Shell } from "./shell";
import { Terminal } from "./terminal";

const NAV: NavRailProps = {
  active: "review",
  space: "memax-v2",
  spaceKind: "Project",
  person: "ZZ",
  settingsHref: "/settings",
  status: { state: "kept", label: "5 agents in sync" },
  items: [
    { id: "today", href: "/memax-v2/today" },
    {
      id: "review",
      href: "/memax-v2/review",
      count: 5,
      tone: "pending",
      countLabel: "5 waiting on you",
    },
    { id: "briefs", href: "/memax-v2/brief" },
    { id: "memories", href: "/memax-v2/memories" },
    { id: "handoffs", href: "/memax-v2/handoffs", count: 1 },
    { id: "agents", href: "/memax-v2/agents" },
  ],
};

describe("NavRail", () => {
  it("is the main navigation with the places, the current one marked", () => {
    render(<NavRail {...NAV} />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    for (const name of [
      "Today",
      /^Review.*5 waiting on you$/,
      "Briefs",
      "Memories",
      /^Handoffs.*1$/,
      "Agents",
      "Settings",
    ]) {
      expect(within(nav).getByRole("link", { name })).toBeTruthy();
    }
    expect(within(nav).getAllByRole("link")).toHaveLength(7);
    const review = within(nav).getByRole("link", { name: /^Review/ });
    expect(review.getAttribute("aria-current")).toBe("page");
    expect(review.querySelector(".mx-count")?.className).toBe(
      "mx-count is-pending",
    );
    expect(
      within(nav)
        .getByRole("link", { name: "Today" })
        .hasAttribute("aria-current"),
    ).toBe(false);
  });

  it("announces the status line, and shows the person", () => {
    render(
      <NavRail
        {...NAV}
        status={{ state: "proposed", label: "Cursor file drifted" }}
      />,
    );
    const status = screen.getByRole("status");
    expect(status.textContent).toBe("Cursor file drifted");
    expect(status.querySelector(".mx-state")?.className).toContain(
      "mx-state--proposed",
    );
    expect(screen.getByText("You")).toBeTruthy();
  });

  it("wires the space switcher and Ask, by handler or by render element", () => {
    const onSpaceClick = vi.fn();
    const onAsk = vi.fn();
    render(
      <NavRail
        {...NAV}
        onSpaceClick={onSpaceClick}
        askRender={<button type="button" data-trigger="" onClick={onAsk} />}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /memax-v2/ }));
    expect(onSpaceClick).toHaveBeenCalled();
    const ask = screen.getByRole("button", { name: "Ask or remember" });
    expect(ask.hasAttribute("data-trigger")).toBe(true);
    expect(ask.className).toBe("mx-rail-ask");
    expect(ask.getAttribute("aria-keyshortcuts")).toBe("Meta+K");
    fireEvent.click(ask);
    expect(onAsk).toHaveBeenCalled();
  });

  it("renders links with the app's link component and speaks Chinese", () => {
    function AppLink(props: LedgerLinkProps) {
      return <a data-app="" {...props} />;
    }
    render(
      <LedgerProvider locale="zh" linkComponent={AppLink}>
        <NavRail {...NAV} />
      </LedgerProvider>,
    );
    const nav = screen.getByRole("navigation", { name: "主导航" });
    const links = within(nav).getAllByRole("link");
    expect(links.every((l) => l.hasAttribute("data-app"))).toBe(true);
    expect(
      links.map((l) => l.querySelector(".mx-rail-label")?.textContent),
    ).toEqual(["今天", "审阅", "简报", "记忆", "交接", "Agent", "设置"]);
    expect(screen.getByRole("button", { name: "提问或记住" })).toBeTruthy();
  });
});

describe("Shell", () => {
  it("frames the page with a skip link to the sheet", () => {
    render(
      <Shell nav={NAV} mainId="sheet">
        <div className="mx-page">
          <PageHeader
            title="Memories"
            lede="214 kept in memax-v2."
            eyebrow="memax-v2"
            actions={<button type="button">Hand off</button>}
          />
        </div>
      </Shell>,
    );
    const skip = screen.getByRole("link", { name: "Skip to content" });
    expect(skip.getAttribute("href")).toBe("#sheet");
    const main = screen.getByRole("main");
    expect(main.id).toBe("sheet");
    expect(main.className).toBe("mx-sheet");
    expect(
      screen.getByRole("heading", { level: 1, name: "Memories" }),
    ).toBeTruthy();
    expect(screen.getByText("214 kept in memax-v2.").className).toBe(
      "mx-page-lede",
    );
  });
});

describe("CommandBar", () => {
  it("asks by default, with the localized placeholder and mode control", () => {
    render(<CommandBar />);
    const field = screen.getByRole("textbox", { name: "Ask" });
    expect(field.getAttribute("placeholder")).toBe(
      "Ask your context, or remember something…",
    );
    expect(screen.getByRole("radiogroup", { name: "Mode" })).toBeTruthy();
    expect(
      screen.getByRole("radio", { name: "Ask" }).getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("switches mode with Tab in the field, and submits with Enter", () => {
    const onSubmit = vi.fn();
    const onModeChange = vi.fn();
    const inputRef = createRef<HTMLInputElement>();
    render(
      <CommandBar
        defaultQuery="River"
        onSubmit={onSubmit}
        onModeChange={onModeChange}
        inputRef={inputRef}
      />,
    );
    const field = inputRef.current!;
    fireEvent.keyDown(field, { key: "Tab" });
    expect(onModeChange).toHaveBeenCalledWith("remember");
    expect(screen.getByRole("textbox", { name: "Remember" })).toBe(field);
    fireEvent.keyDown(field, { key: "Tab", shiftKey: true });
    expect(onModeChange).toHaveBeenCalledTimes(1);
    fireEvent.change(field, { target: { value: "River, not Temporal" } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("River, not Temporal", "remember");
  });

  it("does nothing while an IME is composing", () => {
    const onSubmit = vi.fn();
    render(<CommandBar onSubmit={onSubmit} />);
    const field = screen.getByRole("textbox");
    fireEvent.keyDown(field, { key: "Enter", isComposing: true });
    fireEvent.keyDown(field, { key: "Tab", isComposing: true });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(
      screen.getByRole("radio", { name: "Ask" }).getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("is controlled when asked", () => {
    function Harness() {
      const [query, setQuery] = useState("");
      return (
        <>
          <CommandBar query={query} onQueryChange={setQuery} mode="remember" />
          <output>{query}</output>
        </>
      );
    }
    render(<Harness />);
    fireEvent.change(screen.getByRole("textbox", { name: "Remember" }), {
      target: { value: "x" },
    });
    expect(screen.getByRole("status").textContent).toBe("x");
  });

  it("shows the key legend", () => {
    const { container } = render(<CommandBar />);
    expect(container.querySelector(".mx-cmd-foot")?.textContent).toBe(
      "↵ open⌘↵ keep answer as memoryTab ask / rememberEscclose",
    );
  });
});

describe("CommandDialog", () => {
  it("opens as a labelled dialog, focuses the field and closes on Escape", async () => {
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            open
          </button>
          <CommandDialog open={open} onOpenChange={setOpen}>
            <CommandBar />
          </CommandDialog>
        </>
      );
    }
    render(<Harness />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "open" }));
    });
    const dialog = await screen.findByRole("dialog", {
      name: "Ask or remember",
    });
    expect(dialog.className).toContain("mx-cmd-layer");
    expect(document.querySelector(".mx-cmd-scrim")).not.toBeNull();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(document.activeElement).toBe(
      within(dialog).getByRole("textbox", { name: "Ask" }),
    );
    await act(async () => {
      fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Terminal", () => {
  it("keeps lines as printed, hides glyphs, and says what they mean", () => {
    const { container } = render(
      <Terminal
        lines={[
          { kind: "cmd", text: 'memax recall "deploy target"' },
          {
            kind: "kept",
            text: "M-0174  Deploy the v2 API to Railway.   kept · JY",
          },
          { kind: "proposed", text: "M-0431  Deploy to Fly.io." },
          { kind: "blank" },
          { kind: "forgotten", text: "Forgotten." },
        ]}
      />,
    );
    const body = screen.getByRole("region", { name: "Terminal: memax" });
    expect(body.tagName).toBe("PRE");
    expect(body.tabIndex).toBe(0);
    const lines = container.querySelectorAll(".mx-term-line");
    expect(lines).toHaveLength(5);
    expect(lines[1]!.className).toBe("mx-term-line is-kept");
    expect(
      lines[1]!.querySelector(".mx-term-glyph")?.getAttribute("aria-hidden"),
    ).toBe("true");
    expect(lines[1]!.textContent).toBe(
      "● Kept: M-0174  Deploy the v2 API to Railway.   kept · JY",
    );
    expect(lines[3]!.textContent).toBe(" ");
    expect(lines[0]!.querySelector(".mx-sr")).toBeNull();
  });
});
