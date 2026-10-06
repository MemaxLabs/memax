import { describe, expect, it, vi } from "vitest";
import {
  EventStreamParser,
  Memax,
  MemaxError,
  readEventStream,
  refusalOf,
} from "../index.js";
import type { ServerSentEvent, V2 } from "../index.js";

function parseAll(chunks: string[]): ServerSentEvent[] {
  const p = new EventStreamParser();
  const out = chunks.flatMap((c) => p.push(c));
  return [...out, ...p.end()];
}

describe("EventStreamParser", () => {
  const stream =
    '\ufeff: hello\r\nevent: sources\r\ndata: {"a":1}\r\n\r\n' +
    "event: delta\ndata: one\ndata: two\nid: 7\nretry: 1500\n\n" +
    "data:no space\r\rdata: x\n";
  const want: ServerSentEvent[] = [
    { event: "sources", data: '{"a":1}' },
    { event: "delta", data: "one\ntwo", id: "7", retry: 1500 },
    { event: "message", data: "no space", id: "7", retry: 1500 },
  ];

  it("reads every field and line end, and drops an unfinished event", () => {
    expect(parseAll([stream])).toEqual(want);
  });

  it("gives the same events however the stream is cut", () => {
    for (let size = 1; size <= 9; size++) {
      const chunks: string[] = [];
      for (let i = 0; i < stream.length; i += size) {
        chunks.push(stream.slice(i, i + size));
      }
      expect(parseAll(chunks)).toEqual(want);
    }
  });

  it("ignores unknown fields, blank events and a lone field name", () => {
    expect(parseAll(["foo: bar\nevent: x\n\ndata\n\n"])).toEqual([
      { event: "message", data: "" },
    ]);
  });
});

function sseResponse(
  events: string[],
  opts: { hold?: boolean; onCancel?: () => void } = {},
): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const e of events) controller.enqueue(encoder.encode(e));
      if (!opts.hold) controller.close();
    },
    cancel() {
      opts.onCancel?.();
    },
  });
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

function client(fetchImpl: typeof fetch) {
  return new Memax({
    apiUrl: "https://api.memax.app",
    apiKey: "token",
    fetch: fetchImpl,
    maxRetries: 0,
  });
}

const source: V2.AskSource = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
  ref: "M-0219",
  statement: "Background jobs run on River.",
  section: "decisions",
  kind: "decision",
  state: "kept",
  trust: "person",
  version: 1,
};

const answer = [
  `event: sources\ndata: ${JSON.stringify({ sources: [source], answering: true, lexical_only: true })}\n\n`,
  'event: delta\ndata: {"text":"Jobs run on River."}\n\n',
  'event: cite\ndata: {"n":1,"ref":"M-0219"}\n\n',
  "event: newer-server-thing\ndata: {}\n\n",
  `event: done\ndata: ${JSON.stringify({ outcome: "answered", cited: ["M-0219"], dropped: 1, usage: { input_tokens: 9, output_tokens: 4, retrieval_ms: 20, total_ms: 900 } })}\n\n`,
];

describe("memax.v2.ask", () => {
  it("posts the question and yields typed events in order", async () => {
    const fetchMock = vi.fn(async () => sseResponse(answer));
    const events: V2.AskEvent[] = [];
    for await (const ev of client(fetchMock).v2.ask("memax-v2", "Why River?")) {
      events.push(ev);
    }
    expect(events.map((e) => e.event)).toEqual([
      "sources",
      "delta",
      "cite",
      "done",
    ]);
    const first = events[0];
    if (first?.event !== "sources") throw new Error("sources first");
    expect(first.data.sources[0]?.ref).toBe("M-0219");
    const done = events[3];
    if (done?.event !== "done") throw new Error("done last");
    expect(done.data.outcome).toBe("answered");

    const [url, init] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    expect(url).toBe("https://api.memax.app/v2/spaces/memax-v2/ask");
    expect(init.method).toBe("POST");
    expect(init.headers.Accept).toBe("text/event-stream");
    expect(init.headers["Idempotency-Key"]).toBeUndefined();
    expect(JSON.parse(String(init.body))).toEqual({ question: "Why River?" });
  });

  it("throws a refusal before the stream as a MemaxError", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "refused",
              message: "Asks start again on the 1st.",
              details: {
                policy: { effect: "refuse", code: "ask_limit" },
                limit: 50,
              },
            },
          }),
          { status: 403, headers: { "Content-Type": "application/json" } },
        ),
    );
    const run = async () => {
      for await (const ev of client(fetchMock).v2.ask("memax-v2", "Why?")) {
        void ev;
      }
    };
    const err = await run().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect(refusalOf(err)?.code).toBe("ask_limit");
    expect((err as MemaxError).details?.limit).toBe(50);
  });

  it("aborting mid-answer cancels the body and throws an AbortError", async () => {
    let cancelled = false;
    const controller = new AbortController();
    const fetchMock = vi.fn(async () =>
      sseResponse(answer.slice(0, 2), {
        hold: true,
        onCancel: () => {
          cancelled = true;
        },
      }),
    );
    const seen: string[] = [];
    const err = await (async () => {
      for await (const ev of client(fetchMock).v2.ask("memax-v2", "Why?", {
        signal: controller.signal,
      })) {
        seen.push(ev.event);
        if (ev.event === "delta") controller.abort();
      }
    })().catch((e: unknown) => e);
    expect(seen).toEqual(["sources", "delta"]);
    expect((err as Error).name).toBe("AbortError");
    expect(cancelled).toBe(true);
  });

  it("leaving the loop early closes the connection", async () => {
    let cancelled = false;
    const fetchMock = vi.fn(async () =>
      sseResponse(answer.slice(0, 2), {
        hold: true,
        onCancel: () => {
          cancelled = true;
        },
      }),
    );
    for await (const ev of client(fetchMock).v2.ask("memax-v2", "Why?")) {
      if (ev.event === "sources") break;
    }
    expect(cancelled).toBe(true);
  });
});

describe("readEventStream", () => {
  it("decodes UTF-8 split across chunks", async () => {
    const bytes = new TextEncoder().encode("data: 用 River\n\n");
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(bytes.slice(0, 7));
        c.enqueue(bytes.slice(7));
        c.close();
      },
    });
    const got: ServerSentEvent[] = [];
    for await (const ev of readEventStream(body)) got.push(ev);
    expect(got).toEqual([{ event: "message", data: "用 River" }]);
  });
});
