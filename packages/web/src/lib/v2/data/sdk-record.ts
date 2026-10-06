import type { V2 } from "memax-sdk";
import type {
  ConditionLine,
  LineageEntry,
  MemoryRecord,
  SourceLine,
} from "./memories";
import { actorOf, displayState, isRecordAction, railOf } from "./sdk-records";

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

export function lineageOf(
  receipts: readonly V2.Receipt[],
  viewerId: string | undefined,
): LineageEntry[] {
  return [...receipts]
    .sort((a, b) => a.seq - b.seq)
    .flatMap((r): LineageEntry[] =>
      isRecordAction(r.action)
        ? [
            {
              key: r.id,
              action: r.action,
              by: actorOf(r, viewerId),
              at: r.occurred_at,
              detail: r.reason ?? null,
              count: null,
              to: null,
            },
          ]
        : [],
    );
}

export function recordOf(
  detail: V2.MemoryDetail,
  viewerId: string | undefined,
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
    // PLACEHOLDER: reads, reach, merged notes and compiled files aren't
    // served by /v2 yet.
    reads: null,
    reach: null,
    merged: null,
    reaches: null,
    lineage: lineageOf(receipts, viewerId),
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
