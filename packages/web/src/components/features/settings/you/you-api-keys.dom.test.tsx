// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryClient as QueryClientType,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { en } from "@/i18n/locales/en";

// ── Mocks ─────────────────────────────────────────────────────────────

const updateKeyMock = vi.fn();
const revokeKeyMock = vi.fn();

let apiKeysData: unknown[] = [];
let agentData: unknown[] = [];

vi.mock("@/i18n", () => ({
  useLocale: () => ({ t: en }),
  useInterpolate: () => (template: string, vars: Record<string, string>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => vars[key] ?? ""),
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    hubs: [{ hub: { id: "hub-1", name: "memax-team", hub_type: "team" } }],
  }),
}));

vi.mock("@/hooks/use-api-keys", () => ({
  useApiKeys: () => ({
    data: apiKeysData,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
  useRevokeApiKey: () => ({
    revokeKey: revokeKeyMock,
    feedback: null,
    clearFeedback: vi.fn(),
  }),
  useUpdateApiKey: () => ({
    updateKey: updateKeyMock,
    isPending: false,
    pendingKeyId: null,
  }),
}));

vi.mock("@/hooks/use-connected-agents", () => ({
  useConnectedAgents: () => ({
    data: agentData,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    auth: { createKey: vi.fn() },
  }),
}));

// eslint-disable-next-line import/first
import { YouApiKeys } from "./you-api-keys";

function makeQueryClient(): QueryClientType {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

function renderWithQuery(ui: ReactNode) {
  const client = makeQueryClient();
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}

// A minimal API-key fixture shaped like ApiKeyListItem. We only care
// about the fields the classifier + AssignAgentControl read: id, name,
// prefix, agent_name, standalone.
function makeKey(overrides: {
  id: string;
  name: string;
  agent_name?: string;
  standalone?: boolean;
}) {
  return {
    id: overrides.id,
    name: overrides.name,
    prefix: `mxk_${overrides.id.slice(0, 4)}`,
    scopes: ["read", "write"],
    scope: "all hubs",
    hub_id: null,
    hub_ids: [],
    hub_scope_mode: "all_accessible",
    default_permissions: ["memory:read", "memory:write"],
    trust_level: "elevated",
    agent_name: overrides.agent_name ?? "",
    standalone: overrides.standalone ?? false,
    expires_at: null,
    last_used: null,
    created_at: "2026-04-18T00:00:00Z",
  };
}

describe("YouApiKeys AssignAgentControl", () => {
  beforeEach(() => {
    updateKeyMock.mockReset();
    revokeKeyMock.mockReset();
    apiKeysData = [];
    agentData = [];
  });

  afterEach(() => {
    cleanup();
  });

  it('shows "Create agent: {slug}" when key name normalizes to a slug missing from connected_agents', async () => {
    apiKeysData = [makeKey({ id: "k1", name: "hatch-agent-v4" })];
    agentData = []; // no existing agents

    renderWithQuery(<YouApiKeys />);

    // Open the Assign ▾ dropdown
    fireEvent.click(
      screen.getByRole("button", { name: /Unassigned.*Assign to agent/i }),
    );

    await waitFor(() =>
      expect(screen.getByText("Create agent: hatch-agent-v4")).toBeTruthy(),
    );
  });

  it('hides "Create agent" option when the normalized slug already matches an existing agent', async () => {
    apiKeysData = [makeKey({ id: "k1", name: "claude-code" })];
    agentData = [{ agent_name: "claude-code", display_name: "Claude Code" }];

    renderWithQuery(<YouApiKeys />);

    fireEvent.click(
      screen.getByRole("button", { name: /Unassigned.*Assign to agent/i }),
    );

    await waitFor(() => {
      // Existing agent row should appear
      expect(screen.getByText("Claude Code")).toBeTruthy();
    });
    // The "Create agent:" suggestion should not be present because
    // the normalized slug collides with the existing agent.
    expect(screen.queryByText(/Create agent:/)).toBeNull();
  });

  it("calls updateKey with the suggested slug when Create agent is clicked", async () => {
    apiKeysData = [makeKey({ id: "k2", name: "new-agent" })];
    agentData = [];

    renderWithQuery(<YouApiKeys />);

    fireEvent.click(
      screen.getByRole("button", { name: /Unassigned.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("create-agent-k2")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("create-agent-k2"));

    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k2", {
        agent_name: "new-agent",
      }),
    );
  });

  it("does not render Create agent option for an empty/whitespace-only name", async () => {
    apiKeysData = [makeKey({ id: "k3", name: "   " })];
    agentData = [];

    renderWithQuery(<YouApiKeys />);

    fireEvent.click(
      screen.getByRole("button", { name: /Unassigned.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("mark-standalone-k3")).toBeTruthy(),
    );
    expect(screen.queryByText(/Create agent:/)).toBeNull();
  });

  // ── Reassignment (unified pill always interactive) ─────────────────

  it("linked key opens dropdown with current agent marked, lists other agents", async () => {
    apiKeysData = [
      makeKey({ id: "k4", name: "hatch-key", agent_name: "claude-code" }),
    ];
    agentData = [
      { agent_name: "claude-code", display_name: "Claude Code" },
      { agent_name: "cursor", display_name: "Cursor" },
    ];

    renderWithQuery(<YouApiKeys />);

    fireEvent.click(
      screen.getByRole("button", { name: /Claude Code.*Assign to agent/i }),
    );

    await waitFor(() => expect(screen.getByText("Cursor")).toBeTruthy());
    // Current agent row should carry aria-current to distinguish it.
    const currentRow = screen
      .getAllByRole("menuitem")
      .find((el) => el.getAttribute("aria-current") === "true");
    expect(currentRow?.textContent).toContain("Claude Code");
  });

  it("linked → different agent: PATCHes the new agent_name", async () => {
    apiKeysData = [
      makeKey({ id: "k5", name: "hatch-key", agent_name: "claude-code" }),
    ];
    agentData = [
      { agent_name: "claude-code", display_name: "Claude Code" },
      { agent_name: "cursor", display_name: "Cursor" },
    ];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Claude Code.*Assign to agent/i }),
    );
    await waitFor(() => expect(screen.getByText("Cursor")).toBeTruthy());
    fireEvent.click(screen.getByText("Cursor"));

    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k5", {
        agent_name: "cursor",
      }),
    );
  });

  it("linked → mark as standalone: PATCHes agent_name:'' + standalone:true (belt + suspenders)", async () => {
    apiKeysData = [
      makeKey({ id: "k6", name: "hatch-key", agent_name: "claude-code" }),
    ];
    agentData = [{ agent_name: "claude-code", display_name: "Claude Code" }];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Claude Code.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("mark-standalone-k6")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("mark-standalone-k6"));

    // Client sends BOTH fields so the optimistic update can clear
    // agent_name immediately (server also auto-clears as a fallback).
    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k6", {
        agent_name: "",
        standalone: true,
      }),
    );
  });

  it("linked → clear assignment: PATCHes agent_name:'' + standalone:false", async () => {
    apiKeysData = [
      makeKey({ id: "k7", name: "hatch-key", agent_name: "claude-code" }),
    ];
    agentData = [{ agent_name: "claude-code", display_name: "Claude Code" }];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Claude Code.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("clear-assignment-k7")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("clear-assignment-k7"));

    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k7", {
        agent_name: "",
        standalone: false,
      }),
    );
  });

  it("standalone → agent: PATCHes agent_name (server auto-clears standalone)", async () => {
    apiKeysData = [makeKey({ id: "k8", name: "ci-key", standalone: true })];
    agentData = [{ agent_name: "cursor", display_name: "Cursor" }];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Standalone.*Assign to agent/i }),
    );
    await waitFor(() => expect(screen.getByText("Cursor")).toBeTruthy());
    fireEvent.click(screen.getByText("Cursor"));

    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k8", {
        agent_name: "cursor",
      }),
    );
  });

  it("standalone → unassigned via clear assignment", async () => {
    apiKeysData = [makeKey({ id: "k9", name: "ci-key", standalone: true })];
    agentData = [];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Standalone.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("clear-assignment-k9")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("clear-assignment-k9"));

    await waitFor(() =>
      expect(updateKeyMock).toHaveBeenCalledWith("k9", {
        agent_name: "",
        standalone: false,
      }),
    );
  });

  it("standalone key hides Mark as standalone (already is)", async () => {
    apiKeysData = [makeKey({ id: "k10", name: "ci-key", standalone: true })];
    agentData = [{ agent_name: "cursor", display_name: "Cursor" }];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Standalone.*Assign to agent/i }),
    );
    await waitFor(() => expect(screen.getByText("Cursor")).toBeTruthy());
    expect(screen.queryByTestId("mark-standalone-k10")).toBeNull();
  });

  it("linked key with suggested slug matching current agent hides Create agent", async () => {
    apiKeysData = [
      makeKey({ id: "k11", name: "cursor", agent_name: "cursor" }),
    ];
    agentData = [{ agent_name: "cursor", display_name: "Cursor" }];

    renderWithQuery(<YouApiKeys />);
    fireEvent.click(
      screen.getByRole("button", { name: /Cursor.*Assign to agent/i }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("clear-assignment-k11")).toBeTruthy(),
    );
    expect(screen.queryByText(/Create agent:/)).toBeNull();
  });
});
