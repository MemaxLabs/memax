import type { V2 } from "memax-sdk";
import type { AgentConnectionView, AgentsData } from "./agents";
import { askingAgents, type GatesSource } from "./gates";
import type { MemoryNote } from "./memories";
import { receiptsFor, reviewItemOf, type V2Client } from "./sdk-records";
import { startOfDay, type AgentToday, type TodaySource } from "./today";
import type { Viewer } from "./types";

/**
 * Today through memax.v2: Review's queue (its counts and first items),
 * the decision gates waiting on an answer (the questions, and who asked
 * them), the space's agents and what each wrote today, from today's
 * receipts, and read today, from the space's reads (R-). What /v2
 * doesn't serve is said so: Dream editions (unavailable), the Dream
 * schedule and handoffs (PLACEHOLDER).
 */

const QUEUE = 200;
const RECEIPTS = 200;
/** Today's reads are counted from the reads list, up to this many pages of 200. */
const READS = 200;
const READ_PAGES = 5;

/**
 * Each connection's reads since the start of the viewer's day, counted
 * from the space's reads (newest first) a page at a time. Null when the
 * day holds more reads than the pages Today reads: not counted rather
 * than a floor shown as the count.
 */
export async function readsToday(
  client: V2Client,
  slug: string,
  dayStart: Date,
  signal?: AbortSignal,
): Promise<Map<string, number> | null> {
  const counts = new Map<string, number>();
  let cursor: string | undefined;
  for (let page = 0; page < READ_PAGES; page += 1) {
    const { items, has_more, next_cursor } = await client.v2.reads.list(slug, {
      cursor,
      limit: READS,
      signal,
    });
    for (const read of items) {
      if (Date.parse(read.read_at) < dayStart.getTime()) return counts;
      if (read.connection_id) {
        counts.set(
          read.connection_id,
          (counts.get(read.connection_id) ?? 0) + 1,
        );
      }
    }
    if (!has_more || !next_cursor) return counts;
    cursor = next_cursor;
  }
  return null;
}

/**
 * Each connected agent's writes since the start of the viewer's day,
 * from the space's receipts, and its reads (readsToday); null reads when
 * they weren't counted.
 */
export function agentsTodayOf(
  connections: readonly AgentConnectionView[],
  receipts: readonly V2.Receipt[],
  dayStart: Date,
  reads: ReadonlyMap<string, number> | null = null,
): AgentToday[] {
  const today = receipts.filter(
    (r) =>
      r.actor_kind === "agent" &&
      r.object_kind === "memory" &&
      new Date(r.occurred_at).getTime() >= dayStart.getTime(),
  );
  return connections
    .filter((c) => c.state !== "disconnected")
    .map((c) => {
      const mine = today.filter((r) => r.actor_id === c.id);
      return {
        id: c.id,
        agent: c.agent,
        name: c.name,
        reads: reads ? (reads.get(c.id) ?? 0) : null,
        kept: mine.filter((r) => r.action === "kept").length,
        proposed: mine.filter((r) => r.action === "proposed").length,
        lastSeenAt: c.lastSeenAt,
      };
    })
    .sort(
      (a, b) =>
        Date.parse(b.lastSeenAt ?? "0") - Date.parse(a.lastSeenAt ?? "0"),
    );
}

export function createSdkToday({
  client,
  viewer,
  agents,
  gates,
}: {
  client: V2Client;
  viewer: Viewer | null;
  agents: Pick<AgentsData, "spaceAgents">;
  gates: Pick<GatesSource, "waiting">;
}): TodaySource {
  return {
    async get({ space, signal }) {
      const timeZone =
        viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
      const dayStart = startOfDay(new Date(), timeZone);
      const [queue, asked, connections, log] = await Promise.all([
        client.v2.review.list(space.slug, { limit: QUEUE, signal }),
        gates.waiting({ space, signal }),
        agents.spaceAgents(space, signal).catch(() => null),
        client.v2.receipts
          .list(space.slug, { limit: RECEIPTS, signal })
          .catch(() => null),
      ]);
      // Not counted rather than failing Today.
      const reads = connections
        ? readsToday(client, space.slug, dayStart, signal).catch(() => null)
        : null;
      const receipts = await receiptsFor(
        client,
        space.slug,
        queue.items,
        signal,
      );
      const items = queue.items.map((m) =>
        reviewItemOf(m, receipts, viewer?.id),
      );
      const notes: Record<string, MemoryNote> = {};
      for (const m of queue.items) {
        const last = receipts.get(m.last_receipt_id);
        if (m.state === "stale" && last?.action === "flagged") {
          notes[m.ref] = {
            kind: "stale",
            changedAt: last.occurred_at,
            source: last.source?.ref ?? null,
          };
        }
      }
      // Review lists proposals, then conflicts, then stale facts: what
      // didn't fit the page is at the stale end.
      const unseen = Math.max(0, queue.total - items.length);
      return {
        waiting: {
          items,
          gates: asked,
          total: queue.total + asked.length,
          proposals: items.filter((i) => i.lifecycle === "proposed").length,
          stale: items.filter((i) => i.state === "stale").length + unseen,
          questions: askingAgents(asked),
          notes,
        },
        // PLACEHOLDER: Dream editions aren't built yet (plan §5.10).
        dream: { kind: "unavailable" },
        dreamAt: null,
        // PLACEHOLDER: handoffs arrive in Phase 4.
        inFlight: undefined,
        agents: connections
          ? {
              rows: agentsTodayOf(
                connections,
                log?.items ?? [],
                dayStart,
                await reads,
              ),
              connected: connections.filter((c) => c.state !== "disconnected")
                .length,
            }
          : null,
      };
    },
  };
}
