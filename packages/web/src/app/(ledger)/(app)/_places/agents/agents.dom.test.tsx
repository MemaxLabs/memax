// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LedgerProvider } from "@memaxlabs/ledger";
import {
  AgentCommandError,
  type AgentConnectionView,
  type AgentDetailView,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { SpaceViewContext } from "../../_lib/space-context";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { AgentRowControl } from "./agent-row-control";
import { AgentDetailPlace } from "./agent-detail";

// The autonomy control and Disconnect, over a fake data source: an
// optimistic lower, a refused raise that rolls back, Write greyed for an
// API key's agent, and Disconnect confirmed inline.

const push = vi.fn();
let agentParam = "c2";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/memax-v2/agents",
  useParams: () => ({ space: "memax-v2", agent: agentParam }),
  useSearchParams: () => new URLSearchParams(),
}));

const source = {
  kind: "sdk" as const,
  now: () => new Date("2026-10-05T14:40:00-07:00"),
  viewer: { initials: "ZZ", name: "Ziyang", timeZone: "America/Vancouver" },
  setAutonomy: vi.fn(),
  pauseAgent: vi.fn(),
  resumeAgent: vi.fn(),
  disconnectAgent: vi.fn(),
  agent: vi.fn(),
};
vi.mock("../../_lib/data", () => ({
  useSource: () => source,
  useViewer: () => source.viewer,
  useSpaces: () => ({ data: [SPACE] }),
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

function connection(over: Partial<AgentConnectionView>): AgentConnectionView {
  return {
    id: "c1",
    agent: "claude-code",
    name: "Claude Code",
    surface: "cli",
    state: "active",
    credential: { kind: "oauth_grant", active: true },
    maxAutonomy: "write",
    mine: true,
    clientId: null,
    spaces: [
      {
        spaceId: "s1",
        slug: "memax-v2",
        name: "memax-v2",
        kind: "project",
        autonomy: "write",
        reads7d: null,
        writes7d: 3,
      },
    ],
    reads7d: null,
    writes7d: 3,
    lastSeenAt: "2026-10-05T14:38:00-07:00",
    connectedAt: "2026-09-02T09:30:00-07:00",
    connectedBy: "you",
    ...over,
  };
}

function at(
  level: "read" | "propose" | "write",
  over: Partial<AgentConnectionView> = {},
) {
  const base = connection(over);
  return {
    ...base,
    spaces: base.spaces.map((s) => ({ ...s, autonomy: level })),
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

/** A promise the test settles. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const radio = (name: string) => screen.getByRole("radio", { name });

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("the autonomy control", () => {
  it("lowers at once, and says so before the server answers", async () => {
    const agent = at("write");
    const answer = deferred<AgentConnectionView>();
    source.setAutonomy.mockReturnValue(answer.promise);
    render(
      <Frame>
        <AgentRowControl agent={agent} space={SPACE} />
      </Frame>,
    );
    fireEvent.click(radio("Propose"));
    expect(radio("Propose").getAttribute("aria-checked")).toBe("true");
    await waitFor(() => expect(source.setAutonomy).toHaveBeenCalledTimes(1));
    expect(source.setAutonomy.mock.calls[0]![0]).toMatchObject({
      agent: "c1",
      autonomy: "propose",
      space: SPACE,
    });
    expect(source.setAutonomy.mock.calls[0]![0].idempotencyKey).toBeTruthy();
    // Optimistic: the toast is there while the command is still on its way.
    expect(
      await screen.findByText(
        "Claude Code proposes in memax-v2 now. Its writes wait in Review.",
      ),
    ).toBeTruthy();
    await act(async () => answer.resolve(at("propose")));
    expect(radio("Propose").getAttribute("aria-checked")).toBe("true");
  });

  it("rolls a refused raise back and says how to get past it", async () => {
    const agent = at("read", { id: "c3", agent: "cursor", name: "Cursor" });
    const answer = deferred<AgentConnectionView>();
    source.setAutonomy.mockReturnValue(answer.promise);
    render(
      <Frame>
        <AgentRowControl agent={agent} space={SPACE} />
      </Frame>,
    );
    fireEvent.click(radio("Propose"));
    expect(radio("Propose").getAttribute("aria-checked")).toBe("true");
    await waitFor(() => expect(source.setAutonomy).toHaveBeenCalledTimes(1));
    // A raise waits: no toast until the server answers.
    expect(screen.queryByText(/Cursor proposes in memax-v2 now/)).toBeNull();
    await act(async () => answer.reject(new AgentCommandError("needs_web")));
    await waitFor(() =>
      expect(radio("Read").getAttribute("aria-checked")).toBe("true"),
    );
    expect(radio("Propose").getAttribute("aria-checked")).toBe("false");
    const toast = await screen.findByText(
      /^Memax couldn't confirm this came from you on memax.app, so Cursor stays at Read\./,
    );
    expect(toast.textContent).toContain("WEB_SURFACE_SECRET");
    fireEvent.click(screen.getByRole("button", { name: "Sign in again" }));
    expect(push).toHaveBeenCalledWith(
      "/signin?next=%2Fmemax-v2%2Fagents&again=1",
    );
  });

  it("sends only where the arrows stop", async () => {
    const agent = at("read", { id: "c3", agent: "cursor", name: "Cursor" });
    source.setAutonomy.mockResolvedValue(at("write"));
    render(
      <Frame>
        <AgentRowControl agent={agent} space={SPACE} />
      </Frame>,
    );
    const read = radio("Read");
    read.focus();
    fireEvent.keyDown(read, { key: "ArrowRight" });
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(radio("Write").getAttribute("aria-checked")).toBe("true");
    await waitFor(() => expect(source.setAutonomy).toHaveBeenCalledTimes(1));
    expect(source.setAutonomy.mock.calls[0]![0].autonomy).toBe("write");
    expect(
      await screen.findByText(
        "Cursor writes to memax-v2 now, still with a receipt.",
      ),
    ).toBeTruthy();
  });

  it("greys Write for an API key's agent, says why, and never sends it", async () => {
    const agent = at("propose", {
      credential: { kind: "api_key", active: true },
      maxAutonomy: "propose",
      name: "CI bot",
      agent: "codex",
    });
    render(
      <Frame>
        <AgentRowControl agent={agent} space={SPACE} />
      </Frame>,
    );
    const write = radio("Write");
    expect(write.getAttribute("aria-disabled")).toBe("true");
    expect(write.title).toBe(
      "API keys propose at most. Connect CI bot over OAuth to let it write.",
    );
    fireEvent.click(write);
    expect(radio("Propose").getAttribute("aria-checked")).toBe("true");
    await new Promise((r) => setTimeout(r, 450));
    expect(source.setAutonomy).not.toHaveBeenCalled();
  });

  it("lets an owner lower someone else's agent, never raise it", () => {
    render(
      <Frame>
        <AgentRowControl agent={at("propose", { mine: false })} space={SPACE} />
      </Frame>,
    );
    expect(radio("Write").getAttribute("aria-disabled")).toBe("true");
    expect(radio("Write").title).toMatch(
      /^Only the person Claude Code works for/,
    );
    expect(radio("Read").getAttribute("aria-disabled")).toBeNull();
  });
});

function detail(over: Partial<AgentConnectionView> = {}): AgentDetailView {
  return {
    connection: at("propose", {
      id: "c2",
      agent: "codex",
      name: "Codex",
      surface: "cloud",
      ...over,
    }),
    week: {
      reads: null,
      writes: 2,
      proposals: 2,
      kept: 1,
      rejected: 0,
      waiting: 1,
      questions: null,
      handoffsReceived: null,
      heldExternal: null,
    },
    recentWrites: [],
    sessions: [],
  };
}

describe("Disconnect", () => {
  it("is confirmed inline, naming what it revokes, and Escape backs out", async () => {
    agentParam = "c2";
    source.agent.mockResolvedValue(detail());
    source.disconnectAgent.mockResolvedValue({
      ...detail().connection,
      state: "disconnected",
    });
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    const disconnect = await screen.findByRole("button", {
      name: "Disconnect",
    });
    fireEvent.click(disconnect);
    const confirm = screen.getByRole("region", { name: "Disconnect Codex?" });
    expect(
      within(confirm).getByText(
        "Its OAuth grant is revoked now, so its next request is refused. Its receipts stay, and you can connect it again later.",
      ),
    ).toBeTruthy();
    const cancel = within(confirm).getByRole("button", { name: /Cancel/ });
    await waitFor(() => expect(document.activeElement).toBe(cancel));
    fireEvent.keyDown(cancel, { key: "Escape" });
    expect(
      screen.queryByRole("region", { name: "Disconnect Codex?" }),
    ).toBeNull();
    expect(document.activeElement).toBe(disconnect);
    expect(source.disconnectAgent).not.toHaveBeenCalled();

    fireEvent.click(disconnect);
    fireEvent.click(
      within(
        screen.getByRole("region", { name: "Disconnect Codex?" }),
      ).getByRole("button", { name: "Disconnect Codex" }),
    );
    await waitFor(() =>
      expect(source.disconnectAgent).toHaveBeenCalledTimes(1),
    );
    expect(source.disconnectAgent.mock.calls[0]![0]).toMatchObject({
      agent: "c2",
    });
    expect(
      await screen.findByText("Disconnected Codex. Its credential is revoked."),
    ).toBeTruthy();
  });

  it("names an API key's revocation, and is only for the agent's own person", async () => {
    agentParam = "c2";
    source.agent.mockResolvedValue(
      detail({ credential: { kind: "api_key", active: true }, mine: false }),
    );
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    const disconnect = await screen.findByRole("button", {
      name: "Disconnect",
    });
    expect(disconnect.getAttribute("aria-disabled")).toBe("true");
    expect(disconnect.title).toBe(
      "Only the person Codex works for can pause or disconnect it.",
    );
    fireEvent.click(disconnect);
    expect(
      screen.queryByRole("region", { name: "Disconnect Codex?" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Pause" }).title).toBe(
      "Only the person Codex works for can pause or disconnect it.",
    );
  });

  it("shows a credential revoked outside Agents", async () => {
    agentParam = "c2";
    source.agent.mockResolvedValue(
      detail({ credential: { kind: "api_key", active: false } }),
    );
    render(
      <Frame>
        <AgentDetailPlace />
      </Frame>,
    );
    expect(await screen.findByText("Its credential was revoked.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Pause" }).title).toBe(
      "Its credential was revoked.",
    );
    // Disconnecting clears it, and stays possible.
    expect(
      screen
        .getByRole("button", { name: "Disconnect" })
        .getAttribute("aria-disabled"),
    ).toBeNull();
  });
});
