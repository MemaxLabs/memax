// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider, type Locale } from "@/i18n";
import { DocsScreen } from "./docs";
import { LandingScreen } from "./landing";
import { PricingScreen } from "./pricing";
import { SecurityScreen } from "./security";

// The public site's four pages: what each says, where each control goes,
// and that nothing offers what isn't there (billing, Undo on an example).

afterEach(cleanup);

function show(screenElement: React.ReactElement, locale: Locale = "en") {
  render(
    <LocaleProvider initialLocale={locale}>
      <LedgerProvider locale={locale}>{screenElement}</LedgerProvider>
    </LocaleProvider>,
  );
}

describe("the site frame", () => {
  it("has its landmarks, one h1, and the way in", () => {
    show(<PricingScreen />);
    expect(screen.getByRole("banner")).toBeTruthy();
    expect(screen.getByRole("main")).toBeTruthy();
    expect(screen.getByRole("contentinfo")).toBeTruthy();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    const site = screen.getByRole("navigation", { name: "Site" });
    expect(
      within(site)
        .getByRole("link", { name: "Pricing" })
        .getAttribute("aria-current"),
    ).toBe("page");
    expect(
      within(site).getByRole("link", { name: "Product" }).getAttribute("href"),
    ).toBe("/welcome");
    expect(
      screen.getByRole("link", { name: "Memax home" }).getAttribute("href"),
    ).toBe("/welcome");
    for (const name of ["Sign in", "Get started"]) {
      expect(screen.getByRole("link", { name }).getAttribute("href")).toBe(
        "/signin",
      );
    }
    expect(
      screen
        .getByRole("link", { name: "Skip to content" })
        .getAttribute("href"),
    ).toBe("#content");
    const footer = screen.getByRole("navigation", { name: "Footer" });
    expect(
      within(footer)
        .getByRole("link", { name: "Privacy" })
        .getAttribute("href"),
    ).toBe("/privacy");
  });
});

describe("the Landing", () => {
  it("makes the promise and offers this build's init", () => {
    show(<LandingScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "One context. Every agent.",
    );
    const command = screen.getByRole("group", {
      name: "The command that starts Memax",
    });
    expect(command.textContent).toContain("npx memax-cli init");
    expect(
      screen.getByRole("link", { name: "How it works" }).getAttribute("href"),
    ).toBe("#how-it-works");
    expect(document.getElementById("how-it-works")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "See pricing" }).getAttribute("href"),
    ).toBe("/pricing");
  });

  it("copies the command and says so", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    show(<LandingScreen />);
    fireEvent.click(screen.getByRole("button", { name: "Copy command" }));
    expect(writeText).toHaveBeenCalledWith("npx memax-cli init");
    expect(await screen.findByText("Copied")).toBeTruthy();
  });

  it("shows an example space in which nothing acts", () => {
    show(<LandingScreen />);
    const example = screen.getByRole("figure", { name: "An example space" });
    expect(within(example).queryAllByRole("button")).toEqual([]);
    expect(within(example).getByText(/Kept by you/)).toBeTruthy();
  });

  it("words the targets as the app does: Cursor reads AGENTS.md, ChatGPT is live", () => {
    show(<LandingScreen />);
    expect(screen.getAllByText("In sync")).toHaveLength(3);
    expect(screen.getByText("Live over connector")).toBeTruthy();
  });
});

describe("Pricing", () => {
  it("shows D9's monthly prices, and the yearly ones on Yearly", () => {
    show(<PricingScreen />);
    const plan = (name: string) => screen.getByRole("article", { name });
    expect(within(plan("Free")).getByText("$0")).toBeTruthy();
    expect(within(plan("Pro")).getByText("a month")).toBeTruthy();
    expect(plan("Pro").textContent).toContain("$12");
    expect(plan("Team").textContent).toContain("$20 a seat, a month");
    expect(plan("Enterprise").textContent).toContain("Custom");

    fireEvent.click(screen.getByRole("radio", { name: "Yearly" }));
    expect(plan("Pro").textContent).toContain("$120 a year");
    expect(plan("Pro").textContent).toContain(
      "$10 a month, billed yearly: 2 months free",
    );
    expect(plan("Team").textContent).toContain("$192 a seat, a year");
    expect(plan("Team").textContent).toContain(
      "$16 a seat a month, billed yearly: 20% off",
    );
    expect(plan("Free").textContent).toContain("$0");
  });

  it("sells nothing: Free signs in, Pro and Team are coming, Enterprise is an email", () => {
    show(<PricingScreen />);
    expect(
      screen.getByText(
        "Memax is free during the alpha, and billing isn't open yet. These are the plans it will offer.",
      ),
    ).toBeTruthy();
    const plan = (name: string) => screen.getByRole("article", { name });
    expect(
      within(plan("Free"))
        .getByRole("link", { name: "Start free" })
        .getAttribute("href"),
    ).toBe("/signin");
    for (const name of ["Pro", "Team"]) {
      expect(within(plan(name)).queryAllByRole("button")).toEqual([]);
      expect(within(plan(name)).queryAllByRole("link")).toEqual([]);
      expect(within(plan(name)).getByText("Coming soon")).toBeTruthy();
    }
    expect(
      within(plan("Enterprise"))
        .getByRole("link", { name: "Talk to us" })
        .getAttribute("href"),
    ).toBe("mailto:team@memaxlabs.com");
  });

  it("marks what isn't built as coming", () => {
    show(<PricingScreen />);
    const plan = (name: string) => screen.getByRole("article", { name });
    expect(within(plan("Free")).queryAllByText("Coming")).toHaveLength(0);
    expect(within(plan("Pro")).getAllByText("Coming")).toHaveLength(2);
    expect(within(plan("Team")).getAllByText("Coming")).toHaveLength(5);
    expect(within(plan("Enterprise")).getAllByText("Coming")).toHaveLength(3);
    expect(plan("Pro").textContent).toContain("Ask, 1,000 a month");
  });

  it("speaks Chinese", () => {
    show(<PricingScreen />, "zh");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "一个项目免费。 规模变大，价格公道。",
    );
    expect(screen.getByRole("article", { name: "Pro" }).textContent).toContain(
      "$12 每月",
    );
  });
});

describe("Security", () => {
  it("fills the board's placeholders with what is true", () => {
    show(<SecurityScreen />);
    const text = document.body.textContent ?? "";
    expect(text).toContain("Memax cloud: Neon Postgres, us-west-2");
    expect(text).toContain(
      "Encrypted in transit, and at rest with each provider's defaults.",
    );
    expect(text).toContain(
      "Memax holds no third-party certification, such as SOC 2, yet.",
    );
    expect(text).toContain(
      "7 days. Memax applies every Forget again after any restore.",
    );
    expect(text).not.toMatch(
      /\[(region|security contact|response time|confirm before publishing)\]/,
    );
    expect(
      screen
        .getByRole("link", { name: "security@memaxlabs.com" })
        .getAttribute("href"),
    ).toBe("mailto:security@memaxlabs.com");
  });

  it("lists who else sees a memory's words, with the pinned hosts and the floor", () => {
    show(<SecurityScreen />);
    const table = screen.getAllByRole("table")[1]!;
    const rows = within(table).getAllByRole("row");
    expect(
      rows.map((r) => within(r).queryAllByRole("cell")[0]?.textContent ?? ""),
    ).toEqual(["", "OpenRouter", "Voyage AI", "Resend"]);
    expect(rows[1]!.textContent).toContain(
      "DeepSeek V4.1 Flash, on Together AI, Baseten, CoreWeave and DeepInfra, fp8 or better",
    );
    expect(rows[2]!.textContent).toContain("Not confirmed.");
  });

  it("says what Forget can't reach", () => {
    show(<SecurityScreen />);
    const text = document.body.textContent ?? "";
    expect(text).toContain(
      "Earlier commits of a compiled file, in your repository's history",
    );
    expect(text).toContain(
      "What Voyage AI and Resend kept of what they were sent",
    );
  });
});

describe("Docs", () => {
  it("is the quickstart, with every MCP tool", () => {
    show(<DocsScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Quickstart",
    );
    expect(document.body.textContent).toContain("npx memax-cli init");
    expect(document.body.textContent).not.toContain("memax review");
    expect(document.body.textContent).not.toContain("--via pr");
    expect(document.body.textContent).toContain("https://api.memax.app/mcp");
    const tools = screen.getByRole("table");
    expect(within(tools).getAllByRole("rowheader")).toHaveLength(11);
  });

  it("searches its tree, and points to the full docs when nothing matches", () => {
    show(<DocsScreen />);
    const tree = screen.getByRole("navigation", { name: "Docs" });
    const search = screen.getByRole("searchbox", { name: "Search the docs" });
    fireEvent.change(search, { target: { value: "cursor" } });
    expect(
      within(tree)
        .getAllByRole("link")
        .map((a) => a.textContent),
    ).toEqual(["Cursor"]);
    expect(within(tree).getByRole("status").textContent).toBe("1 page matches");
    fireEvent.change(search, { target: { value: "zzz" } });
    expect(
      within(tree)
        .queryAllByRole("link")
        .map((a) => a.textContent),
    ).toEqual(["docs.memax.app"]);
    fireEvent.change(search, { target: { value: "" } });
    expect(within(tree).getAllByRole("link").length).toBeGreaterThan(15);
  });
});
