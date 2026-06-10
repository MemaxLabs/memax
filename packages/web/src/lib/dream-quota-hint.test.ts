import { describe, it, expect } from "vitest";
import { renderDreamQuotaHint } from "./dream-quota-hint";

const copy = {
  used: "{used} of {limit} dreams this month",
  unlimited: "Unlimited dreams",
  exhausted: "Out of dreams this month — resets {resetDate}",
  disabled: "Dreams aren't included in this hub's plan",
};

const interpolate = (template: string, vars: Record<string, string>): string =>
  template.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);

describe("renderDreamQuotaHint", () => {
  it("returns undefined when usage is unavailable (don't flash placeholder copy)", () => {
    expect(renderDreamQuotaHint(undefined, copy, interpolate)).toBeUndefined();
  });

  it("returns undefined for unlimited tiers (counter absence IS the signal)", () => {
    const result = renderDreamQuotaHint(
      {
        allowed: true,
        limit: -1,
        used: 9999,
        period_end: "2026-04-30T00:00:00Z",
      },
      copy,
      interpolate,
    );
    expect(result).toBeUndefined();
  });

  it("returns warn-tone disabled copy when tier limit is 0", () => {
    const result = renderDreamQuotaHint(
      { allowed: false, limit: 0, used: 0, period_end: "2026-04-30T00:00:00Z" },
      copy,
      interpolate,
    );
    expect(result).toEqual({
      text: "Dreams aren't included in this hub's plan",
      tone: "warn",
    });
  });

  it("returns info-tone counter for finite cap under limit", () => {
    const result = renderDreamQuotaHint(
      { allowed: true, limit: 40, used: 5, period_end: "2026-04-30T00:00:00Z" },
      copy,
      interpolate,
    );
    expect(result).toEqual({
      text: "5 of 40 dreams this month",
      tone: "info",
    });
  });

  it("returns warn-tone exhausted copy with reset date when allowed=false at finite cap", () => {
    // April ends on the 30th in the API contract; period_end is the
    // LAST day, so reset is May 1.
    const result = renderDreamQuotaHint(
      {
        allowed: false,
        limit: 40,
        used: 40,
        period_end: "2026-04-30T00:00:00Z",
      },
      copy,
      interpolate,
      { locale: "en-US" },
    );
    expect(result?.tone).toBe("warn");
    // Exact string depends on Intl impl across runtimes; assert the
    // shape (contains the right reset date) without pinning format.
    expect(result?.text).toContain("Out of dreams this month — resets");
    expect(result?.text).toMatch(/May\s*1/);
  });

  it("handles end-of-year period rollover correctly (Dec 31 → Jan 1)", () => {
    const result = renderDreamQuotaHint(
      {
        allowed: false,
        limit: 100,
        used: 100,
        period_end: "2026-12-31T00:00:00Z",
      },
      copy,
      interpolate,
      { locale: "en-US" },
    );
    expect(result?.text).toMatch(/Jan\s*1/);
  });

  // Regression for the timezone bug: period_end is a UTC boundary
  // (2026-04-30T00:00:00Z), reset = +1 UTC day = 2026-05-01T00:00:00Z.
  // In US Pacific that instant is Apr 30 17:00 local — a naive
  // toLocaleDateString without a timeZone option would render
  // "Apr 30". The helper must pin formatting to UTC.
  //
  // We can't easily change vitest's process timezone mid-test, so
  // we test the real fix by asserting the output matches the UTC
  // calendar date for a couple of known boundary instants. If the
  // helper ever drops the `timeZone: "UTC"` option, this test fails
  // on any CI runner whose system clock isn't UTC.
  it("formats reset date in UTC regardless of host timezone", () => {
    const cases = [
      { period_end: "2026-04-30T00:00:00Z", expected: /May\s*1/ },
      { period_end: "2026-12-31T00:00:00Z", expected: /Jan\s*1/ },
      // First-day-of-month boundary: period_end = Jan 31, reset Feb 1
      { period_end: "2026-01-31T00:00:00Z", expected: /Feb\s*1/ },
    ];
    for (const c of cases) {
      const result = renderDreamQuotaHint(
        { allowed: false, limit: 1, used: 1, period_end: c.period_end },
        copy,
        interpolate,
        { locale: "en-US" },
      );
      expect(result?.text).toMatch(c.expected);
    }
  });

  it("info-tone takes 'used' copy verbatim with vars interpolated", () => {
    const result = renderDreamQuotaHint(
      {
        allowed: true,
        limit: 100,
        used: 73,
        period_end: "2026-04-30T00:00:00Z",
      },
      copy,
      interpolate,
    );
    expect(result?.text).toBe("73 of 100 dreams this month");
  });
});
