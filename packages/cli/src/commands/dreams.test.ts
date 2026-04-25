import { describe, expect, it } from "vitest";
import type { DreamUsage } from "memax-sdk";
import { formatQuota, summarizeRun } from "./dreams.js";

const buildUsage = (overrides: Partial<DreamUsage>): DreamUsage => ({
  scope: "personal",
  tier: "lucid",
  mode: "soft",
  limit: 40,
  used: 0,
  allowed: true,
  period_start: "2026-04-01T00:00:00Z",
  period_end: "2026-04-30T00:00:00Z",
  quota_source: "personal_pro",
  ...overrides,
});

describe("summarizeRun", () => {
  it("includes only non-zero dream outcomes", () => {
    expect(
      summarizeRun({
        id: "r1",
        owner_id: "u1",
        hub_id: "h1",
        status: "completed",
        started_at: "",
        finished_at: "",
        memories_scanned: 12,
        duplicates_merged: 2,
        contradictions_found: 1,
        memories_archived: 0,
        memories_organized: 5,
        topics_restructured: 0,
        report: "",
      }),
    ).toEqual(["2 merged", "1 contradictions", "5 organized"]);
  });

  it("returns an empty summary when no changes were made", () => {
    expect(
      summarizeRun({
        id: "r2",
        owner_id: "u1",
        hub_id: "h1",
        status: "completed",
        started_at: "",
        finished_at: "",
        memories_scanned: 2,
        duplicates_merged: 0,
        contradictions_found: 0,
        memories_archived: 0,
        memories_organized: 0,
        topics_restructured: 0,
        report: "",
      }),
    ).toEqual([]);
  });
});

describe("formatQuota", () => {
  it("personal Lucid under cap shows tier + counter + remaining + reset", () => {
    const lines = formatQuota(buildUsage({ scope: "personal", used: 5 }));
    expect(lines[0]).toBe("Dream quota — your personal quota");
    expect(lines).toContain("  tier:    Lucid");
    expect(lines).toContain("  used:    5 of 40");
    expect(lines).toContain("  remains: 35");
    // April → reset May 1 (UTC, no off-by-one).
    expect(lines.some((l) => /resets:\s+May\s*1/.test(l))).toBe(true);
  });

  it("hub scope swaps the headline copy", () => {
    const lines = formatQuota(
      buildUsage({ scope: "hub", hub_id: "h-team", limit: 100, used: 30 }),
    );
    expect(lines[0]).toBe("Dream quota — this hub");
    expect(lines).toContain("  used:    30 of 100");
  });

  it("Basic tier renders the right tier label", () => {
    const lines = formatQuota(
      buildUsage({ tier: "basic", quota_source: "personal_free" }),
    );
    expect(lines).toContain("  tier:    Basic");
  });

  it("unlimited (limit=-1) shows used count without cap line or reset", () => {
    const lines = formatQuota(
      buildUsage({ limit: -1, remaining: undefined, used: 999 }),
    );
    expect(lines).toContain("  used:    999 (unlimited)");
    expect(lines.some((l) => l.startsWith("  remains:"))).toBe(false);
    expect(lines.some((l) => l.startsWith("  resets:"))).toBe(false);
  });

  it("disabled tier (limit=0) shows upgrade hint, no counters", () => {
    const lines = formatQuota(
      buildUsage({
        scope: "hub",
        hub_id: "h-free",
        limit: 0,
        allowed: false,
        quota_source: "hub_free_team",
      }),
    );
    expect(lines[0]).toBe("Dream quota — this hub");
    expect(lines.some((l) => l.includes("doesn't have dreams included"))).toBe(
      true,
    );
    expect(lines.some((l) => l.includes("Upgrade:"))).toBe(true);
    // Should NOT show tier/used/remains/resets for a disabled tier —
    // those numbers would be misleading.
    expect(lines.some((l) => l.startsWith("  tier:"))).toBe(false);
    expect(lines.some((l) => l.startsWith("  used:"))).toBe(false);
  });

  it("exhausted finite cap (hard mode at-cap) shows (exhausted) suffix and skips remaining", () => {
    const lines = formatQuota(
      buildUsage({ mode: "hard", used: 40, limit: 40, allowed: false }),
    );
    expect(lines).toContain("  used:    40 of 40 (exhausted)");
    expect(lines.some((l) => l.startsWith("  remains:"))).toBe(false);
    // Reset still shows so the user knows when they get more.
    expect(lines.some((l) => /resets:/.test(l))).toBe(true);
  });

  it("soft-mode-at-cap (allowed=true, used>=limit) does NOT mark exhausted", () => {
    // Phase 1 contract: soft mode reports allowed=true past the
    // cap. UI should still surface the counter literally rather
    // than claim exhausted, because the button still works.
    const lines = formatQuota(
      buildUsage({ mode: "soft", used: 40, limit: 40, allowed: true }),
    );
    expect(lines).toContain("  used:    40 of 40");
    expect(lines.some((l) => l.includes("(exhausted)"))).toBe(false);
  });

  it("formats reset date in UTC regardless of host timezone", () => {
    // Same regression case as the web helper: April end → May 1, not Apr 30.
    const lines = formatQuota(buildUsage({ used: 1 }));
    expect(lines.some((l) => /May\s*1/.test(l))).toBe(true);
    expect(lines.some((l) => /Apr\s*30/.test(l))).toBe(false);

    // Year rollover.
    const yearEnd = formatQuota(
      buildUsage({ period_end: "2026-12-31T00:00:00Z" }),
    );
    expect(yearEnd.some((l) => /Jan\s*1/.test(l))).toBe(true);
  });
});
