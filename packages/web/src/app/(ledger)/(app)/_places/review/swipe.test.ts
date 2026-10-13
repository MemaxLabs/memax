import { describe, expect, it } from "vitest";
import { swipeDecision, swipeThreshold } from "./swipe";

// What letting go of a card decides (MobileReview.png): past the line, or
// a flick, right keeps and left rejects; anything less puts it back.

describe("the swipe's line", () => {
  it("is 30% of the card, at most 120px, and 120px before layout", () => {
    expect(swipeThreshold(358)).toBeCloseTo(107.4);
    expect(swipeThreshold(600)).toBe(120);
    expect(swipeThreshold(0)).toBe(120);
  });
});

describe("letting go", () => {
  const card = 358;
  it.each([
    // dx, ms, decides
    [120, 400, "keep"],
    [-120, 400, "reject"],
    [100, 400, null], // short of the line, slowly
    [-100, 400, null],
    [60, 50, "keep"], // a flick right
    [-60, 50, "reject"], // a flick left
    [40, 20, null], // too short to be a flick
    [0, 10, null],
  ] as const)("at %ipx after %ims: %s", (dx, ms, decides) => {
    expect(swipeDecision(dx, ms, card)).toBe(decides);
  });
});
