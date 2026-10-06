import { MemaxError, type V2 } from "memax-sdk";
import type { DecisionResult } from "./records";
import type { ReviewCardData, ReviewSource, TouchedMemory } from "./review";
import { choiceFor, conflictOf } from "./sdk-conflict";
import {
  actorOf,
  conflictPartnerOf,
  receiptsFor,
  reviewItemOf,
  type V2Client,
} from "./sdk-records";

/**
 * Review through memax.v2: the queue (GET /v2/spaces/{space}/review),
 * one memory for the card with what the judge linked to it (`updates`,
 * `conflicts_with`), keep / reject with If-Match and the caller's
 * idempotency key, the conflict compare (GET …/conflict) and its answer
 * (POST …:resolve-conflict). What /v2 doesn't serve yet is marked
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

/** The compare has nothing to show: the memory has no conflict (409), or is gone. */
function noConflict(err: unknown): boolean {
  return (
    isNotFound(err) ||
    (err instanceof MemaxError && err.code === "invalid_transition")
  );
}

function decision(
  result: V2.CommandResult | V2.MemoriesCommandResult,
  done: "kept" | "rejected",
): DecisionResult {
  return {
    ref: result.memory.ref,
    outcome: result.outcome === "applied" ? done : "proposed",
    version: result.memory.version,
    // PLACEHOLDER: compile runs aren't served to Review yet.
    recompiled: null,
    // Undo addresses the command by any of its receipts.
    receipt:
      result.outcome === "applied" ? (result.receipts[0]?.id ?? null) : null,
  };
}

/** A linked memory as "This touches" lists it: its Keep, else its latest receipt. */
function touchedOf(
  detail: V2.MemoryDetail,
  viewerId: string | undefined,
): TouchedMemory {
  const newest = [...detail.receipts.items].sort((a, b) => b.seq - a.seq);
  const receipt = newest.find((r) => r.action === "kept") ?? newest[0];
  return {
    ref: detail.memory.ref,
    statement: detail.memory.statement,
    by: actorOf(receipt, viewerId),
    at: receipt?.occurred_at ?? detail.memory.updated_at,
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

  async function detailOf(slug: string, ref: string, signal?: AbortSignal) {
    try {
      return await client.v2.memories.get(ref, { space: slug, signal });
    } catch (err) {
      if (isNotFound(err)) return null;
      throw err;
    }
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
      const detail = await client.v2.memories.get(item.ref, {
        space: space.slug,
        signal,
      });
      const memory = detail.memory;
      const sources = memory.sources ?? [];
      const quoted = sources.find((s) => s.quote);
      const external = sources.find((s) => s.external);
      const linked = sources.find((s) => s.uri && /^https?:\/\//i.test(s.uri));

      // What the judge linked: the memory an update replaces, and the
      // decision in force a conflict contradicts.
      const updates = memory.updates?.ref ?? item.updates;
      const partner =
        memory.state === "conflict"
          ? (conflictPartnerOf(memory) ?? item.conflictsWith)
          : null;
      const refs = [...new Set([updates, partner].filter(Boolean))] as string[];
      const details = (
        await Promise.all(refs.map((ref) => detailOf(space.slug, ref, signal)))
      ).filter((d): d is V2.MemoryDetail => d !== null);
      const statementOf = (ref: string | null) =>
        ref
          ? (details.find((d) => d.memory.ref === ref)?.memory.statement ??
            null)
          : null;

      let memories: TouchedMemory[];
      let basis: "links" | "section";
      if (details.length > 0) {
        memories = details.map((d) => touchedOf(d, viewerId()));
        basis = "links";
      } else {
        // Nothing linked: kept memories in the same section.
        const page = await client.v2.memories.list(space.slug, {
          state: "kept",
          section: item.section,
          limit: TOUCHED + 1,
          signal,
        });
        const related = page.items
          .filter((m) => m.ref !== item.ref)
          .slice(0, TOUCHED);
        memories = await touched(space.slug, related, signal);
        basis = "section";
      }

      const beforeStatement =
        memory.updates?.statement ?? statementOf(updates ?? null);
      const conflictStatement = statementOf(partner);
      return {
        before:
          updates && beforeStatement
            ? { ref: updates, statement: beforeStatement }
            : null,
        conflict:
          partner && conflictStatement
            ? { ref: partner, statement: conflictStatement }
            : null,
        evidence: quoted?.quote
          ? { quote: quoted.quote, source: quoted.ref }
          : null,
        readFrom: external?.ref ?? null,
        sourceUrl: linked?.uri ?? null,
        touches: {
          memories,
          basis,
          // Keeping an update doesn't merge the memory it replaces: a fact
          // stays kept, and a decision in force is superseded (it stays,
          // and stops compiling). So the card doesn't promise a merge.
          replacesOnKeep: false,
          // PLACEHOLDER: compile targets aren't served to Review yet.
          targets: null,
        },
      };
    },

    async item({ space, ref, signal }) {
      const detail = await detailOf(space.slug, ref, signal);
      if (!detail) return null;
      const memory = detail.memory;
      const waiting =
        memory.lifecycle === "proposed" ||
        memory.state === "conflict" ||
        memory.state === "stale";
      if (!waiting) return null;
      const receipts = new Map(detail.receipts.items.map((r) => [r.id, r]));
      // The receipt page is newest first and may not reach the proposal.
      if (!receipts.has(memory.created_receipt_id)) {
        const joined = await receiptsFor(client, space.slug, [memory], signal);
        for (const [id, r] of joined) receipts.set(id, r);
      }
      return reviewItemOf(memory, receipts, viewerId());
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

    async conflict({ space, ref, signal }) {
      try {
        const found = await client.v2.memories.conflict(ref, {
          space: space.slug,
          signal,
        });
        return conflictOf(found, viewerId());
      } catch (err) {
        if (noConflict(err)) return null;
        throw err;
      }
    },

    async resolveConflict({
      space,
      ref,
      other,
      version,
      option,
      statement,
      otherStatement,
      idempotencyKey,
    }) {
      const body: V2.ResolveConflictInput = {
        choice: choiceFor(option),
        other,
      };
      if (option === "both") {
        if (statement) body.statement = statement;
        if (otherStatement) body.other_statement = otherStatement;
      }
      const result = await client.v2.memories.resolveConflict(ref, body, {
        space: space.slug,
        idempotencyKey,
        ifMatch: version,
      });
      // Every answer settles the flagged side: kept (as an open question
      // when left open), or rejected when the decision in force stays.
      return decision(result, option === "kept" ? "rejected" : "kept");
    },
  };
}
