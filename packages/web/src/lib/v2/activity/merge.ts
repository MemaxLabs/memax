import type { ActivityEntry } from "../data/activity";

/**
 * Activity's "All": the receipts and the reads (R-) as one log, newest
 * first. They're paged separately (reads aren't receipts, plan §5.3), so
 * one stream may reach further back than the other. The log stops where
 * the stream that reaches least far back stops, so a row never appears
 * out of place once older pages load: the rest of the loaded rows wait,
 * and "older" loads the stream that's holding the log back.
 */

/** One stream as loaded so far, newest first. */
export interface LoadedStream {
  entries: readonly ActivityEntry[];
  /** It has older pages. */
  hasMore: boolean;
}

export type StreamName = "receipts" | "reads";

export interface MergedLog {
  entries: ActivityEntry[];
  /** The streams to load for older rows; empty when both are at their end. */
  next: StreamName[];
}

const time = (e: ActivityEntry) => Date.parse(e.at);

/** How far back a stream is known to be complete: its oldest loaded row, or all of it at the end. */
function reach(stream: LoadedStream): number {
  if (!stream.hasMore) return -Infinity;
  const last = stream.entries[stream.entries.length - 1];
  return last ? time(last) : Infinity;
}

/**
 * Merges by time, keeping each stream's own order, and cuts the log at
 * the newest of the streams' reaches. `reads` is null when the reads
 * aren't there (they failed to load): the receipts alone, as they are.
 */
export function mergeLog(
  receipts: LoadedStream,
  reads: LoadedStream | null,
): MergedLog {
  if (!reads) {
    return {
      entries: [...receipts.entries],
      next: receipts.hasMore ? ["receipts"] : [],
    };
  }
  const cut = Math.max(reach(receipts), reach(reads));
  const r = receipts.entries;
  const d = reads.entries;
  const entries: ActivityEntry[] = [];
  let i = 0;
  let j = 0;
  while (i < r.length || j < d.length) {
    // A receipt goes first on a tie: what changed, then who read it.
    const takeReceipt =
      i < r.length && (j >= d.length || time(r[i]!) >= time(d[j]!));
    const row = takeReceipt ? r[i]! : d[j]!;
    if (time(row) < cut) break;
    entries.push(row);
    if (takeReceipt) i += 1;
    else j += 1;
  }
  const next: StreamName[] = [];
  if (receipts.hasMore && reach(receipts) === cut) next.push("receipts");
  if (reads.hasMore && reach(reads) === cut) next.push("reads");
  return { entries, next };
}
