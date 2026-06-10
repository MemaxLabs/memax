import { describe, it, expect } from "vitest";
import { dreamHistoryQueryKey } from "./use-dream-runs";

// The hook itself requires a QueryClient, so its full behavior
// is covered by integration + manual smoke. The pure key-builder
// is worth locking down — changing it silently would partition
// the cache and drop optimistic updates from other surfaces that
// touch the ["dreams", ...] namespace.
describe("dreamHistoryQueryKey", () => {
  it("namespaces by 'all' when hubId is undefined", () => {
    expect(dreamHistoryQueryKey(undefined)).toEqual([
      "dreams",
      "history",
      "all",
    ]);
  });

  it("uses the hubId when provided", () => {
    expect(dreamHistoryQueryKey("hub-1")).toEqual([
      "dreams",
      "history",
      "hub-1",
    ]);
  });

  it("partitions different hubs into separate cache entries", () => {
    // Different hub ids must NOT share cache — otherwise filtering
    // from "all" → "hub-1" would show stale "all" runs.
    expect(dreamHistoryQueryKey("hub-1")).not.toEqual(
      dreamHistoryQueryKey("hub-2"),
    );
  });
});
