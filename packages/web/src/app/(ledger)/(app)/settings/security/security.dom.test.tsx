// @vitest-environment jsdom
import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { SecurityView } from "@/lib/v2/data/settings";
import { DEMO_SECURITY } from "@/lib/v2/data/settings-demo";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { renderPlace } from "../../_places/test-frame";
import { SecuritySettings } from "./security-settings";

// Settings › Security over the demo source: every section from the
// server's posture, each space's seal and key, the commands, what Forget
// can't reach, Voyage's retention said as unconfirmed, the agents and
// this session's assurance.

const h = vi.hoisted(() => ({ source: null as LedgerDataSource | null }));

vi.mock("@/lib/v2/data/demo-source", async (load) => {
  const actual = await load<typeof import("@/lib/v2/data/demo-source")>();
  return {
    ...actual,
    get demoSource() {
      return h.source ?? actual.demoSource;
    },
  };
});
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/settings/security",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

function withSecurity(security: SecurityView = DEMO_SECURITY) {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  h.source = {
    ...demo,
    settings: {
      ...demo.settings,
      peekSecurity: () => security,
      security: async () => security,
    },
  };
}

const section = (name: string) => screen.getByRole("region", { name });

describe("Settings › Security", () => {
  it("shows each space's seal, verified, and the key it's signed with", () => {
    withSecurity();
    renderPlace(<SecuritySettings />);
    expect(
      screen.getByRole("heading", { level: 1, name: "Security" }),
    ).toBeTruthy();
    const receipts = section("Receipts");
    const memax = within(receipts)
      .getByText("memax-v2")
      .closest("li") as HTMLElement;
    expect(
      within(memax).getByText(/^Sealed through receipt 1,284/),
    ).toBeTruthy();
    expect(
      within(memax).getByText(/^Verified from the first receipt/),
    ).toBeTruthy();
    expect(
      within(memax).getByText("Signed with key ed25519:7f3a9c2e41d0b6a5."),
    ).toBeTruthy();
    const personal = within(receipts)
      .getByText("Personal")
      .closest("li") as HTMLElement;
    expect(within(personal).getByText("Nothing sealed yet.")).toBeTruthy();
  });

  it("gives the commands to export and verify the space", () => {
    withSecurity();
    renderPlace(<SecuritySettings />);
    const exp = section("Export and verify");
    expect(
      within(exp).getByText("memax export --space memax-v2", { exact: false }),
    ).toBeTruthy();
    expect(
      within(exp).getByText("memax verify-export memax-export/memax-v2", {
        exact: false,
      }),
    ).toBeTruthy();
  });

  it("says what Forget reaches and what it can't, backups for the server's days", () => {
    withSecurity({ ...DEMO_SECURITY, backupDays: 14 });
    renderPlace(<SecuritySettings />);
    const forget = section("What Forget reaches");
    expect(
      within(forget).getByText(
        "Every compiled file, rewritten within a minute",
      ),
    ).toBeTruthy();
    expect(
      within(forget).getByText(
        "Database backups, for 14 days. Memax applies every Forget again after any restore.",
      ),
    ).toBeTruthy();
    expect(
      within(forget).getByText(
        "Earlier commits of a compiled file, in your repository's history",
      ),
    ).toBeTruthy();
    expect(
      within(forget).getByText("What agents wrote to their own memory"),
    ).toBeTruthy();
  });

  it("names where data lives and who else sees a memory's words, Voyage's retention unconfirmed", () => {
    withSecurity();
    renderPlace(<SecuritySettings />);
    const residency = section("Where your data lives");
    expect(
      within(residency).getByText("Neon Postgres, us-west-2"),
    ).toBeTruthy();
    expect(within(residency).getByText("Fly.io, sjc")).toBeTruthy();
    expect(within(residency).getByText("Cloudflare R2")).toBeTruthy();
    expect(within(residency).getByText("Cloudflare Workers")).toBeTruthy();

    const processors = section("Who else sees a memory's words");
    const voyage = within(processors).getByRole("row", { name: /^Voyage AI/ });
    expect(
      within(voyage).getByText(/^Not confirmed\. Voyage AI keeps API inputs/),
    ).toBeTruthy();
    const openrouter = within(processors).getByRole("row", {
      name: /^OpenRouter/,
    });
    expect(
      within(openrouter).getByText(
        "The judge's check on decisions in force and Dream's check on decisions in force: Claude Sonnet 5.5, on Google Vertex AI, fp8 or better",
      ),
    ).toBeTruthy();
    expect(
      within(openrouter).getByText(
        "Nothing. Every call goes to zero-retention endpoints only.",
      ),
    ).toBeTruthy();
  });

  it("says when the server sends words nowhere, and when Voyage's opt-out is confirmed", () => {
    withSecurity({
      ...DEMO_SECURITY,
      processors: [{ ...DEMO_SECURITY.processors[1]!, retention: "zero" }],
    });
    renderPlace(<SecuritySettings />);
    expect(screen.queryByText(/^Not confirmed/)).toBeNull();
    cleanup();
    withSecurity({ ...DEMO_SECURITY, processors: [] });
    renderPlace(<SecuritySettings />);
    expect(
      screen.getByText(
        "This server sends no memory's words to outside services.",
      ),
    ).toBeTruthy();
  });

  it("sums up the agents, links to them, and says what this session's Keep counts as", () => {
    withSecurity();
    renderPlace(<SecuritySettings />);
    const agents = section("Agents and autonomy");
    expect(within(agents).getByText(/connected across/)).toBeTruthy();
    expect(
      within(agents).getByRole("link", { name: "Agents and keys" }),
    ).toHaveProperty("href", expect.stringContaining("/settings/keys"));
    const assurance = section("What your Keep counts as");
    expect(within(assurance).getByText("human_web")).toBeTruthy();
    expect(
      within(assurance).getByText(
        "Keeping a decision in a team space, or anything that came from outside, needs human_web.",
      ),
    ).toBeTruthy();
  });
});
