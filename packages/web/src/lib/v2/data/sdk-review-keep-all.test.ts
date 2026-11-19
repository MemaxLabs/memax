import { describe, expect, it, vi } from "vitest";
import type { V2 } from "memax-sdk";
import { KEEP_ALL_BATCH, type ReviewItem } from "./review";
import { createSdkReview } from "./sdk-review";
import type { V2Client } from "./sdk-records";
import type { SpaceSummary } from "./types";

const space = { slug: "memax-v2" } as SpaceSummary;
const item = (n: number) =>
  ({ ref: `M-${String(n).padStart(4, "0")}`, version: 1 }) as ReviewItem;

describe("Review's keepAll through memax.v2", () => {
  it("sends 200 at a time, one key per request, and counts what stays", async () => {
    const keepMany = vi.fn(
      async (
        _space: string,
        input: V2.BulkReviewInput,
      ): Promise<V2.BulkReviewResult> =>
        ({
          items: input.items.map((i, at) => ({
            memory: i.memory,
            ref: i.memory,
            // The first of each request is refused by policy.
            outcome: at === 0 ? "refused" : "applied",
          })),
        }) as V2.BulkReviewResult,
    );
    const client = { v2: { memories: { keepMany } } } as unknown as V2Client;
    const review = createSdkReview(client, () => undefined);
    const items = Array.from({ length: KEEP_ALL_BATCH + 5 }, (_, n) => item(n));

    const result = await review.keepAll({
      space,
      items,
      idempotencyKey: "key",
    });

    expect(keepMany).toHaveBeenCalledTimes(2);
    expect(keepMany.mock.calls.map((c) => c[1].items.length)).toEqual([
      KEEP_ALL_BATCH,
      5,
    ]);
    expect(keepMany.mock.calls.map((c) => c[0])).toEqual([
      "memax-v2",
      "memax-v2",
    ]);
    expect(
      keepMany.mock.calls.map(
        (c) =>
          (c as unknown as [string, unknown, { idempotencyKey: string }])[2]
            .idempotencyKey,
      ),
    ).toEqual(["key:0", "key:1"]);
    // Each item goes with the version the person saw.
    expect(keepMany.mock.calls[0]![1].items[0]).toEqual({
      memory: "M-0000",
      version: 1,
    });
    expect(result.left).toBe(2);
    expect(result.kept).toHaveLength(KEEP_ALL_BATCH + 3);
  });
});
