import { afterEach, describe, expect, it, vi } from "vitest";
import { adminV2MetricsClient, lastWeeks } from "./v2-metrics";

// The gate metrics are asked for by signup week: Monday-based UTC weeks,
// as the server counts cohorts.

afterEach(() => {
  vi.restoreAllMocks();
});

describe("the gate metrics client", () => {
  it("asks for the last weeks, this one included", () => {
    // Wednesday Oct 7, 2026, late in the day west of UTC.
    expect(lastWeeks(8, new Date("2026-10-07T23:30:00Z"))).toEqual({
      from: "2026-08-17",
      to: "2026-10-12",
    });
    expect(lastWeeks(1, new Date("2026-10-12T00:00:00Z"))).toEqual({
      from: "2026-10-12",
      to: "2026-10-19",
    });
  });

  it("calls the admin endpoint with the range", async () => {
    const fetch = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(
        new Response(JSON.stringify({ data: { gates: [] } }), { status: 200 }),
      );
    const out = await adminV2MetricsClient.get({
      from: "2026-08-17",
      to: "2026-10-12",
    });
    expect(out).toEqual({ gates: [] });
    expect(fetch.mock.calls[0][0]).toMatch(
      /\/v1\/admin\/v2\/metrics\?from=2026-08-17&to=2026-10-12$/,
    );
  });
});
