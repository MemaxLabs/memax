// @vitest-environment jsdom

/**
 * C3 guard: the mechanism panel's numbers come from the live APIs —
 * a hardcoded figure would pass visual review and then silently lie
 * forever, which is the exact drift this page exists to prevent.
 */

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, cleanup, screen } from "@testing-library/react";
import { en } from "@/i18n/locales/en";

vi.mock("@/i18n", () => ({
  useLocale: () => ({ t: en, locale: "en" }),
  useInterpolate: () => (tpl: string, vars: Record<string, unknown>) =>
    tpl.replace(/\{(\w+)\}/g, (_, k) => String(vars[k] ?? "")),
  interpolate: (tpl: string, vars: Record<string, unknown>) =>
    tpl.replace(/\{(\w+)\}/g, (_, k) => String(vars[k] ?? "")),
}));

const hubSummaryMock = vi.fn();
const agentsMock = vi.fn();
const dreamsMock = vi.fn();

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ hubs: [{ hub: { id: "h1" } }, { hub: { id: "h2" } }] }),
  useActiveHub: () => ({ activeHub: { hub: { id: "h1" } } }),
}));
vi.mock("@/hooks/use-hub-management", () => ({
  useHubSummary: () => hubSummaryMock(),
}));
vi.mock("@/hooks/use-connected-agents", () => ({
  useConnectedAgents: () => agentsMock(),
}));
vi.mock("@/hooks/use-dreams", () => ({
  useDreamReport: () => dreamsMock(),
}));
vi.mock("@/lib/scroll-lock", () => ({
  acquireBodyScrollLock: () => () => {},
}));
vi.mock("./connect-agents-section", () => ({
  ConnectAgentsBody: () => <div data-testid="connect-body" />,
}));

// eslint-disable-next-line import/first
import { OnboardingMechanismModal } from "./onboarding-mechanism-modal";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OnboardingMechanismModal mechanism tab", () => {
  it("renders the API-provided figures verbatim", () => {
    hubSummaryMock.mockReturnValue({
      data: { stats: { memories: 1247, topics: 38 } },
    });
    agentsMock.mockReturnValue({ data: [{}, {}, {}, {}, {}] });
    dreamsMock.mockReturnValue({
      data: { has_run: true, run: { finished_at: new Date().toISOString() } },
    });

    render(
      <OnboardingMechanismModal onClose={() => {}} initialTab="mechanism" />,
    );

    // These exact values only exist in the mocks — if the component
    // hardcoded anything, this fails.
    expect(screen.getByText("1247")).toBeTruthy();
    expect(screen.getByText("38")).toBeTruthy();
    expect(screen.getByText("5")).toBeTruthy(); // agents
    expect(screen.getByText("2")).toBeTruthy(); // hubs from useAuth
  });

  it("shows em-dashes while data is loading — never a fake number", () => {
    hubSummaryMock.mockReturnValue({ data: undefined });
    agentsMock.mockReturnValue({ data: undefined });
    dreamsMock.mockReturnValue({ data: undefined });

    render(
      <OnboardingMechanismModal onClose={() => {}} initialTab="mechanism" />,
    );

    // memories/topics/agents unloaded → three em-dash stats, plus the
    // dreams row's own dash.
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(3);
  });

  it("quick start tab hosts the connect flow", () => {
    hubSummaryMock.mockReturnValue({ data: undefined });
    agentsMock.mockReturnValue({ data: undefined });
    dreamsMock.mockReturnValue({ data: undefined });
    render(
      <OnboardingMechanismModal onClose={() => {}} initialTab="quickstart" />,
    );
    expect(screen.getByTestId("connect-body")).toBeTruthy();
  });
});
