import { describe, expect, it } from "vitest";
import { countPendingReviewNotifications } from "./notification-review-count";

describe("countPendingReviewNotifications", () => {
  it("counts only pending needs-action notifications", () => {
    expect(
      countPendingReviewNotifications([
        { kind: "review_contradiction", status: "pending" },
        { kind: "review_topic_merge", status: "pending" },
        { kind: "review_topic_restructure", status: "pending" },
        { kind: "dream_run_completed", status: "pending" },
        { kind: "review_contradiction", status: "resolved" },
      ]),
    ).toBe(3);
  });
});
