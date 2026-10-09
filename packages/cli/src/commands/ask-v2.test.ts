import { describe, expect, it, vi } from "vitest";
import { MemaxError, type V2 } from "memax-sdk";
import { askV2, receiptPhrase } from "./ask-v2.js";

const space: V2.Space = {
  id: "22222222-2222-4222-8222-222222222222",
  tenant_id: "22222222-2222-4222-8222-222222222222",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  v2_enabled_at: "2026-10-01T00:00:00Z",
};

const now = new Date("2026-10-06T12:00:00Z");

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
    receipt: receipt as V2.Receipt,
  };
}

const river = source("M-0219", "Background jobs run on River.", {
  action: "kept",
  actor_kind: "person",
  occurred_at: "2026-10-02T17:12:00Z",
});
const temporal = source("M-0230", "Temporal was dropped.", {
  action: "kept",
  actor_kind: "agent",
  agent: "claude-code",
  occurred_at: "2026-08-21T10:00:00Z",
});
const usage = {
  input_tokens: 1,
  output_tokens: 1,
  retrieval_ms: 1,
  total_ms: 1,
};

function run(
  events: V2.AskEvent[],
  opts: { tty?: boolean; format?: string; error?: unknown } = {},
) {
  let written = "";
  const errs: string[] = [];
  const ask = vi.fn(async function* () {
    for (const e of events) yield e;
    if (opts.error) throw opts.error;
  });
  const exit = askV2(
    space,
    "Why River?",
    {
      memax: { v2: { ask } } as never,
      write: (s) => {
        written += s;
      },
      err: (l) => errs.push(l),
      tty: opts.tty ?? false,
      now,
    },
    { format: opts.format },
  );
  return exit.then((code) => ({ code, written, errs, ask }));
}

const answered: V2.AskEvent[] = [
  {
    event: "sources",
    data: { sources: [river, temporal], answering: true, lexical_only: true },
  },
  { event: "delta", data: { text: "Jobs run on River." } },
  { event: "cite", data: { n: 1, ref: "M-0219" } },
  { event: "delta", data: { text: " Temporal was dropped." } },
  { event: "cite", data: { n: 2, ref: "M-0230" } },
  {
    event: "done",
    data: {
      outcome: "answered",
      cited: ["M-0219", "M-0230"],
      dropped: 0,
      usage,
    },
  },
];

describe("memax ask on the V2 record", () => {
  it("streams the answer with its citations, then their receipts", async () => {
    const { code, written, ask } = await run(answered);
    expect(code).toBe(0);
    expect(ask).toHaveBeenCalledWith(space.id, "Why River?", {
      signal: undefined,
    });
    expect(written).toBe(
      "Jobs run on River. [1] Temporal was dropped. [2]\n" +
        "[1] M-0219 kept · Oct 2   [2] M-0230 kept by CC · Aug 21\n",
    );
  });

  it("says when nothing kept answers it", async () => {
    const { code, written } = await run([
      {
        event: "sources",
        data: { sources: [], answering: true, lexical_only: true },
      },
      {
        event: "done",
        data: { outcome: "not_covered", cited: [], dropped: 0, usage },
      },
    ]);
    expect(code).toBe(0);
    expect(written).toContain("Nothing kept in memax-v2 answers that yet.");
  });

  it("warns that an answer with no citation isn't one", async () => {
    const { written, errs } = await run([
      {
        event: "sources",
        data: { sources: [river], answering: true, lexical_only: true },
      },
      { event: "delta", data: { text: "Probably River." } },
      {
        event: "done",
        data: { outcome: "unsupported", cited: [], dropped: 1, usage },
      },
    ]);
    expect(written).toBe("Probably River.\n");
    expect(errs.join("\n")).toContain("cites nothing kept in memax-v2");
  });

  it("lists what matched when answers are off", async () => {
    const { written } = await run([
      {
        event: "sources",
        data: { sources: [river], answering: false, lexical_only: true },
      },
      {
        event: "done",
        data: { outcome: "sources_only", cited: [], dropped: 0, usage },
      },
    ]);
    expect(written).toContain("Answers are off on this server.");
    expect(written).toContain("[1] Background jobs run on River.");
    expect(written).toContain("M-0219 kept · Oct 2");
  });

  it("explains a refusal and fails", async () => {
    const limit = new MemaxError(
      "You've asked 50 questions this month.",
      "refused",
      403,
      {
        policy: {
          effect: "refuse",
          code: "ask_limit",
          message: "You've asked 50 questions this month.",
        },
      },
    );
    const { code, errs } = await run([], { error: limit });
    expect(code).toBe(1);
    expect(errs.join("\n")).toContain("50 questions");
  });

  it("fails when the model stops partway", async () => {
    const { code, written, errs } = await run([
      {
        event: "sources",
        data: { sources: [river], answering: true, lexical_only: true },
      },
      { event: "delta", data: { text: "Jobs" } },
      {
        event: "error",
        data: {
          code: "answer_failed",
          message: "The answer didn't finish. Ask again in a moment.",
        },
      },
    ]);
    expect(code).toBe(1);
    expect(written).toBe("Jobs\n");
    expect(errs.join("\n")).toContain("didn't finish");
  });

  it("prints one JSON object with --format json", async () => {
    const { code, written } = await run(answered, { format: "json" });
    expect(code).toBe(0);
    const out = JSON.parse(written);
    expect(out).toMatchObject({
      space: "memax-v2",
      outcome: "answered",
      answer: "Jobs run on River. [1] Temporal was dropped. [2]",
      citations: [
        { n: 1, ref: "M-0219", statement: "Background jobs run on River." },
        { n: 2, ref: "M-0230", statement: "Temporal was dropped." },
      ],
    });
  });

  it("names who kept a source", () => {
    expect(receiptPhrase(river, now)).toBe("kept · Oct 2");
    expect(receiptPhrase(temporal, now)).toBe("kept by CC · Aug 21");
    expect(
      receiptPhrase(
        source("M-0001", "x", {
          action: "merged",
          actor_kind: "dream",
          occurred_at: "2026-10-06T09:30:00Z",
        }),
        now,
      ),
    ).toMatch(/^merged by Dream · \d\d:\d\d$/);
  });
});
