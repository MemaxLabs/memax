import type { V2 } from "memax-sdk";
import type {
  ConditionLine,
  FoldUndo,
  LineageEntry,
  LineageFlag,
  MemoryRecord,
  SourceLine,
} from "./memories";
import { registryKey } from "./agents-sdk";
import { forgetRequestsOf } from "./sdk-forget";
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
 * "Stays true while": the memory's typed conditions in the record's
 * order, then its date to check again (`stale_after`), as the design
 * lists it with the others. A kind this build doesn't know is left out.
 */
export function conditionsOf(
  raw: readonly V2.Condition[],
  staleAfter?: string | null,
): ConditionLine[] {
  const lines = raw.flatMap((c, i): ConditionLine[] => {
    const key = c.id ?? `c${i + 1}`;
    switch (c.kind) {
      case "path_exists":
        return c.path || c.glob
          ? [
              {
                key,
                kind: c.kind,
                path: c.path ?? null,
                glob: c.glob ?? null,
              },
            ]
          : [];
      case "file_contains":
        return [
          {
            key,
            kind: c.kind,
            path: c.path,
            text: c.text,
            word: c.word ?? false,
          },
        ];
      case "dep_present":
        return [
          {
            key,
            kind: c.kind,
            manifest: c.manifest,
            name: c.name,
            range: c.range ?? null,
          },
        ];
      case "file_unchanged":
        return [{ key, kind: c.kind, path: c.path }];
      case "pr_state":
        return [
          {
            key,
            kind: c.kind,
            repo: c.repo,
            number: c.number,
            state: c.state,
          },
        ];
      default:
        return [];
    }
  });
  return staleAfter
    ? [...lines, { key: "date", kind: "date", at: staleAfter }]
    : lines;
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

/**
 * Which flag a `flagged` receipt set. The receipt names no flag, but who
 * wrote it and where it came from tell: the judge's and an import's
 * conflict flags cite the memory they contradict (`source` kind memory);
 * Dream cites its edition, and says which in its reason ("Contradicts
 * M-0174. …" for a conflict, "Its date to check again … has passed" for a
 * stale fact). A reason Forget took out leaves it unsaid.
 */
export function flagOf(r: V2.Receipt): LineageFlag | null {
  if (r.action !== "flagged") return null;
  if (r.source?.kind === "memory") {
    return { kind: "conflict", with: r.source.ref || null };
  }
  // The checker's and the date sweep's flags cite what they checked.
  if (["check", "commit", "pr", "date"].includes(r.source?.kind ?? "")) {
    return { kind: "stale" };
  }
  const reason = r.reason?.trim() ?? "";
  const contradicts = /^Contradicts (M-\d+)/.exec(reason);
  if (contradicts) return { kind: "conflict", with: contradicts[1] ?? null };
  if (/^Its date to check again\b/.test(reason)) return { kind: "stale" };
  return null;
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
        flag: flagOf(r),
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
    // Its reads and the agents they came from (absent when the counts
    // couldn't be read). The files it reaches come from the targets, and
    // what was folded into it from its links: both the caller's.
    reads: detail.reads?.reads ?? null,
    ...(detail.reads?.unobserved_target ? { readsUnobserved: true } : {}),
    reach: detail.reads ? { files: null, agents: detail.reads.agents } : null,
    merged,
    reaches: null,
    lineage: lineageOf(receipts, viewerId, {
      merged: memory.lifecycle === "merged",
      now,
    }),
    sources: (memory.sources ?? []).map(sourceLineOf),
    conditions: conditionsOf(memory.conditions ?? [], memory.stale_after),
    // A member's machine (or the GitHub App) checking its conditions; a
    // person's Verify is a lineage line, not a check of these.
    checked: detail.checked
      ? {
          at: detail.checked.at,
          branch: detail.checked.branch,
          agent: detail.checked.agent
            ? { kind: "agent", agent: registryKey(detail.checked.agent) }
            : null,
        }
      : null,
    forgotten:
      state === "forgotten"
        ? {
            at: newest[0]?.occurred_at ?? memory.updated_at,
            by: null,
            detail: null,
          }
        : null,
    forgetRequests:
      state === "forgotten" ? [] : forgetRequestsOf(detail.forget_requests),
    repos: memory.scope.repos ?? [],
  };
}
