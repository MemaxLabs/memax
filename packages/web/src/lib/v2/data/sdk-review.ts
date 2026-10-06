import { MemaxError, type V2 } from "memax-sdk";
import { CommandFailedError } from "./command-error";
import type { DecisionResult } from "./records";
import type { ReviewCardData, ReviewSource, TouchedMemory } from "./review";
import {
  actorOf,
  receiptsFor,
  reviewItemOf,
  type V2Client,
} from "./sdk-records";
import { targetsOrNull } from "./targets-sdk";

/**
 * Review through memax.v2: the queue (GET /v2/spaces/{space}/review),
 * one memory for the card, and keep / reject with If-Match and the
 * caller's idempotency key. What /v2 doesn't serve yet is marked
 * PLACEHOLDER and returns "not served", never demo data.
 */

const QUEUE_PAGE = 50;
const TOUCHED = 2;

function isNotFound(err: unknown): boolean {
  return (
    err instanceof MemaxError &&
    (err.status === 404 || err.code === "not_found")
  );
}

function decision(
  result: V2.CommandResult,
  done: "kept" | "rejected",
): DecisionResult {
  return {
    ref: result.memory.ref,
    outcome: result.outcome === "applied" ? done : "proposed",
    version: result.memory.version,
    // PLACEHOLDER: compile runs aren't served, and there is no inverse
    // command for a decision yet, so no Undo.
    recompiled: null,
  };
}

export function createSdkReview(
  client: V2Client,
  viewerId: () => string | undefined,
): ReviewSource {
  async function touched(
    slug: string,
    memories: V2.Memory[],
    signal?: AbortSignal,
  ): Promise<TouchedMemory[]> {
    const receipts = await receiptsFor(client, slug, memories, signal);
    return memories.map((m) => {
      const last = receipts.get(m.last_receipt_id);
      return {
        ref: m.ref,
        statement: m.statement,
        by: actorOf(last, viewerId()),
        at: last?.occurred_at ?? m.updated_at,
      };
    });
  }

  return {
    async queue({ space, cursor, signal }) {
      const page = await client.v2.review.list(space.slug, {
        cursor,
        limit: QUEUE_PAGE,
        signal,
      });
      const receipts = await receiptsFor(
        client,
        space.slug,
        page.items,
        signal,
      );
      return {
        items: page.items.map((m) => reviewItemOf(m, receipts, viewerId())),
        total: page.total,
        nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
      };
    },

    async card({ space, item, signal }): Promise<ReviewCardData> {
      const [detail, before, targets] = await Promise.all([
        client.v2.memories.get(item.ref, { space: space.slug, signal }),
        item.updates
          ? client.v2.memories
              .get(item.updates, { space: space.slug, signal })
              .catch((err: unknown) => {
                if (isNotFound(err)) return null;
                throw err;
              })
          : Promise.resolve(null),
        targetsOrNull(client, space.slug, signal),
      ]);
      const sources = detail.memory.sources ?? [];
      const quoted = sources.find((s) => s.quote);
      const external = sources.find((s) => s.external);
      const linked = sources.find((s) => s.uri && /^https?:\/\//i.test(s.uri));

      // "This touches": the memory an update replaces when there is one.
      // Otherwise kept memories in the same section, the only relation
      // /v2 serves until the judge relates memories (plan §5.8).
      let related: V2.Memory[];
      let basis: "links" | "section";
      if (before) {
        related = [before.memory];
        basis = "links";
      } else {
        const page = await client.v2.memories.list(space.slug, {
          state: "kept",
          section: item.section,
          limit: TOUCHED + 1,
          signal,
        });
        related = page.items
          .filter((m) => m.ref !== item.ref)
          .slice(0, TOUCHED);
        basis = "section";
      }

      return {
        before: before
          ? { ref: before.memory.ref, statement: before.memory.statement }
          : null,
        // PLACEHOLDER: no memory links in /v2, so no conflict partner.
        conflict: null,
        evidence: quoted?.quote
          ? { quote: quoted.quote, source: quoted.ref }
          : null,
        readFrom: external?.ref ?? null,
        sourceUrl: linked?.uri ?? null,
        touches: {
          memories: await touched(space.slug, related, signal),
          basis,
          // The server keeps an update without merging the memory it
          // supersedes (yet), so the card doesn't promise it.
          replacesOnKeep: false,
          // A Keep recompiles every file the space compiles to (not
          // ChatGPT's copy-out, nor a stopped target).
          targets: targets
            ? targets.filter(
                (t) => t.delivery !== "copy" && t.syncState !== "off",
              )
            : null,
        },
      };
    },

    // No X-Memax-Via: the server decides whether a keep is a person's
    // on the web (assurance human_web), not the client.
    async keep({ space, item, idempotencyKey }) {
      const result = await client.v2.memories.keep(
        item.ref,
        {},
        { space: space.slug, idempotencyKey, ifMatch: item.version },
      );
      return decision(result, "kept");
    },

    async reject({ space, item, reason, idempotencyKey }) {
      const result = await client.v2.memories.reject(
        item.ref,
        reason ? { reason } : {},
        { space: space.slug, idempotencyKey, ifMatch: item.version },
      );
      return decision(result, "rejected");
    },

    // PLACEHOLDER: the judge that links conflicts doesn't exist, so
    // there is never a conflict to compare, and no resolve command.
    async conflict() {
      return null;
    },
    async resolveConflict() {
      throw new CommandFailedError({ kind: "unavailable" });
    },
  };
}
