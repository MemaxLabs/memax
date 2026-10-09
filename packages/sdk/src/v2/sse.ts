// SPDX-License-Identifier: Apache-2.0
//
// A server-sent events reader with no dependencies, for the /v2 streams
// (Ask). It follows the WHATWG HTML "event stream" interpretation: UTF-8,
// an optional leading BOM, lines ending in CRLF, LF or CR (a CR at the end
// of one chunk and an LF at the start of the next are one line end),
// comments (":"), the event, data, id and retry fields, multi-line data
// joined with "\n", and an event dispatched at a blank line only when it
// has data. An event the stream ends inside is discarded, as browsers do.

/** One event, as sent: its name ("message" when the stream names none) and its data, unparsed. */
export interface ServerSentEvent {
  event: string;
  data: string;
  /** The last event id the stream set, if any. */
  id?: string;
  /** A reconnection time the stream asked for, in milliseconds. */
  retry?: number;
}

/** Turns decoded text into events, however the text is cut into chunks. */
export class EventStreamParser {
  private buffer = "";
  private started = false;
  private pendingCR = false;
  private name = "";
  private data: string[] = [];
  private lastId: string | undefined;
  private retry: number | undefined;

  /** Feeds the next chunk; returns the events it completed. */
  push(chunk: string): ServerSentEvent[] {
    let text = chunk;
    if (!this.started && text.length > 0) {
      this.started = true;
      if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);
    }
    if (this.pendingCR) {
      this.pendingCR = false;
      if (text.startsWith("\n")) text = text.slice(1);
    }
    this.buffer += text;
    const out: ServerSentEvent[] = [];
    for (;;) {
      const cr = this.buffer.indexOf("\r");
      const lf = this.buffer.indexOf("\n");
      if (cr < 0 && lf < 0) break;
      let end: number;
      let skip: number;
      if (lf >= 0 && (cr < 0 || lf < cr)) {
        end = lf;
        skip = 1;
      } else if (cr === this.buffer.length - 1) {
        // A CR at the end of the chunk: an LF may follow in the next one.
        end = cr;
        skip = 1;
        this.pendingCR = true;
      } else {
        end = cr;
        skip = this.buffer[cr + 1] === "\n" ? 2 : 1;
      }
      const line = this.buffer.slice(0, end);
      this.buffer = this.buffer.slice(end + skip);
      const ev = this.line(line);
      if (ev) out.push(ev);
    }
    return out;
  }

  /** Ends the stream: an event without its closing blank line is dropped. */
  end(): ServerSentEvent[] {
    this.buffer = "";
    this.name = "";
    this.data = [];
    return [];
  }

  private line(line: string): ServerSentEvent | undefined {
    if (line === "") {
      if (this.data.length === 0) {
        this.name = "";
        return undefined;
      }
      const ev: ServerSentEvent = {
        event: this.name || "message",
        data: this.data.join("\n"),
      };
      if (this.lastId !== undefined) ev.id = this.lastId;
      if (this.retry !== undefined) ev.retry = this.retry;
      this.name = "";
      this.data = [];
      return ev;
    }
    if (line.startsWith(":")) return undefined;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    switch (field) {
      case "event":
        this.name = value;
        break;
      case "data":
        this.data.push(value);
        break;
      case "id":
        if (!value.includes("\0")) this.lastId = value;
        break;
      case "retry":
        if (/^\d+$/.test(value)) this.retry = Number(value);
        break;
    }
    return undefined;
  }
}

/**
 * Reads a response body as server-sent events. Stopping early (a `break`
 * out of `for await`) or aborting `signal` cancels the body, which closes
 * the connection, so the server stops too.
 */
export async function* readEventStream(
  body: ReadableStream<Uint8Array>,
  signal?: AbortSignal,
): AsyncGenerator<ServerSentEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8");
  const parser = new EventStreamParser();
  const onAbort = () => {
    void reader.cancel(signal?.reason).catch(() => undefined);
  };
  signal?.addEventListener("abort", onAbort, { once: true });
  let finished = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        const tail = decoder.decode();
        if (tail) yield* parser.push(tail);
        parser.end();
        finished = true;
        return;
      }
      for (const ev of parser.push(decoder.decode(value, { stream: true }))) {
        yield ev;
      }
    }
  } finally {
    signal?.removeEventListener("abort", onAbort);
    if (!finished) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
