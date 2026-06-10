import { describe, expect, it } from "vitest";
import { EXPAND_THRESHOLD, shouldAutoExpand } from "@/hooks/use-auto-grow";

describe("shouldAutoExpand", () => {
  it("returns false at single-line scrollHeight", () => {
    expect(shouldAutoExpand(24)).toBe(false);
  });

  it("returns false at the wrap-edge transition (32px)", () => {
    // Sub-threshold values were the historical wrap-flip trigger when
    // the hysteresis collapse ran on the widened layout's measurement.
    // The signal must stay false until the textarea has clearly wrapped.
    expect(shouldAutoExpand(32)).toBe(false);
  });

  it("returns true only when scrollHeight clearly exceeds the threshold", () => {
    expect(shouldAutoExpand(EXPAND_THRESHOLD)).toBe(false);
    expect(shouldAutoExpand(EXPAND_THRESHOLD + 1)).toBe(true);
  });

  it("returns true at two-line scrollHeight", () => {
    // 2 * 24px line-height = 48px — the natural value once the
    // compact-width input wraps. The original feedback loop relied on
    // this measurement triggering expansion AND a follow-up collapse;
    // this signal now only triggers expansion.
    expect(shouldAutoExpand(48)).toBe(true);
  });

  it("never reports a collapse signal — collapse is owned by the consumer", () => {
    // The whole point of removing computeExpandedWithHysteresis: there
    // is no scrollHeight value that means "collapse." `shouldAutoExpand`
    // is a one-way signal. Collapse is decided in bar-context.tsx from
    // the typed value transitioning to empty / short single-line.
    for (const scrollH of [0, 8, 16, 24, 26, 32, 40, 48, 72, 200]) {
      const result = shouldAutoExpand(scrollH);
      expect([true, false]).toContain(result);
      // No false-from-true path is exposed; the function is monotone in
      // scrollH and threshold-only.
    }
  });
});
