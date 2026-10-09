import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, undoableAction } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "mxk_test",
    fetch: fetchMock,
    maxRetries: 0,
    rateLimitRetries: 0,
  });
  const call = (i = 0) => {
    const [url, init] = fetchMock.mock.calls[i] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body)),
    };
  };
  return { memax, call };
}

const action: V2.DreamAction = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70",
  edition_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71",
  edition_ref: "D-0214",
  n: 1,
  kind: "fade",
  note_refs: [],
  receipt_ids: ["0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72"],
  undoable: true,
  created_at: "2026-10-05T10:12:00Z",
};

const edition: V2.DreamEdition = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71",
  ref: "D-0214",
  n: 214,
  space_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
  slot: "2026-10-05T10:00:00Z",
  trigger: "schedule",
  until: "2026-10-05T10:11:19Z",
  started_at: "2026-10-05T10:11:19Z",
  finished_at: "2026-10-05T10:12:00Z",
  seconds: 41,
  notes_read: 34,
  notes_by: [{ kind: "agent", agent: "claude-code", count: 14 }],
  note_refs: ["N-1180"],
  fact_refs: ["M-0219"],
  counts: {
    fold: 2,
    propose: 4,
    dedupe: 0,
    conflict: 0,
    stale: 1,
    fade: 11,
    brief: 0,
  },
  undone: 0,
  needs_you: 2,
  receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a73",
  actions: [action],
};

describe("memax.v2.dream", () => {
  it("reads editions, latest first, and one edition by ref", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { items: [edition], has_more: false } }),
    );
    const page = await memax.v2.dream.editions("memax-v2", { limit: 5 });
    expect(page.items[0]?.ref).toBe("D-0214");
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/dream/editions?limit=5",
    );

    const one = client(jsonResponse({ data: edition }));
    const got = await one.memax.v2.dream.edition("memax-v2", "latest");
    expect(got.actions?.[0]?.kind).toBe("fade");
    expect(one.call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/dream/editions/latest",
    );
  });

  it("lists an edition's actions of one kind", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { items: [action], has_more: false } }),
    );
    await memax.v2.dream.actions("memax-v2", "D-0214", { kind: "fade" });
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/dream/editions/D-0214/actions?kind=fade",
    );
  });

  it("undoes one action, and every action of a kind, with the key", async () => {
    const undone = {
      ...action,
      undoable: false,
      undone: { receipt_id: action.receipt_ids[0], at: "2026-10-05T12:00:00Z" },
    };
    const { memax, call } = client(
      jsonResponse({
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          action: undone,
          memories: [],
          receipts: [],
        },
      }),
    );
    const res = await memax.v2.dream.undo(
      action.id,
      {},
      { idempotencyKey: "k1" },
    );
    expect(undoableAction(res.action)).toBe(false);
    expect(call().url).toBe(
      `https://api.memax.app/v2/dream/actions/${action.id}:undo`,
    );
    expect(call().headers["Idempotency-Key"]).toBe("k1");

    const all = client(
      jsonResponse({ data: { undone: [undone], refused: [] } }),
    );
    await all.memax.v2.dream.undoAll(
      "memax-v2",
      "D-0214",
      { kind: "fade" },
      { idempotencyKey: "k2" },
    );
    expect(all.call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/dream/editions/D-0214:undo",
    );
    expect(all.call().body).toEqual({ kind: "fade" });
  });

  it("restores a faded memory", async () => {
    const { memax, call } = client(
      jsonResponse({
        data: { outcome: "applied", policy: { effect: "apply" }, receipts: [] },
      }),
    );
    await memax.v2.memories.restore(
      "M-0044",
      {},
      { idempotencyKey: "k3", space: "memax-v2" },
    );
    expect(call().url).toBe(
      "https://api.memax.app/v2/memories/M-0044:restore?space=memax-v2",
    );
  });

  it("runs Dream now, and says when it's too soon", async () => {
    const { memax, call } = client(
      jsonResponse(
        {
          data: {
            queued: true,
            slot: "2026-10-07T12:00:00Z",
            trigger: "manual",
          },
        },
        { status: 202 },
      ),
    );
    const run = await memax.v2.dream.run("memax-v2", { idempotencyKey: "k4" });
    expect(run.trigger).toBe("manual");
    expect(call().url).toBe(
      "https://api.memax.app/v2/spaces/memax-v2/dream:run",
    );

    const late = client(
      jsonResponse(
        {
          error: {
            code: "rate_limited",
            message: "Give it a minute.",
            details: { retry_after: 30 },
          },
        },
        {
          status: 429,
          headers: { "Content-Type": "application/json", "Retry-After": "30" },
        },
      ),
    );
    await expect(
      late.memax.v2.dream.run("memax-v2", { idempotencyKey: "k5" }),
    ).rejects.toBeInstanceOf(MemaxError);
  });

  it("reads and changes the settings, and unsubscribes with a token", async () => {
    const { memax, call } = client(
      jsonResponse({
        data: {
          time_zone: "America/Vancouver",
          time_zone_source: "set",
          morning_email: false,
        },
      }),
    );
    await memax.v2.dream.updateSettings(
      { time_zone: "America/Vancouver", morning_email: false },
      { idempotencyKey: "k6" },
    );
    expect(call().method).toBe("PATCH");
    expect(call().body).toEqual({
      time_zone: "America/Vancouver",
      morning_email: false,
    });

    const unsub = client(jsonResponse({ data: { unsubscribed: true } }));
    await unsub.memax.v2.dream.unsubscribe("abc123");
    expect(unsub.call().url).toBe(
      "https://api.memax.app/v2/dream/email:unsubscribe?token=abc123",
    );
  });
});
