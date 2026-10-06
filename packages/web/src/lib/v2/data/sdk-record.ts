import type { V2 } from "memax-sdk";
import type {
  ConditionLine,
  FoldUndo,
  LineageEntry,
  MemoryRecord,
  SourceLine,
} from "./memories";
import { actorOf, displayState, isRecordAction, railOf } from "./sdk-records";
import { FOLD_UNDO_WINDOW_MS } from "./undo";

/**
 * One /v2 memory (GET /v2/memories/{ref}) as a memory's page reads it:
 * its sources, its "stays true while" conditions and its lineage from
 * receipts. Pure, like sdk-records.ts, so the rules are unit-tested.
 */

const SOURCE_URL = /^https?:\/\//i;

export function sourceLineOf(source: V2.Source): SourceLine {
  return {
    key: source.id,
    kind: source.kind,
    label: source.ref,
    provider: null,
    url: source.uri && SOURCE_URL.test(source.uri) ? source.uri : null,
  };
}

/**
 * "Stays true while" conditions. The spec leaves their shape open, so
 * this reads the forms a writer is likely to send: a sentence, or an
 * object with a `text` and optionally a `code` (a dependency, a path)
 * set in mono before it.
 */
export function conditionsOf(raw: unknown[]): ConditionLine[] {
  return raw.flatMap((item, i): ConditionLine[] => {
    if (typeof item === "string" && item.trim()) {
      return [{ key: String(i), code: null, text: item.trim() }];
    }
    if (item && typeof item === "object") {
      const o = item as Record<string, unknown>;
      const code =
        typeof o.code === "string"
          ? o.code
          : typeof o.subject === "string"
            ? o.subject
            : null;
      const text =
        typeof o.text === "string"
          ? o.text
          : typeof o.rest === "string"
            ? o.rest
            : typeof o.description === "string"
              ? o.description
              : null;
      if (text || code) {
        return [{ key: String(i), code, text: text ?? "" }];
      }
    }
    return [];
  });
}

/** One of the judge's folds: Memax merged a proposal into a kept memory. */
export function isFold(r: V2.Receipt): boolean {
  return (
    r.action === "merged" &&
    r.actor_kind === "memax" &&
    r.source?.kind === "memory"
  );
}

/** Whether a receipt was undone, by the `undid` receipts that cite it. */
export function undoneIn(
  receipts: readonly V2.Receipt[],
  receipt: string,
): boolean {
  return receipts.some(
    (r) =>
      r.action === "undid" &&
      r.source?.kind === "receipt" &&
      r.source.ref === receipt,
  );
}

/** A fold's Undo while its 14 days last, else null. */
export function foldUndo(
  receipt: string,
  at: string,
  now: Date,
): FoldUndo | null {
  const until = Date.parse(at) + FOLD_UNDO_WINDOW_MS;
  if (!Number.isFinite(until) || until <= now.getTime()) return null;
  return { receipt, until: new Date(until).toISOString() };
}

export function lineageOf(
  receipts: readonly V2.Receipt[],
  viewerId: string | undefined,
  { merged = false, now = new Date() }: { merged?: boolean; now?: Date } = {},
): LineageEntry[] {
  const sorted = [...receipts].sort((a, b) => a.seq - b.seq);
  // Only the latest fold can be undone, while the memory is still folded.
  const lastFold = merged ? sorted.filter(isFold).at(-1) : undefined;
  return sorted.flatMap((r): LineageEntry[] => {
    if (!isRecordAction(r.action)) return [];
    const fold = isFold(r);
    return [
      {
        key: r.id,
        action: r.action,
        by: actorOf(r, viewerId),
        at: r.occurred_at,
        detail: r.reason ?? null,
        count: null,
        to: null,
        into: fold ? (r.source?.ref ?? null) : null,
        undo:
          fold && r === lastFold && !undoneIn(sorted, r.id)
            ? foldUndo(r.id, r.occurred_at, now)
            : null,
      },
    ];
  });
}

export function recordOf(
  detail: V2.MemoryDetail,
  viewerId: string | undefined,
  {
    merged = null,
    now = new Date(),
  }: { merged?: MemoryRecord["merged"]; now?: Date } = {},
): MemoryRecord {
  const { memory } = detail;
  const receipts = detail.receipts.items;
  const newest = [...receipts].sort((a, b) => b.seq - a.seq);
  const keptReceipt = newest.find((r) => r.action === "kept");
  const verified = newest.find((r) => r.action === "verified");
  const state = displayState(memory);
  const lifecycle =
    memory.lifecycle === "rejected" ? "proposed" : memory.lifecycle;
  return {
    ref: memory.ref,
    version: memory.version,
    statement: memory.statement,
    section: memory.section,
    state,
    lifecycle,
    kept:
      lifecycle === "kept" && keptReceipt
        ? { by: actorOf(keptReceipt, viewerId), at: keptReceipt.occurred_at }
        : null,
    latest: railOf(newest[0], viewerId),
    // PLACEHOLDER: reads, reach and compiled files aren't served by /v2
    // yet. What was folded into it comes from its links (the caller).
    reads: null,
    reach: null,
    merged,
    reaches: null,
    lineage: lineageOf(receipts, viewerId, {
      merged: memory.lifecycle === "merged",
      now,
    }),
    sources: (memory.sources ?? []).map(sourceLineOf),
    conditions: conditionsOf(memory.conditions ?? []),
    checked: verified
      ? { at: verified.occurred_at, by: actorOf(verified, viewerId) }
      : null,
    forgotten:
      state === "forgotten"
        ? {
            at: newest[0]?.occurred_at ?? memory.updated_at,
            by: null,
            detail: null,
          }
        : null,
  };
}
