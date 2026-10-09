import { describe, expect, it, vi } from "vitest";
import { MemaxError, type V2 } from "memax-sdk";
import { createSdkAsk } from "./ask-sdk";
import type { AskEvent, SpaceSummary, Viewer } from "./types";

const space: SpaceSummary = {
  id: "s1",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  kept: null,
  agents: null,
  people: null,
  waiting: null,
};

const viewer: Viewer = {
  id: "u-zz",
  initials: "ZZ",
  name: "Ziyang Zeng",
  timeZone: "UTC",
};

function source(
  ref: string,
  statement: string,
  receipt?: Partial<V2.Receipt>,
): V2.AskSource {
  return {
    id: `id-${ref}`,
    ref,
    statement,
    section: "decisions",
    kind: "fact",
    state: "kept",
    trust: "person",
    version: 1,
    receipt: receipt as V2.Receipt | undefined,
  };
}

const river = source("M-0219", "Background jobs run on River.", {
  action: "kept",
  actor_kind: "person",
  actor_id: "u-zz",
  occurred_at: "2026-10-02T17:12:00Z",
});
const temporal = source("M-0230", "Temporal was dropped in August.", {
  action: "kept",
  actor_kind: "agent",
  agent: "claude-code",
  occurred_at: "2026-08-21T10:00:00Z",
});
const unrelated = source("M-0301", "The web app is Next.js.");

function clientWith(events: V2.AskEvent[], error?: unknown) {
  const ask = vi.fn(async function* () {
    for (const e of events) yield e;
    if (error) throw error;
  });
  return { client: { v2: { ask } } as never, ask };
}

async function collect(it: AsyncIterable<AskEvent>): Promise<AskEvent[]> {
  const out: AskEvent[] = [];
  for await (const e of it) out.push(e);
  return out;
}

const usage = {
  input_tokens: 1,
  output_tokens: 1,
  retrieval_ms: 1,
  total_ms: 1,
};

describe("Ask over memax.v2.ask", () => {
  it("adds a source when it is first cited, numbered as cited, and ends done", async () => {
    const { client, ask } = clientWith([
      {
        event: "sources",
        data: {
          sources: [unrelated, river, temporal],
          answering: true,
          lexical_only: true,
        },
      },
      { event: "delta", data: { text: "Jobs run on River." } },
      { event: "cite", data: { n: 1, ref: "M-0219" } },
      { event: "delta", data: { text: " Temporal was dropped." } },
      { event: "cite", data: { n: 2, ref: "M-0230" } },
      { event: "cite", data: { n: 1, ref: "M-0219" } },
      {
        event: "done",
        data: {
          outcome: "answered",
          cited: ["M-0219", "M-0230"],
          dropped: 0,
          usage,
        },
      },
    ]);
    const signal = new AbortController().signal;
    const events = await collect(
      createSdkAsk(
        client,
        () => viewer,
      )({ space, question: "Why River?", signal }),
    );
    expect(ask).toHaveBeenCalledWith("memax-v2", "Why River?", { signal });
    expect(events.map((e) => e.type)).toEqual([
      "part",
      "sources",
      "part",
      "part",
      "sources",
      "part",
      "part",
      "done",
    ]);
    const last = events.filter((e) => e.type === "sources").at(-1);
    expect(last).toEqual({
      type: "sources",
      sources: [
        {
          n: 1,
          statement: "Background jobs run on River.",
          state: "kept",
          receipt: {
            person: "ZZ",
            action: "kept",
            at: "2026-10-02T17:12:00Z",
            ref: "M-0219",
          },
          memory: "M-0219",
          section: "decisions",
        },
        {
          n: 2,
          statement: "Temporal was dropped in August.",
          state: "kept",
          receipt: {
            agent: "claude-code",
            action: "kept",
            at: "2026-08-21T10:00:00Z",
            ref: "M-0230",
          },
          memory: "M-0230",
          section: "decisions",
        },
      ],
    });
  });

  it.each([
    ["not_covered", { type: "none" }],
    ["unsupported", { type: "none" }],
  ] as const)("ends %s as nothing kept answers it", async (outcome, want) => {
    const { client } = clientWith([
      {
        event: "sources",
        data: { sources: [river], answering: true, lexical_only: false },
      },
      { event: "delta", data: { text: "A guess." } },
      { event: "done", data: { outcome, cited: [], dropped: 2, usage } },
    ]);
    const events = await collect(
      createSdkAsk(client, () => viewer)({ space, question: "?" }),
    );
    expect(events.at(-1)).toEqual(want);
  });

  it("shows what matched when answers are off", async () => {
    const { client } = clientWith([
      {
        event: "sources",
        data: {
          sources: [river, unrelated],
          answering: false,
          lexical_only: true,
        },
      },
      {
        event: "done",
        data: { outcome: "sources_only", cited: [], dropped: 0, usage },
      },
    ]);
    const events = await collect(
      createSdkAsk(client, () => viewer)({ space, question: "?" }),
    );
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({
      type: "off",
      sources: [
        { n: 1, memory: "M-0219" },
        { n: 2, memory: "M-0301" },
      ],
    });
  });

  it("turns the plan limit into its own state, and a mid-answer failure into an error", async () => {
    const limited = new MemaxError("Used up.", "refused", 403, {
      policy: { effect: "refuse", code: "ask_limit" },
      limit: 50,
      reset_at: "2026-11-01T00:00:00Z",
    });
    const { client } = clientWith([], limited);
    expect(
      await collect(
        createSdkAsk(client, () => viewer)({ space, question: "?" }),
      ),
    ).toEqual([{ type: "limit", limit: 50, resetAt: "2026-11-01T00:00:00Z" }]);
    const failing = clientWith([
      {
        event: "sources",
        data: { sources: [river], answering: true, lexical_only: true },
      },
      { event: "delta", data: { text: "Jobs" } },
      {
        event: "error",
        data: { code: "answer_failed", message: "The answer didn't finish." },
      },
    ]);
    await expect(
      collect(
        createSdkAsk(failing.client, () => viewer)({ space, question: "?" }),
      ),
    ).rejects.toThrow("didn't finish");
  });

  it("is quiet when the person stopped it", async () => {
    const controller = new AbortController();
    controller.abort();
    const err = Object.assign(new Error("Aborted"), { name: "AbortError" });
    const { client } = clientWith([], err);
    expect(
      await collect(
        createSdkAsk(
          client,
          () => viewer,
        )({ space, question: "?", signal: controller.signal }),
      ),
    ).toEqual([]);
  });
});
