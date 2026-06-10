// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  applyEventRoute,
  __registerRouteForTests,
  type StreamEnvelope,
} from "./event-routing";

describe("event-routing", () => {
  let qc: QueryClient;
  const cleanups: Array<() => void> = [];

  beforeEach(() => {
    qc = new QueryClient();
  });

  afterEach(() => {
    cleanups.splice(0).forEach((fn) => fn());
    qc.clear();
  });

  it("returns false for unknown event types (forward-compat no-op)", () => {
    const result = applyEventRoute(
      "unknown.event.type",
      {} as StreamEnvelope,
      qc,
    );
    expect(result).toBe(false);
  });

  it("invalidate: fires invalidateQueries for each key", () => {
    const spy = vi.spyOn(qc, "invalidateQueries");
    cleanups.push(
      __registerRouteForTests("test.invalidate.only", {
        invalidate: [["foo"], ["bar", "baz"]],
      }),
    );
    const result = applyEventRoute(
      "test.invalidate.only",
      {} as StreamEnvelope,
      qc,
    );
    expect(result).toBe(true);
    expect(spy).toHaveBeenCalledTimes(2);
    expect(spy).toHaveBeenCalledWith({ queryKey: ["foo"] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ["bar", "baz"] });
  });

  it("patch: called synchronously before invalidate", () => {
    const calls: string[] = [];
    const patch = vi.fn(() => calls.push("patch"));
    const invalidate = vi.fn();
    vi.spyOn(qc, "invalidateQueries").mockImplementation(
      invalidate as unknown as typeof qc.invalidateQueries,
    );

    cleanups.push(
      __registerRouteForTests("test.patch+invalidate", {
        patch,
        invalidate: [["x"]],
      }),
    );

    // Tie the invalidate spy to the sequence tracker too.
    invalidate.mockImplementation(() => {
      calls.push("invalidate");
      return Promise.resolve();
    });

    applyEventRoute(
      "test.patch+invalidate",
      { entity_id: "1" } as StreamEnvelope,
      qc,
    );

    expect(patch).toHaveBeenCalledTimes(1);
    expect(calls).toEqual(["patch", "invalidate"]);
  });

  it("hydrate: fire-and-forget, does not block invalidate", () => {
    type Resolver = () => void;
    let hydrateResolver: Resolver | undefined;
    const hydrate = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          hydrateResolver = resolve;
        }),
    );
    const spy = vi.spyOn(qc, "invalidateQueries");

    cleanups.push(
      __registerRouteForTests("test.hydrate+invalidate", {
        hydrate,
        invalidate: [["y"]],
      }),
    );

    applyEventRoute("test.hydrate+invalidate", {} as StreamEnvelope, qc);

    // Hydrate was called but has not resolved yet. Invalidate must
    // have run regardless — the point of hydrate being fire-and-forget
    // is that a slow fetch doesn't block broad refresh behavior.
    expect(hydrate).toHaveBeenCalledTimes(1);
    expect(spy).toHaveBeenCalledTimes(1);

    // Resolve the pending hydrate promise so no async work is left
    // dangling between tests.
    hydrateResolver?.();
  });

  it("payload is passed through to patch and hydrate handlers", () => {
    const payload: StreamEnvelope = {
      entity_id: "mem-123",
      state: "updated",
      hub_id: "hub-x",
    };
    const patch = vi.fn();
    const hydrate = vi.fn(() => undefined);

    cleanups.push(
      __registerRouteForTests("test.payload-passthrough", {
        patch,
        hydrate,
      }),
    );

    applyEventRoute("test.payload-passthrough", payload, qc);

    expect(patch).toHaveBeenCalledWith(payload, qc);
    expect(hydrate).toHaveBeenCalledWith(payload, qc);
  });
});
