import { MemaxError, refusalOf, type Memax, type V2 } from "memax-sdk";
import { actorOf } from "./sdk-records";
import type {
  AskEvent,
  AskSource,
  ReceiptLine,
  SpaceSummary,
  Viewer,
} from "./types";

/**
 * Ask over memax.v2.ask (plan §5.11): the server's stream mapped onto
 * the frame's AskEvent. The server sends every memory the answer may
 * cite first; the panel shows only the ones it does cite, numbered in
 * citation order, so a source is added when it is first cited. An answer
 * that ends not_covered or unsupported (no citation the server let
 * through) becomes "none", replacing any words that streamed.
 */
export function createSdkAsk(
  client: Pick<Memax, "v2">,
  viewer: () => Viewer | null,
) {
  return async function* ask({
    space,
    question,
    signal,
  }: {
    space: SpaceSummary;
    question: string;
    signal?: AbortSignal;
  }): AsyncGenerator<AskEvent> {
    const byRef = new Map<string, V2.AskSource>();
    let matched: V2.AskSource[] = [];
    const cited: AskSource[] = [];
    try {
      for await (const ev of client.v2.ask(space.slug, question, {
        signal,
      })) {
        switch (ev.event) {
          case "sources":
            matched = ev.data.sources;
            for (const s of matched) byRef.set(s.ref, s);
            break;
          case "delta":
            yield { type: "part", part: { kind: "text", text: ev.data.text } };
            break;
          case "cite": {
            const { n, ref } = ev.data;
            const s = byRef.get(ref);
            if (s && !cited.some((c) => c.n === n)) {
              cited.push(askSourceOf(s, n, viewer()));
              yield { type: "sources", sources: [...cited] };
            }
            yield { type: "part", part: { kind: "cite", n } };
            break;
          }
          case "done":
            if (ev.data.outcome === "answered") {
              yield { type: "done" };
            } else if (ev.data.outcome === "sources_only") {
              yield {
                type: "off",
                sources: matched.map((s, i) => askSourceOf(s, i + 1, viewer())),
              };
            } else {
              yield { type: "none" };
            }
            return;
          case "error":
            throw new MemaxError(ev.data.message, ev.data.code, 200);
        }
      }
    } catch (err) {
      if (signal?.aborted) return;
      const refused = refusalOf(err);
      if (refused?.code === "ask_limit" && err instanceof MemaxError) {
        const limit = Number(err.details?.limit ?? 0);
        const resetAt = String(err.details?.reset_at ?? "");
        yield { type: "limit", limit, resetAt };
        return;
      }
      if (err instanceof MemaxError && err.code === "unavailable") {
        yield { type: "unavailable" };
        return;
      }
      throw err;
    }
    if (!signal?.aborted) {
      throw new MemaxError("The answer ended early.", "stream_error", 0);
    }
  };
}

/** One server source as the panel shows it. */
export function askSourceOf(
  s: V2.AskSource,
  n: number,
  viewer: Viewer | null,
): AskSource {
  return {
    n,
    statement: s.statement,
    state: s.state === "merged" ? "merged" : "kept",
    receipt: receiptLineOf(s, viewer),
    memory: s.ref,
    section: s.section,
  };
}

function receiptLineOf(s: V2.AskSource, viewer: Viewer | null): ReceiptLine {
  const rc = s.receipt;
  const line: ReceiptLine = {
    action:
      rc?.action === "merged"
        ? "merged"
        : rc?.action === "verified"
          ? "verified"
          : "kept",
    at: rc?.occurred_at ?? "",
    ref: s.ref,
  };
  const actor = actorOf(rc, viewer?.id);
  switch (actor?.kind) {
    case "agent":
      line.agent = actor.agent;
      break;
    case "dream":
    case "memax":
    case "repository":
      line.agent = actor.kind;
      break;
    case "person":
      // Receipts don't name other people yet: no stamp rather than a guess.
      if (actor.self && viewer) line.person = viewer.initials;
      break;
  }
  return line;
}
