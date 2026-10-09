// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { trackFunnelStep, type FunnelCounts } from "@/lib/v2/funnel";
import { useFunnelStep } from "./funnel";

// The first session's funnel events: one per step and mount, once it is
// reached, counts only, and none from the demo data.

const h = vi.hoisted(() => ({
  trackEvent: vi.fn(),
  kind: "sdk" as "sdk" | "demo",
}));
vi.mock("@/lib/posthog", () => ({ trackEvent: h.trackEvent }));
vi.mock("../(app)/_lib/data", () => ({ useSource: () => ({ kind: h.kind }) }));

afterEach(() => {
  cleanup();
  h.trackEvent.mockReset();
  h.kind = "sdk";
});

function Step({ ready, files }: { ready: boolean; files: number }) {
  useFunnelStep("first_import_seen", ready, { files });
  return null;
}

describe("the onboarding funnel", () => {
  it("sends a step once, when it is first reached, with counts only", () => {
    const { rerender } = render(<Step ready={false} files={0} />);
    expect(h.trackEvent).not.toHaveBeenCalled();
    rerender(<Step ready files={3} />);
    rerender(<Step ready files={4} />);
    expect(h.trackEvent).toHaveBeenCalledTimes(1);
    expect(h.trackEvent).toHaveBeenCalledWith("onboarding.first_import_seen", {
      files: 3,
      step: "first_import_seen",
    });
  });

  it("sends nothing from the demo data", () => {
    h.kind = "demo";
    render(<Step ready files={3} />);
    expect(h.trackEvent).not.toHaveBeenCalled();
  });

  it("names the step, and takes no words", () => {
    trackFunnelStep("signed_in");
    expect(h.trackEvent).toHaveBeenCalledWith("onboarding.signed_in", {
      step: "signed_in",
    });
    // @ts-expect-error a funnel event carries counts, never text
    const words: FunnelCounts = { path: "CLAUDE.md" };
    expect(words).toBeTruthy();
  });
});
