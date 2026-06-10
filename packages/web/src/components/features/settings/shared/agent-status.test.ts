import { describe, expect, it, vi } from "vitest";

import {
  getAgentStatus,
  getAgentStatusSortOrder,
} from "@/components/features/settings/shared/agent-status";

describe("agent status", () => {
  it("uses configured when there is no observed activity (never used)", () => {
    expect(
      getAgentStatus({
        memory_count: 0,
        last_active_at: null,
        last_observed_at: null,
      }),
    ).toBe("configured");
  });

  it("uses just_now within 5 minutes of activity", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-17T12:00:00Z"));

    expect(
      getAgentStatus({
        memory_count: 1,
        last_observed_at: "2026-04-17T11:57:00Z",
      }),
    ).toBe("just_now");

    vi.useRealTimers();
  });

  it("uses today for activity within the last 24 hours", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-17T12:00:00Z"));

    // 4h ago — well inside the today window but outside just_now.
    expect(
      getAgentStatus({
        memory_count: 1,
        last_observed_at: "2026-04-17T08:00:00Z",
      }),
    ).toBe("today");

    vi.useRealTimers();
  });

  it("uses idle for observed activity older than 24 hours", () => {
    // Distinct from configured (never used). Idle agents have a real
    // timestamp; they just haven't been touched in a while. Solid grey
    // dot vs outlined dot for configured.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-17T12:00:00Z"));

    // 2 days ago — past today window but has a real observed_at.
    expect(
      getAgentStatus({
        memory_count: 1,
        last_observed_at: "2026-04-15T12:00:00Z",
      }),
    ).toBe("idle");

    // Much older — still idle, not configured.
    expect(
      getAgentStatus({
        memory_count: 1,
        last_observed_at: "2026-04-01T12:00:00Z",
      }),
    ).toBe("idle");

    vi.useRealTimers();
  });

  it("sort order: just_now → today → idle → configured", () => {
    expect(getAgentStatusSortOrder("just_now")).toBe(0);
    expect(getAgentStatusSortOrder("today")).toBe(1);
    expect(getAgentStatusSortOrder("idle")).toBe(2);
    expect(getAgentStatusSortOrder("configured")).toBe(3);
  });
});
