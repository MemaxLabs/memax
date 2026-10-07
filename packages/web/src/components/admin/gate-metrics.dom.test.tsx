// @vitest-environment jsdom

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";

import { LocaleProvider } from "@/i18n";
import type { AdminV2Metrics } from "@/lib/admin-client";
import { GateMetricsView, formatRate, formatSeconds } from "./gate-metrics";

// The gate panel shows what the server computed: the gates (judged on
// new people, from V1 beside them), the north star, the cohorts and
// review health. The numbers are the server's gate fixture
// (internal/ledger/ledgertest).

const metrics: AdminV2Metrics = {
  from: "2026-08-03T00:00:00Z",
  to: "2026-10-05T00:00:00Z",
  as_of: "2026-10-03T00:00:00Z",
  first_session_hours: 24,
  cohorts: [
    {
      week: "2026-08-03T00:00:00Z",
      cohort: "new",
      people: 5,
      sessions_closed: 5,
      two_connections: 3,
      two_agent_kinds: 2,
      compiled: 3,
      activated: 2,
      agents_read: 3,
      first_files: 4,
      first_files_under_5m: 3,
      first_file_p50_seconds: 210,
      first_file_p90_seconds: 408,
      signup_to_file_p50_seconds: 570,
      retention_eligible: 2,
      retained: 1,
      team_eligible: 2,
      team_pulled: 1,
    },
  ],
  totals: [
    {
      cohort: "new",
      people: 10,
      sessions_closed: 9,
      two_connections: 5,
      two_agent_kinds: 4,
      compiled: 6,
      activated: 4,
      agents_read: 3,
      first_files: 8,
      first_files_under_5m: 7,
      first_file_p50_seconds: 180,
      first_file_p90_seconds: 312,
      signup_to_file_p50_seconds: 360,
      retention_eligible: 3,
      retained: 2,
      team_eligible: 2,
      team_pulled: 1,
    },
  ],
  review: [
    {
      week: "2026-08-03T00:00:00Z",
      proposals: 6,
      kept: 2,
      rejected: 1,
      folded: 1,
      forgotten: 1,
      open: 1,
      decision_p50_seconds: 120,
      decision_p90_seconds: 312,
    },
  ],
  review_total: {
    proposals: 8,
    kept: 3,
    rejected: 1,
    folded: 1,
    forgotten: 1,
    open: 2,
    decision_p50_seconds: 240,
    decision_p90_seconds: 2628,
  },
  gates: [
    {
      phase: 2,
      name: "activation",
      bar: 0.6,
      value: 4 / 9,
      numerator: 4,
      denominator: 9,
      status: "fail",
      from_v1: 1,
    },
    {
      phase: 2,
      name: "first_file",
      bar: 300,
      value: 180,
      numerator: 7,
      denominator: 8,
      status: "pass",
      from_v1: null,
    },
    {
      phase: 3,
      name: "week4_keeping",
      bar: 0.4,
      value: null,
      numerator: 0,
      denominator: 0,
      status: "pending",
      from_v1: null,
    },
    {
      phase: 4,
      name: "team_pull",
      bar: 0.25,
      value: 0.5,
      numerator: 1,
      denominator: 2,
      status: "pass",
      from_v1: null,
    },
  ],
  north_star: {
    week_ending: "2026-10-02",
    spaces_read: 4,
    spaces_two_agents: 2,
    spaces_two_connections: 3,
    spaces_hook_loads: 1,
    connections_reading: 3,
    connections_seen: 4,
    coverage: 0.75,
  },
};

afterEach(() => {
  cleanup();
});

describe("GateMetricsView", () => {
  it("shows each gate with its people, bar, status and the V1 cohort beside it", () => {
    const { container } = render(
      <LocaleProvider initialLocale="en">
        <GateMetricsView data={metrics} />
      </LocaleProvider>,
    );
    const gate = (name: string) =>
      within(container.querySelector(`[data-gate="${name}"]`) as HTMLElement);
    expect(
      gate("activation").getByText(
        "Phase 2 · 2+ agents and a compile in the first session",
      ),
    ).toBeTruthy();
    expect(gate("activation").getByText("44%")).toBeTruthy();
    expect(gate("activation").getByText("Below the bar")).toBeTruthy();
    expect(gate("activation").getByText("4 of 9 · bar 60%")).toBeTruthy();
    expect(gate("activation").getByText("From V1: 100%")).toBeTruthy();
    expect(gate("first_file").getByText("3m00s")).toBeTruthy();
    expect(
      gate("first_file").getByText("7 of 8 under 5 min · bar 5m00s"),
    ).toBeTruthy();
    expect(
      gate("week4_keeping").getByText("Waiting for windows to close"),
    ).toBeTruthy();
    expect(screen.getByText("2 of 4")).toBeTruthy();
  });

  it("lists the cohorts with a total, and review health by week", () => {
    render(
      <LocaleProvider initialLocale="en">
        <GateMetricsView data={metrics} />
      </LocaleProvider>,
    );
    // The week starts a row of each table.
    expect(screen.getAllByText("2026-08-03")).toHaveLength(2);
    expect(screen.getByText("2 (40%)")).toBeTruthy();
    expect(screen.getByText("4 (44%)")).toBeTruthy();
    expect(screen.getByText("33%")).toBeTruthy();
    expect(screen.getByText("43m48s")).toBeTruthy();
    expect(
      screen.getByText(
        "First session: the 24 hours after signup, long enough to run init and open a second agent.",
      ),
    ).toBeTruthy();
  });

  it("says when there is no one yet", () => {
    render(
      <LocaleProvider initialLocale="zh">
        <GateMetricsView
          data={{ ...metrics, cohorts: [], totals: [], review: [] }}
        />
      </LocaleProvider>,
    );
    expect(screen.getByText("这些周里没有人进入 V2 空间。")).toBeTruthy();
    expect(screen.getByText("这些周里没有提议。")).toBeTruthy();
  });
});

describe("formatting", () => {
  it("writes durations and rates", () => {
    expect(formatSeconds(null, "–")).toBe("–");
    expect(formatSeconds(210, "–")).toBe("3m30s");
    expect(formatSeconds(3600, "–")).toBe("1h00m");
    expect(formatSeconds(263040, "–")).toBe("3d01h");
    expect(formatRate(4 / 9)).toBe("44%");
  });
});
