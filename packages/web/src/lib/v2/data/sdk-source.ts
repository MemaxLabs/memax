import type { Memax } from "memax-sdk";
import type { LedgerDataSource } from "./source";
import type { AskEvent, KeepResult, SpaceOverview, Viewer } from "./types";

/**
 * The SDK source: memax.v2 for signed-in people.
 *
 * What /v2 serves today: the spaces list, Review's queue (its `total`
 * is the rail's ochre count), memories, and receipts. Everything else
 * the frame shows is marked PLACEHOLDER below and returns "not served"
 * (null) or a neutral value until its endpoint lands: Handoffs, agents,
 * compile targets and the status line, Dream, the near-duplicate check
 * and Ask. The demo source has all of them, for comparison with the
 * boards.
 */

type V2Client = Pick<Memax, "v2">;

export function createSdkSource({
  client,
  viewer,
}: {
  client: V2Client;
  /** From the session (AuthProvider); null while it loads. */
  viewer: Viewer | null;
}): LedgerDataSource {
  return {
    kind: "sdk",
    now: () => new Date(),
    viewer,
    async spaces(signal) {
      const { items } = await client.v2.spaces.list({ signal });
      return items.map((space) => ({
        id: space.id,
        slug: space.slug,
        name: space.name,
        kind: space.kind,
        role: space.role,
        repository: space.repository,
        // PLACEHOLDER: the spaces list carries no counts yet.
        kept: null,
        agents: null,
        people: null,
        waiting: null,
      }));
    },
    async overview(space, signal): Promise<SpaceOverview> {
      const [review, memories, receipts] = await Promise.all([
        client.v2.review.list(space.slug, { limit: 1, signal }),
        client.v2.memories.list(space.slug, { limit: 1, signal }),
        client.v2.receipts.list(space.slug, { limit: 1, signal }),
      ]);
      const anyMemory = memories.items.length > 0;
      return {
        waiting: review.total,
        // Review lists oldest first, so the first item is the oldest.
        oldestWaitingAt: review.items[0]?.created_at ?? null,
        memories: { any: anyMemory, kept: null, forgotten: null },
        activity: { any: receipts.items.length > 0 },
        brief: anyMemory
          ? { title: null, facts: null, rewrittenAt: null }
          : null,
        // PLACEHOLDER from here down: not served by /v2 yet.
        reviewFilters: null,
        lastReview: null,
        waitingBreakdown: null,
        openHandoffs: null,
        handoffs: null,
        status: { kind: "not-compiling" },
        dream: null,
        agents: null,
        targets: null,
        decisions: null,
      };
    },
    // PLACEHOLDER: no /v2 ask yet (plan §5.11 streams it over SSE).
    async *ask(): AsyncGenerator<AskEvent> {
      yield { type: "unavailable" };
    },
    // PLACEHOLDER: no near-duplicate endpoint yet (plan §5.8).
    async checkRemember() {
      return { duplicate: null, section: null, condition: null };
    },
    // No X-Memax-Via: the server records a keep as a person's on the web
    // only when it can tell (spec, Via), not because a client says so.
    async remember({ space, statement, section, idempotencyKey }) {
      const result = await client.v2.memories.remember(
        space.slug,
        { statement, section },
        { idempotencyKey },
      );
      return toKeepResult(result.memory.ref, result.outcome);
    },
    async keepProposal({ space, ref, idempotencyKey }) {
      const result = await client.v2.memories.keep(
        ref,
        {},
        { space: space.slug, idempotencyKey },
      );
      return toKeepResult(result.memory.ref, result.outcome);
    },
  };
}

function toKeepResult(ref: string, outcome: string): KeepResult {
  return {
    ref,
    outcome: outcome === "applied" ? "kept" : "proposed",
    // PLACEHOLDER: compile runs aren't served, and there is no inverse
    // command for a keep yet, so no Undo.
    recompiled: null,
  };
}
