// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LedgerProvider } from "@memaxlabs/ledger";
import { START_PACKAGE, cloudHookFile } from "@/lib/v2/agents/cloud";
import type {
  AgentConnectionView,
  AgentDetailView,
  AgentStartView,
  CloudAgentResultView,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { SpaceViewContext } from "../../_lib/space-context";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { AgentDetailPlace } from "./agent-detail";
import { ConnectCloudDialog } from "./connect-cloud-dialog";

// Connect a cloud agent and a cloud agent's page, over a fake data
// source: the key shown once and never written into the hook file, a new
// key for one already connected, Codex's committed files, the agent's
// last start, and a new key confirmed inline.

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace }),
  usePathname: () => "/memax-v2/agents",
  useParams: () => ({ space: "memax-v2", agent: "k1" }),
  useSearchParams: () => new URLSearchParams(),
}));

const SPACE: SpaceSummary = {
  id: "s1",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  kept: null,
  agents: null,
  people: null,
  waiting: null,
};
const PERSONAL: SpaceSummary = {
  ...SPACE,
  id: "s9",
  slug: "personal",
  name: "Personal",
  kind: "personal",
};
const TEAM: SpaceSummary = {
  ...SPACE,
  id: "s2",
  slug: "acme-team",
  name: "Acme team",
  kind: "team",
};

const source = {
  kind: "sdk" as const,
  now: () => new Date("2026-10-05T14:40:00-07:00"),
  viewer: { initials: "ZZ", name: "Ziyang", timeZone: "America/Vancouver" },
  myAgents: vi.fn(),
  agent: vi.fn(),
  connectCloudAgent: vi.fn(),
  rekeyAgent: vi.fn(),
  pauseAgent: vi.fn(),
  disconnectAgent: vi.fn(),
  setAutonomy: vi.fn(),
};
vi.mock("../../_lib/data", () => ({
  useSource: () => source,
  useViewer: () => source.viewer,
  useSpaces: () => ({ data: [PERSONAL, SPACE, TEAM] }),
}));

const SECRET = `mxk_${"7f2c3b1d".repeat(8)}`;
const NEXT_SECRET = `mxk_${"a41c9e07".repeat(8)}`;

function cloudAgent(
  over: Partial<AgentConnectionView> = {},
): AgentConnectionView {
  return {
    id: "k1",
    agent: "copilot",
    name: "Copilot cloud agent · memax-v2",
    surface: "cloud",
    state: "active",
    credential: { kind: "api_key", active: true },
    maxAutonomy: "read",
    mine: true,
    clientId: null,
    spaces: [
      {
        spaceId: "s1",
        slug: "memax-v2",
        name: "memax-v2",
        kind: "project",
        autonomy: "read",
        reads7d: 4,
        writes7d: 0,
      },
    ],
    reads7d: 4,
    writes7d: 0,
    lastSeenAt: "2026-10-05T14:20:00-07:00",
    connectedAt: "2026-10-05T09:00:00-07:00",
    connectedBy: "you",
    ...over,
  };
}

function result(secret: string, over: Partial<AgentConnectionView> = {}) {
  return {
    connection: cloudAgent(over),
    key: {
      secret,
      prefix: secret.slice(0, 8),
      expiresAt: "2027-04-03T14:40:00-07:00",
    },
  } satisfies CloudAgentResultView;
}

function detail(lastStart: AgentStartView | null): AgentDetailView {
  return {
    connection: cloudAgent(),
    week: {
      reads: 4,
      writes: 0,
      proposals: 0,
      kept: 0,
      rejected: 0,
      waiting: 0,
      questions: null,
      handoffsReceived: null,
      heldExternal: null,
    },
    recentWrites: [],
    sessions: [],
    lastStart,
  };
}

function Frame({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <LedgerProvider locale="en">
        <ToastProvider>
          <SpaceViewContext
            value={{
              space: SPACE,
              overview: undefined,
              overviewFailed: false,
              retryOverview: () => {},
            }}
          >
            {children}
          </SpaceViewContext>
          <ToastViewport />
        </ToastProvider>
      </LedgerProvider>
    </QueryClientProvider>
  );
}

const writeText = vi.fn(async () => {});
beforeEach(() => {
  vi.clearAllMocks();
  source.myAgents.mockResolvedValue([]);
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
});
afterEach(() => cleanup());

function openDialog() {
  return render(
    <Frame>
      <ConnectCloudDialog open onOpenChange={() => {}} />
    </Frame>,
  );
}

describe("Connect a cloud agent", () => {
  it("pins the start package this repository builds", () => {
    const pkg = JSON.parse(
      readFileSync(join(process.cwd(), "..", "start", "package.json"), "utf8"),
    ) as { name: string; version: string };
    expect(START_PACKAGE).toBe(`${pkg.name}@${pkg.version}`);
  });

  it("makes a start key for one project or team space and shows it once, never in the hook file", async () => {
    source.connectCloudAgent.mockResolvedValue(result(SECRET));
    openDialog();
    const dialog = await screen.findByRole("dialog", {
      name: "Connect a cloud agent",
    });
    // Personal is never a place a cloud agent starts.
    const spaces = within(dialog).getAllByRole("radio", {
      name: /memax-v2|Acme team|Personal/,
    });
    expect(spaces.map((r) => r.closest("label")?.textContent)).toEqual([
      "memax-v2",
      "Acme team",
    ]);
    fireEvent.click(within(dialog).getByRole("radio", { name: /^Copilot/ }));
    fireEvent.click(within(dialog).getByRole("radio", { name: "Acme team" }));
    expect(within(dialog).queryByText(SECRET)).toBeNull();
    fireEvent.click(
      within(dialog).getByRole("button", { name: /Create start key/ }),
    );
    await waitFor(() =>
      expect(source.connectCloudAgent).toHaveBeenCalledTimes(1),
    );
    const input = source.connectCloudAgent.mock.calls[0]![0];
    expect(input).toMatchObject({ agent: "copilot", space: TEAM });
    expect(input.idempotencyKey).toBeTruthy();

    expect(await within(dialog).findByText(SECRET)).toBeTruthy();
    expect(
      within(dialog).getByText(
        "Copy it now: Memax shows a start key once and keeps only its hash.",
      ),
    ).toBeTruthy();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Copy the key" }),
    );
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(SECRET));

    // The steps for Copilot, and the file to commit, which names the variable.
    expect(
      within(dialog).getByText(/Agents secret MEMAX_START_KEY/),
    ).toBeTruthy();
    const file = cloudHookFile("copilot")!;
    const pre = dialog.querySelector("pre")!;
    expect(pre.textContent).toBe(file.json);
    expect(pre.textContent).toContain('[ -n \\"$MEMAX_START_KEY\\" ]');
    expect(pre.textContent).not.toContain("mxk_");
    expect(
      within(dialog).getByText(/connect --cloud copilot --space acme-team/),
    ).toBeTruthy();
    // Nowhere but the layer: not the URL.
    for (const call of replace.mock.calls)
      expect(JSON.stringify(call)).not.toContain(SECRET);
    expect(within(dialog).getByRole("button", { name: "Done" })).toBeTruthy();
  });

  it("gives a connected cloud agent a new key instead of a second connection", async () => {
    source.myAgents.mockResolvedValue([
      cloudAgent({
        agent: "claude-code",
        name: "Claude Code cloud agent · memax-v2",
      }),
    ]);
    source.rekeyAgent.mockResolvedValue(
      result(NEXT_SECRET, { agent: "claude-code" }),
    );
    openDialog();
    const dialog = await screen.findByRole("dialog", {
      name: "Connect a cloud agent",
    });
    const rekey = await within(dialog).findByRole("button", {
      name: /Give it a new key/,
    });
    expect(
      within(dialog).getByText(/A new key stops the old one at once/),
    ).toBeTruthy();
    fireEvent.click(rekey);
    await waitFor(() => expect(source.rekeyAgent).toHaveBeenCalledTimes(1));
    expect(source.rekeyAgent.mock.calls[0]![0]).toMatchObject({ agent: "k1" });
    expect(source.connectCloudAgent).not.toHaveBeenCalled();
    expect(await within(dialog).findByText(NEXT_SECRET)).toBeTruthy();
    // Claude Code's steps and its guarded hook.
    expect(within(dialog).getByText(/network secret for/)).toBeTruthy();
    expect(dialog.querySelector("pre")!.textContent).toContain(
      'CLAUDE_CODE_REMOTE\\" = true',
    );
  });

  it("has no key for Codex: it starts from the committed AGENTS.md", async () => {
    openDialog();
    const dialog = await screen.findByRole("dialog", {
      name: "Connect a cloud agent",
    });
    fireEvent.click(within(dialog).getByRole("radio", { name: /^Codex/ }));
    expect(
      within(dialog).getByText(/starts from the committed AGENTS.md alone/),
    ).toBeTruthy();
    expect(
      within(dialog).queryByRole("button", { name: /Create start key/ }),
    ).toBeNull();
    expect(dialog.querySelector("pre")).toBeNull();
  });
});

describe("a cloud agent's page", () => {
  it("shows its last start: when, the compile its files held, and what it was told", async () => {
    source.agent.mockResolvedValue(
      detail({
        at: "2026-10-05T14:20:00-07:00",
        spaceId: "s1",
        sessionRef: "cp-1",
        compiles: ["C-0881"],
        told: ["M-0219", "M-0443"],
      }),
    );
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    const panel = await screen.findByRole("region", { name: "Last start" });
    expect(within(panel).getByText("C-0881")).toBeTruthy();
    expect(within(panel).getByText("M-0219, M-0443")).toBeTruthy();
    expect(within(panel).getByText(/2 memories changed since/)).toBeTruthy();
    expect(within(panel).getByText("cp-1")).toBeTruthy();
    expect(screen.getByText("Start key")).toBeTruthy();
    // Pausing a start key would change nothing: it gets a new key instead.
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
  });

  it("says when it hasn't started yet", async () => {
    source.agent.mockResolvedValue(detail(null));
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    const panel = await screen.findByRole("region", { name: "Last start" });
    expect(within(panel).getByText(/No start yet/)).toBeTruthy();
  });

  it("gives it a new start key, confirmed inline, and shows the key once", async () => {
    source.agent.mockResolvedValue(detail(null));
    source.rekeyAgent.mockResolvedValue(result(NEXT_SECRET));
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    const open = await screen.findByRole("button", { name: "New start key" });
    fireEvent.click(open);
    const confirm = screen.getByRole("region", {
      name: "Give Copilot cloud agent · memax-v2 a new start key?",
    });
    const cancel = within(confirm).getByRole("button", { name: /Cancel/ });
    await waitFor(() => expect(document.activeElement).toBe(cancel));
    fireEvent.keyDown(cancel, { key: "Escape" });
    expect(
      screen.queryByRole("region", { name: /a new start key\?/ }),
    ).toBeNull();
    expect(document.activeElement).toBe(open);
    expect(source.rekeyAgent).not.toHaveBeenCalled();

    fireEvent.click(open);
    fireEvent.click(
      within(
        screen.getByRole("region", { name: /a new start key\?/ }),
      ).getByRole("button", { name: "Give it a new key" }),
    );
    await waitFor(() => expect(source.rekeyAgent).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(NEXT_SECRET)).toBeTruthy();
    expect(
      await screen.findByText(
        "Gave Copilot cloud agent · memax-v2 a new start key. The old one no longer works.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText(NEXT_SECRET)).toBeNull();
  });
});
