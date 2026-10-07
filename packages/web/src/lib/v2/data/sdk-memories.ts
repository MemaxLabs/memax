import { MemaxError, type V2 } from "memax-sdk";
import type {
  MemoriesSource,
  MemoryFilter,
  MemoryRecord,
  MergedNote,
} from "./memories";
import { previewOf, tombstoneOf } from "./sdk-forget";
import { foldUndo, recordOf, undoneIn } from "./sdk-record";
import { actorOf, listItemOf, receiptsFor, type V2Client } from "./sdk-records";
import { reachesTarget } from "./targets";
import { targetsOrNull } from "./targets-sdk";

/**
 * Memories through memax.v2: the list (GET /v2/spaces/{space}/memories,
 * newest first, cursor-paged), one memory (GET /v2/memories/{ref}), edit
 * with If-Match, and Forget: its preview, the Forget itself, Keep it
 * instead of an agent's request, and the tombstone. A memory's page has its reads and reach: the
 * agents from its reads, the files from the compile targets. Section
 * counts and totals aren't served yet (PLACEHOLDER: null), and search
 * is the page's own (it filters what's loaded) until /v2 has one.
 */

const PAGE = 50;
/** Folds read for a memory's page; the rest are counted. */
const FOLDS_SHOWN = 10;

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

  /**
   * The proposals the judge folded into a memory (its `merged_into`
   * links in), each with its words and, inside the 14 days, the fold's
   * Undo: the link names the receipt that made it. Null when there are
   * none (Dream's notes aren't served yet).
   */
  async function foldsInto(
    slug: string,
    memory: V2.Memory,
    now: Date,
    signal?: AbortSignal,
  ): Promise<MemoryRecord["merged"]> {
    const links = (memory.links ?? [])
      .filter((l) => l.kind === "merged_into" && l.direction === "in")
      .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
    if (links.length === 0) return null;
    const shown = links.slice(0, FOLDS_SHOWN);
    const folded = await Promise.all(
      shown.map((l) => detail(slug, l.ref, signal)),
    );
    const notes = shown.flatMap((link, i): MergedNote[] => {
      const found = folded[i];
      if (!found) return [];
      const receipts = found.receipts.items;
      const created =
        receipts.find((r) => r.id === found.memory.created_receipt_id) ??
        [...receipts].sort((a, b) => a.seq - b.seq)[0];
      const still =
        found.memory.lifecycle === "merged" &&
        !undoneIn(receipts, link.receipt_id);
      return [
        {
          ref: found.memory.ref,
          statement: found.memory.statement,
          by: actorOf(created, viewerId()),
          at: link.created_at,
          undo: still ? foldUndo(link.receipt_id, link.created_at, now) : null,
        },
      ];
    });
    return { notes, total: links.length };
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
      const now = new Date();
      const record = recordOf(found, viewerId(), {
        merged: await foldsInto(space.slug, found.memory, now, signal),
        now,
      });
      if (!targets || record.lifecycle !== "kept") return record;
      // "Reaches": the files that hold its words, or read one that does.
      const reaches = targets.filter(
        (t) => t.syncState !== "off" && reachesTarget(record.ref, t, targets),
      );
      return {
        ...record,
        reaches,
        reach: { files: reaches.length, agents: record.reach?.agents ?? null },
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

    async previewForget({ space, ref, signal }) {
      return previewOf(
        await client.v2.memories.previewForget(ref, {
          space: space.slug,
          signal,
        }),
      );
    },

    async forget({ space, ref, version, carries, note, idempotencyKey }) {
      const body: V2.ForgetInput = { carries };
      if (note?.trim()) body.note = note.trim();
      const result = await client.v2.memories.forget(ref, body, {
        space: space.slug,
        ifMatch: version,
        idempotencyKey,
      });
      return { ref: result.memory.ref };
    },

    async declineForget({ space, ref, idempotencyKey }) {
      await client.v2.memories.declineForget(
        ref,
        {},
        { space: space.slug, idempotencyKey },
      );
    },

    async tombstone({ space, ref, signal }) {
      try {
        return tombstoneOf(
          await client.v2.memories.tombstone(ref, {
            space: space.slug,
            signal,
          }),
          viewerId(),
        );
      } catch (err) {
        if (isNotFound(err)) return null;
        throw err;
      }
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
      // Edit, then keep, whose words wait for the judge: the edit is saved
      // in place (the proposal's new version), the Keep is still to come.
      const judgePending = result.policy.code === "judge_pending";
      return {
        ref: result.memory.ref,
        outcome: judgePending
          ? "edited"
          : result.outcome !== "applied"
            ? "proposed"
            : keep && kept
              ? "kept"
              : "edited",
        version: result.memory.version,
        // PLACEHOLDER: compile runs aren't served yet.
        recompiled: null,
        // A person's edit (and edit-then-keep, kept or saved) is undoable
        // by its receipt; an edit sent to Review as a new proposal isn't.
        receipt:
          result.outcome === "applied" || judgePending
            ? (result.receipts[0]?.id ?? null)
            : null,
        ...(judgePending ? { judgePending } : {}),
      };
    },
  };
}
