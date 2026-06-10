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

const createKeyMock = vi.fn();
const revokeKeyMock = vi.fn();
const toastErrorMock = vi.fn();
const disconnectMock = vi.fn();
const forgetConfigsMock = vi.fn();

let agentData: unknown[] = [];
let apiKeysData: unknown[] = [];

vi.mock("@/i18n", () => ({
  useLocale: () => ({ t: en }),
  useInterpolate: () => (template: string, vars: Record<string, string>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => vars[key] ?? ""),
  pluralize: (one: string, other: string, n: number) =>
    n === 1
      ? one
      : other.replace(/\{(\w+)\}/g, (_, key) => (key === "n" ? String(n) : "")),
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    hubs: [{ hub: { id: "hub-1", name: "memax-team", hub_type: "team" } }],
  }),
}));

vi.mock("@/hooks/use-agent-configs", () => ({
  useAgentConfigs: () => ({
    data: { configs: [], extraction_counts: {} },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-agent-config-forget", () => ({
  useAgentConfigForget: () => ({
    forgetConfigs: forgetConfigsMock,
    feedback: null,
    clearFeedback: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-connected-agents", () => ({
  useConnectedAgents: () => ({
    data: agentData,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
  useUpdateAgent: () => ({
    mutate: vi.fn(),
  }),
  useDisconnectAgent: () => ({
    disconnectAgent: disconnectMock,
    feedback: null,
    clearFeedback: vi.fn(),
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
}));

vi.mock("@/hooks/use-bar-toast", () => ({
  useBarToast: () => ({
    error: toastErrorMock,
    success: vi.fn(),
    info: vi.fn(),
    dismiss: vi.fn(),
  }),
}));

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    auth: {
      createKey: (...args: unknown[]) => createKeyMock(...args),
    },
  }),
}));

// eslint-disable-next-line import/first
import {
  AgentConfigsSection,
  CredentialClassPill,
} from "./agent-configs-section";

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

describe("AgentConfigsSection settings polish", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    createKeyMock.mockReset();
    revokeKeyMock.mockReset();
    toastErrorMock.mockReset();
    disconnectMock.mockReset();
    forgetConfigsMock.mockReset();

    agentData = [
      {
        agent_name: "claude-code",
        display_name: "Claude Code",
        memory_count: 3,
        key_count: 1,
        config_count: 0,
        last_active_at: "2026-04-17T11:00:00Z",
        last_observed_at: "2026-04-17T11:57:00Z",
        last_operation: "push",
        last_activity_summary: "saved setup note",
      },
    ];

    apiKeysData = [
      {
        id: "key-agent",
        prefix: "mxk_agent",
        name: "claude-code",
        scope: "global",
        scopes: [],
        hub_id: null,
        created_at: "2026-04-17T10:00:00Z",
        expires_at: null,
        last_used: "2026-04-17T11:57:00Z",
        agent_name: "claude-code",
      },
      {
        id: "key-orphan",
        prefix: "mxk_orphan",
        name: "hatch-agent-v3",
        scope: "global",
        scopes: [],
        hub_id: null,
        created_at: "2026-04-17T10:00:00Z",
        expires_at: null,
        last_used: "2026-04-17T11:57:00Z",
      },
    ];
  });

  // Orphan-section + reissue-dialog tests removed — that UI moved to
  // YouApiKeys under a unified "unassigned" state with inline
  // "Assign ▾" affordance. Coverage lives in you-api-keys.dom.test.tsx
  // and use-api-keys.dom.test.tsx.

  it("renders credential-class pill labels for all three states", () => {
    render(
      <div className="space-y-2">
        <CredentialClassPill kind="api_key" t={en as never} />
        <CredentialClassPill kind="oauth" t={en as never} />
        <CredentialClassPill kind="session" t={en as never} />
      </div>,
    );

    expect(screen.getByText("via API key")).toBeTruthy();
    expect(screen.getByText("via OAuth")).toBeTruthy();
    expect(screen.getByText("via session")).toBeTruthy();
  });

  it("renders an unnamed agent card with reconnect remediation dialog", async () => {
    agentData = [
      {
        agent_name: "unknown",
        display_name: "",
        memory_count: 1,
        key_count: 0,
        config_count: 0,
        needs_reconnect: true,
        last_active_at: "2026-04-17T11:00:00Z",
        last_observed_at: "2026-04-17T11:57:00Z",
      },
    ];
    apiKeysData = [];
    disconnectMock.mockResolvedValueOnce(true);

    renderWithQuery(<AgentConfigsSection />);

    expect(screen.getByText("Unnamed agent")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Unnamed agent/i }));
    fireEvent.click(screen.getByRole("button", { name: "Reconnect" }));

    expect(screen.getByText("Disconnect agent")).toBeTruthy();
    expect(
      screen.getByText(
        "This connection was created without a stable agent identity. Disconnect the agent and have it reconnect through MCP OAuth — its registration will be validated.",
      ),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Disconnect agent" }));
    await waitFor(() => expect(disconnectMock).toHaveBeenCalledWith("unknown"));
  });

  it("does not show reconnect CTA for a normal agent card", () => {
    renderWithQuery(<AgentConfigsSection />);

    fireEvent.click(screen.getByRole("button", { name: /Claude Code/i }));
    expect(screen.queryByRole("button", { name: "Reconnect" })).toBeNull();
  });
});
