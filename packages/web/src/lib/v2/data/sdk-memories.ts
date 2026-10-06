import { MemaxError, type V2 } from "memax-sdk";
import type { MemoriesSource, MemoryFilter } from "./memories";
import { recordOf } from "./sdk-record";
import { actorOf, listItemOf, receiptsFor, type V2Client } from "./sdk-records";
import { reachesTarget } from "./targets";
import { targetsOrNull } from "./targets-sdk";

/**
 * Memories through memax.v2: the list (GET /v2/spaces/{space}/memories,
 * newest first, cursor-paged), one memory (GET /v2/memories/{ref}) and
 * edit with If-Match. Section counts, totals, links, reads and compiled
 * files aren't served yet (PLACEHOLDER: null), and search is the page's
 * own (it filters what's loaded) until /v2 has one.
 */

const PAGE = 50;

const FILTER_STATES: Record<MemoryFilter, V2.State[] | undefined> = {
  all: undefined,
  // What Review holds: proposals, conflicts and stale facts.
  waiting: ["proposed", "conflict", "stale"],
  stale: ["stale"],
  merged: ["merged"],
  forgotten: ["forgotten"],
};

function isNotFound(err: unknown): boolean {
  return (
    err instanceof MemaxError &&
    (err.status === 404 || err.code === "not_found")
  );
}

export function createSdkMemories(
  client: V2Client,
  viewerId: () => string | undefined,
): MemoriesSource {
  async function detail(slug: string, ref: string, signal?: AbortSignal) {
    try {
      return await client.v2.memories.get(ref, { space: slug, signal });
    } catch (err) {
      // A memory in a space you're not part of is a 404 too (spec).
      if (isNotFound(err)) return null;
      throw err;
    }
  }

  return {
    async list({ space, filter, cursor, signal }) {
      const page = await client.v2.memories.list(space.slug, {
        state: FILTER_STATES[filter],
        cursor,
        limit: PAGE,
        signal,
      });
      const receipts = await receiptsFor(
        client,
        space.slug,
        page.items,
        signal,
      );
      return {
        items: page.items.map((m) => listItemOf(m, receipts, viewerId())),
        nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
        // PLACEHOLDER: /v2 doesn't count sections or totals yet.
        sectionCounts: null,
        total: null,
      };
    },

    async get({ space, ref, signal }) {
      const [found, targets] = await Promise.all([
        detail(space.slug, ref, signal),
        targetsOrNull(client, space.slug, signal),
      ]);
      if (!found) return null;
      const record = recordOf(found, viewerId());
      if (!targets || record.lifecycle !== "kept") return record;
      // "Reaches": the files that hold its words, or read one that does.
      return {
        ...record,
        reaches: targets.filter(
          (t) => t.syncState !== "off" && reachesTarget(record.ref, t, targets),
        ),
      };
    },

    async latest({ space, ref, signal }) {
      const found = await detail(space.slug, ref, signal);
      if (!found) return null;
      const newest = [...found.receipts.items].sort((a, b) => b.seq - a.seq);
      const change =
        newest.find((r) => r.action === "edited" || r.action === "kept") ??
        newest[0];
      return {
        version: found.memory.version,
        statement: found.memory.statement,
        by: actorOf(change, viewerId()),
        at: change?.occurred_at ?? found.memory.updated_at,
      };
    },

    async edit({
      space,
      ref,
      version,
      statement,
      reason,
      keep,
      idempotencyKey,
    }) {
      const body: V2.EditInput = { statement };
      if (reason) body.reason = reason;
      if (keep) body.keep = true;
      const result = await client.v2.memories.edit(ref, body, {
        space: space.slug,
        ifMatch: version,
        idempotencyKey,
      });
      const kept = result.receipts.some((r) => r.action === "kept");
      return {
        ref: result.memory.ref,
        outcome:
          result.outcome !== "applied"
            ? "proposed"
            : keep && kept
              ? "kept"
              : "edited",
        version: result.memory.version,
        // PLACEHOLDER: compile runs aren't served yet.
        recompiled: null,
      };
    },
  };
}
