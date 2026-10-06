import { describe, expect, it, vi } from "vitest";
import { DEMO_SPACES } from "./demo-dataset";
import { createDemoSource } from "./demo-source";
import { pickDataMode } from "./mode";
import { createSdkSource } from "./sdk-source";
import type { LedgerDataSource } from "./source";
import type { AskEvent } from "./types";

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

async function collect(events: AsyncIterable<AskEvent>) {
  const out: AskEvent[] = [];
  for await (const event of events) out.push(event);
  return out;
}

describe("pickDataMode", () => {
  it.each([
    [{ hasSession: true, dataCookie: undefined, devFixtures: false }, "sdk"],
    [{ hasSession: true, dataCookie: undefined, devFixtures: true }, "sdk"],
    [{ hasSession: true, dataCookie: "demo", devFixtures: true }, "demo"],
    // The demo cookie means nothing in production.
    [{ hasSession: true, dataCookie: "demo", devFixtures: false }, "sdk"],
    [{ hasSession: false, dataCookie: undefined, devFixtures: true }, "demo"],
    [{ hasSession: false, dataCookie: "demo", devFixtures: false }, "signin"],
  ] as const)("%j → %s", (input, mode) => {
    expect(pickDataMode(input)).toBe(mode);
  });
});

describe("the demo source", () => {
  const demo = createDemoSource({ streamDelayMs: 0 });

  it("has its data on hand for the first render", () => {
    expect(demo.peek?.spaces().map((s) => s.slug)).toEqual([
      "personal",
      "memax-v2",
      "memax-web",
      "memax-team",
    ]);
    expect(demo.peek?.overview("memax-v2")?.waiting).toBe(5);
    expect(demo.viewer?.initials).toBe("ZZ");
    expect(demo.now().toISOString()).toBe("2026-10-05T21:40:00.000Z");
  });

  it("streams Ask.png's answer, with sources first", async () => {
    const events = await collect(
      demo.ask({ space: v2, question: "Why did we pick River over Temporal?" }),
    );
    expect(events[0]).toMatchObject({ type: "sources" });
    expect(events.at(-1)).toEqual({ type: "done" });
    const text = events
      .flatMap((e) =>
        e.type === "part" && e.part.kind !== "cite" ? [e.part.text] : [],
      )
      .join("");
    expect(text).toContain("River runs on the Postgres we already operate");
    expect(
      events.filter((e) => e.type === "part" && e.part.kind === "cite"),
    ).toHaveLength(3);
  });

  it("says when nothing answers", async () => {
    expect(await collect(demo.ask({ space: v2, question: "Lunch?" }))).toEqual([
      { type: "none" },
    ]);
  });

  it("stops streaming when aborted", async () => {
    const slow = createDemoSource({ streamDelayMs: 5 });
    const controller = new AbortController();
    const seen: AskEvent[] = [];
    const run = (async () => {
      for await (const event of slow.ask({
        space: v2,
        question: "River?",
        signal: controller.signal,
      })) {
        seen.push(event);
        if (seen.length === 3) controller.abort();
      }
    })();
    await run;
    expect(seen.length).toBe(3);
    expect(seen.some((e) => e.type === "done")).toBe(false);
  });

  it("offers Remember.png's near-duplicate, and keeps with new IDs", async () => {
    const check = await demo.checkRemember({
      space: v2,
      statement: "Pin shared dependency versions with the pnpm catalog.",
    });
    expect(check.duplicate).toMatchObject({
      ref: "M-0432",
      agent: "codex",
      lifecycle: "proposed",
      match: "near",
    });
    expect(check.condition?.subject).toBe("pnpm-workspace.yaml");
    const none = await demo.checkRemember({ space: v2, statement: "Tabs." });
    expect(none.duplicate).toBeNull();
    const first = await demo.remember({
      space: v2,
      statement: "x",
      section: "conventions",
      idempotencyKey: "a",
    });
    const second = await demo.remember({
      space: v2,
      statement: "y",
      section: "conventions",
      idempotencyKey: "b",
    });
    expect([first.ref, second.ref]).toEqual(["M-0439", "M-0440"]);
    expect(first).toMatchObject({ outcome: "kept", recompiled: 3 });
    // A fresh Remember has no undo on the server, so none here either.
    expect(first.receipt).toBeNull();
  });
});

function fakeClient() {
  return {
    v2: {
      spaces: {
        list: vi.fn().mockResolvedValue({
          items: [
            {
              id: "s1",
              tenant_id: "t1",
              slug: "memax-v2",
              name: "memax-v2",
              kind: "project",
              role: "owner",
              repository: "MemaxLabs/memax",
            },
          ],
        }),
      },
      review: {
        list: vi.fn().mockResolvedValue({
          items: [{ created_at: "2026-10-05T11:40:00Z" }],
          has_more: true,
          total: 7,
        }),
      },
      memories: {
        nearDuplicates: vi.fn().mockResolvedValue({
          items: [
            {
              memory: {
                ref: "M-0432",
                lifecycle: "proposed",
                section: "conventions",
              },
              similarity: 0.95,
              match: "near",
              created: {
                actor_kind: "agent",
                agent: "codex",
                occurred_at: "2026-10-05T14:18:00Z",
              },
            },
          ],
          semantic: true,
          floor: 0.9,
        }),
        list: vi.fn().mockResolvedValue({ items: [{}], has_more: false }),
        remember: vi.fn().mockResolvedValue({
          outcome: "applied",
          memory: { ref: "M-0500" },
          receipts: [{}],
        }),
        keep: vi.fn().mockResolvedValue({
          outcome: "proposed",
          memory: { ref: "M-0432" },
          receipts: [{}],
        }),
      },
      receipts: {
        list: vi.fn().mockResolvedValue({ items: [], has_more: false }),
      },
    },
  };
}

describe("the SDK source", () => {
  function setup() {
    const client = fakeClient();
    const source: LedgerDataSource = createSdkSource({
      client: client as never,
      viewer: { initials: "ZZ", name: "Ziyang Zeng", timeZone: "UTC" },
    });
    return { client, source };
  }

  it("lists spaces from memax.v2.spaces, with counts it can't know yet left null", async () => {
    const { source } = setup();
    const [space] = await source.spaces();
    expect(space).toEqual({
      id: "s1",
      slug: "memax-v2",
      name: "memax-v2",
      kind: "project",
      role: "owner",
      repository: "MemaxLabs/memax",
      kept: null,
      agents: null,
      people: null,
      waiting: null,
    });
    expect(source.peek).toBeUndefined();
  });

  it("takes the Review count from the queue's total", async () => {
    const { client, source } = setup();
    const overview = await source.overview(v2);
    expect(client.v2.review.list).toHaveBeenCalledWith("memax-v2", {
      limit: 1,
      signal: undefined,
    });
    expect(overview.waiting).toBe(7);
    expect(overview.oldestWaitingAt).toBe("2026-10-05T11:40:00Z");
    expect(overview.memories.any).toBe(true);
    expect(overview.activity.any).toBe(false);
    // Not served by /v2 yet: placeholders, never demo data.
    expect(overview.openHandoffs).toBeNull();
    expect(overview.agents).toBeNull();
    expect(overview.status).toEqual({ kind: "not-compiling" });
  });

  it("remembers and keeps through memax.v2.memories with the idempotency key", async () => {
    const { client, source } = setup();
    const kept = await source.remember({
      space: v2,
      statement: "River, not Temporal.",
      section: "decisions",
      idempotencyKey: "key-1",
    });
    expect(client.v2.memories.remember).toHaveBeenCalledWith(
      "memax-v2",
      { statement: "River, not Temporal.", section: "decisions" },
      { idempotencyKey: "key-1" },
    );
    expect(kept).toEqual({
      ref: "M-0500",
      outcome: "kept",
      recompiled: null,
      receipt: null,
    });
    const proposal = await source.keepProposal({
      space: v2,
      ref: "M-0432",
      idempotencyKey: "key-2",
    });
    expect(client.v2.memories.keep).toHaveBeenCalledWith(
      "M-0432",
      {},
      { space: "memax-v2", idempotencyKey: "key-2" },
    );
    expect(proposal.outcome).toBe("proposed");
  });

  it("carries a Keep's receipt for Undo, and undoes by receipt", async () => {
    const { client, source } = setup();
    client.v2.memories.keep.mockResolvedValueOnce({
      outcome: "applied",
      memory: { ref: "M-0432" },
      receipts: [{ id: "r-keep" }],
    });
    const kept = await source.keepProposal({
      space: v2,
      ref: "M-0432",
      idempotencyKey: "key-3",
    });
    expect(kept.receipt).toBe("r-keep");
    const undo = vi.fn().mockResolvedValue({
      memories: [{ ref: "M-0432" }],
      receipts: [{ id: "r-undid" }],
    });
    (client.v2 as unknown as { receipts: Record<string, unknown> }).receipts = {
      ...client.v2.receipts,
      undo,
    };
    expect(
      await source.undo({ space: v2, receipt: "r-keep", idempotencyKey: "u1" }),
    ).toEqual({ refs: ["M-0432"] });
    expect(undo).toHaveBeenCalledWith("r-keep", {}, { idempotencyKey: "u1" });
  });

  it("checks Remember's draft with memax.v2.memories.nearDuplicates", async () => {
    const { client, source } = setup();
    const controller = new AbortController();
    const check = await source.checkRemember({
      space: v2,
      statement: "  Pin shared dependency versions with the pnpm catalog. ",
      signal: controller.signal,
    });
    expect(client.v2.memories.nearDuplicates).toHaveBeenCalledWith(
      "memax-v2",
      {
        statement: "Pin shared dependency versions with the pnpm catalog.",
        limit: 1,
      },
      { signal: controller.signal },
    );
    expect(check).toEqual({
      duplicate: {
        ref: "M-0432",
        lifecycle: "proposed",
        agent: "codex",
        writtenAt: "2026-10-05T14:18:00Z",
        match: "near",
      },
      section: "conventions",
      condition: null,
    });
    // An empty draft asks nothing.
    client.v2.memories.nearDuplicates.mockClear();
    expect(await source.checkRemember({ space: v2, statement: " " })).toEqual({
      duplicate: null,
      section: null,
      condition: null,
    });
    expect(client.v2.memories.nearDuplicates).not.toHaveBeenCalled();
  });

  it("maps a kept repeat a person wrote, and offers no open-question section", async () => {
    const { rememberCheckOf } = await import("./remember-sdk");
    const check = rememberCheckOf({
      items: [
        {
          memory: {
            ref: "M-0219",
            lifecycle: "kept",
            section: "open_question",
          },
          similarity: 1,
          match: "exact",
          created: {
            actor_kind: "person",
            occurred_at: "2026-10-01T09:00:00Z",
          },
        },
      ],
      semantic: false,
      floor: 0.9,
    } as never);
    expect(check.duplicate).toEqual({
      ref: "M-0219",
      lifecycle: "kept",
      agent: null,
      writtenAt: "2026-10-01T09:00:00Z",
      match: "exact",
    });
    expect(check.section).toBeNull();
    expect(
      rememberCheckOf({ items: [], semantic: true, floor: 0.9 }).duplicate,
    ).toBeNull();
  });

  it("lets Keep go ahead when the check fails, but not when it was aborted", async () => {
    const { client, source } = setup();
    const { MemaxError } = await import("memax-sdk");
    client.v2.memories.nearDuplicates.mockRejectedValueOnce(
      new MemaxError("Too many", "rate_limited", 429),
    );
    expect(await source.checkRemember({ space: v2, statement: "x" })).toEqual({
      duplicate: null,
      section: null,
      condition: null,
    });
    const controller = new AbortController();
    controller.abort();
    client.v2.memories.nearDuplicates.mockRejectedValueOnce(
      new DOMException("aborted", "AbortError"),
    );
    await expect(
      source.checkRemember({
        space: v2,
        statement: "x",
        signal: controller.signal,
      }),
    ).rejects.toThrow("aborted");
  });

  it("can't ask yet, and says so", async () => {
    const { source } = setup();
    expect(await collect(source.ask({ space: v2, question: "?" }))).toEqual([
      { type: "unavailable" },
    ]);
  });
});
