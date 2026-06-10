// @vitest-environment jsdom

import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";

import { QuotaBanner } from "./quota-banner";

// QuotaBanner is a pure function of (subscription, memoryCount,
// memoryLimit). These tests cover the three lifecycle states + the
// healthy render-nothing path to keep the state resolver honest when
// the grace / inactive / OK boundaries are touched.

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      admin: {
        hubs: {
          detail: {
            quota: {
              overLimitTitle: "Over capacity",
              overLimitBody:
                "{current}/{limit} memories, freeze in {days} day(s)",
              overLimitBodyToday: "{current}/{limit}, freeze within 24h",
              frozenTitle: "Frozen",
              frozenBody: "Pushes rejected.",
              inactiveTitle: "Subscription inactive",
              inactiveBody: "Subscription {status}.",
              sinceLabel: "Over limit since",
            },
          },
        },
      },
    },
  }),
  useInterpolate: () => (s: string, vars: Record<string, string>) =>
    s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`),
}));

afterEach(() => {
  vi.useRealTimers();
});

const baseSub = {
  id: "s1",
  hub_id: "h1",
  plan_id: "hub_team",
  seat_count: 3,
  provider: "admin",
  billing_user_id: "u1",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("QuotaBanner", () => {
  it("renders nothing when subscription is null", () => {
    const { container } = render(
      <QuotaBanner subscription={null} memoryCount={0} memoryLimit={1000} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when subscription is active and under limit", () => {
    const { container } = render(
      <QuotaBanner
        subscription={{
          ...baseSub,
          status: "active",
          over_limit_since: null,
        }}
        memoryCount={100}
        memoryLimit={1000}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders inactive banner when subscription status is not active", () => {
    render(
      <QuotaBanner
        subscription={{ ...baseSub, status: "past_due" }}
        memoryCount={100}
        memoryLimit={1000}
      />,
    );
    expect(screen.getByText("Subscription inactive")).toBeTruthy();
    expect(screen.getByText("Subscription past_due.")).toBeTruthy();
  });

  it("renders over-limit banner mid-grace (days remaining)", () => {
    vi.useFakeTimers();
    const since = new Date("2026-01-01T00:00:00Z");
    vi.setSystemTime(new Date(since.getTime() + 3 * 24 * 60 * 60 * 1000));

    render(
      <QuotaBanner
        subscription={{
          ...baseSub,
          status: "active",
          over_limit_since: since.toISOString(),
        }}
        memoryCount={1030}
        memoryLimit={1000}
      />,
    );
    expect(screen.getByText("Over capacity")).toBeTruthy();
    expect(
      screen.getByText("1030/1000 memories, freeze in 4 day(s)"),
    ).toBeTruthy();
  });

  it("renders frozen banner when elapsed > grace", () => {
    vi.useFakeTimers();
    const since = new Date("2026-01-01T00:00:00Z");
    // 8 days elapsed — strictly over the 7d grace.
    vi.setSystemTime(new Date(since.getTime() + 8 * 24 * 60 * 60 * 1000));

    render(
      <QuotaBanner
        subscription={{
          ...baseSub,
          status: "active",
          over_limit_since: since.toISOString(),
        }}
        memoryCount={1200}
        memoryLimit={1000}
      />,
    );
    expect(screen.getByText("Frozen")).toBeTruthy();
  });
});
