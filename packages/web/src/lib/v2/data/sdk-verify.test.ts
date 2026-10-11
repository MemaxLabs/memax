import { describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { staleWhyOf, verifyBody, verifyResultOf } from "./sdk-verify";

const ME = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72";

function receipt(fields: Partial<V2.Receipt>): V2.Receipt {
  return {
    id: "r1",
    seq: 1,
    tenant_id: "t",
    space_id: "s",
    object_kind: "memory",
    object_id: "m",
    object_ref: "M-0187",
    action: "kept",
    actor_kind: "person",
    actor_id: ME,
    via: "web",
    occurred_at: "2026-07-14T11:20:00Z",
    recorded_at: "2026-07-14T11:20:00Z",
    stream_id: "m",
    stream_version: 1,
    ...fields,
  };
}

const report: V2.StaleReport = {
  memory_id: "m",
  ref: "M-0187",
  space_id: "s",
  version: 1,
  statement: "Ask memax answers with the Haiku tier.",
  kind: "fact",
  state: "stale",
  stale: true,
  stale_after: "2026-10-01T00:00:00Z",
  flagged: receipt({
    id: "r2",
    action: "flagged",
    actor_kind: "dream",
    actor_id: undefined,
    via: "system",
    source: { kind: "dream", ref: "D-0214" },
    occurred_at: "2026-10-05T03:12:00Z",
  }),
  kept: receipt({}),
  reads_since: 88,
  causes: [{ kind: "date", stale_after: "2026-10-01T00:00:00Z" }],
};

describe("VerifyStale through memax.v2", () => {
  it("reads why a memory is stale", () => {
    expect(staleWhyOf(report, ME)).toEqual({
      ref: "M-0187",
      version: 1,
      statement: "Ask memax answers with the Haiku tier.",
      kind: "fact",
      stale: true,
      since: "2026-10-05T03:12:00Z",
      flaggedBy: { kind: "dream" },
      kept: {
        by: { kind: "person", self: true },
        at: "2026-07-14T11:20:00Z",
        action: "kept",
      },
      readsSince: 88,
      causes: [{ kind: "date", staleAfter: "2026-10-01T00:00:00Z" }],
      suggestion: null,
    });
  });

  it("leaves out what it isn't stale for any more", () => {
    const why = staleWhyOf(
      { ...report, stale: false, causes: [], flagged: undefined },
      ME,
    );
    expect(why).toMatchObject({ stale: false, since: null, causes: [] });
  });

  it("sends each answer's body", () => {
    expect(verifyBody({ answer: "still_true", statement: "x" })).toEqual({
      answer: "still_true",
    });
    expect(verifyBody({ answer: "update", statement: "New words." })).toEqual({
      answer: "update",
      statement: "New words.",
    });
    expect(
      verifyBody({ answer: "forget", carries: ["M-0202"], note: "  old  " }),
    ).toEqual({ answer: "forget", carries: ["M-0202"], note: "old" });
    expect(verifyBody({ answer: "forget" })).toEqual({
      answer: "forget",
      carries: [],
    });
  });

  it("undoes by the verified receipt, and never a Forget", () => {
    const memory = { ref: "M-0187", version: 2 } as V2.Memory;
    const edited = receipt({ id: "e", action: "edited" });
    const verified = receipt({ id: "v", action: "verified" });
    expect(
      verifyResultOf("update", {
        outcome: "applied",
        policy: { effect: "apply" },
        memory,
        receipts: [edited, verified],
      }),
    ).toEqual({ ref: "M-0187", answer: "update", version: 2, receipt: "v" });
    expect(
      verifyResultOf("forget", {
        outcome: "applied",
        policy: { effect: "apply" },
        memory,
        receipts: [receipt({ id: "f", action: "forgot" })],
      }).receipt,
    ).toBeNull();
  });
});
