import type { V2 } from "memax-sdk";
import type { AgentConnectionView, AgentsData } from "./agents";
import type { MemoryNote } from "./memories";
import { receiptsFor, reviewItemOf, type V2Client } from "./sdk-records";
import { startOfDay, type AgentToday, type TodaySource } from "./today";
import type { Viewer } from "./types";

/**
 * Today through memax.v2: Review's queue (its counts and first items),
 * the space's agents and what each wrote today, from today's receipts.
 * What /v2 doesn't serve is said so: Dream editions (unavailable), the
 * Dream schedule and handoffs (PLACEHOLDER), decision gates (no
 * questions yet) and reads (null).
 */

const QUEUE = 200;
const RECEIPTS = 200;

/** Each connected agent's writes since the start of the viewer's day, from the space's receipts. */
export function agentsTodayOf(
  connections: readonly AgentConnectionView[],
  receipts: readonly V2.Receipt[],
  dayStart: Date,
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
        // PLACEHOLDER: reads aren't recorded yet.
        reads: null,
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
}: {
  client: V2Client;
  viewer: Viewer | null;
  agents: Pick<AgentsData, "spaceAgents">;
}): TodaySource {
  return {
    async get({ space, signal }) {
      const timeZone =
        viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
      const dayStart = startOfDay(new Date(), timeZone);
      const [queue, connections, log] = await Promise.all([
        client.v2.review.list(space.slug, { limit: QUEUE, signal }),
        agents.spaceAgents(space, signal).catch(() => null),
        client.v2.receipts
          .list(space.slug, { limit: RECEIPTS, signal })
          .catch(() => null),
      ]);
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
          total: queue.total,
          proposals: items.filter((i) => i.lifecycle === "proposed").length,
          stale: items.filter((i) => i.state === "stale").length + unseen,
          // PLACEHOLDER: decision gates aren't served yet (epic 1.11).
          questions: [],
          notes,
        },
        // PLACEHOLDER: Dream editions aren't built yet (plan §5.10).
        dream: { kind: "unavailable" },
        dreamAt: null,
        // PLACEHOLDER: handoffs arrive in Phase 4.
        inFlight: undefined,
        agents: connections
          ? {
              rows: agentsTodayOf(connections, log?.items ?? [], dayStart),
              connected: connections.filter((c) => c.state !== "disconnected")
                .length,
            }
          : null,
      };
    },
  };
}
