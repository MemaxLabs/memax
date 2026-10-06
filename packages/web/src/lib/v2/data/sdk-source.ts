import type { Memax, V2 } from "memax-sdk";
import { createSdkActivity } from "./activity-sdk";
import { createSdkAsk } from "./ask-sdk";
import { agentsOverview, createSdkAgents } from "./agents-sdk";
import { createSdkBrief } from "./brief-sdk";
import { checkRememberOver } from "./remember-sdk";
import { createSdkMemories } from "./sdk-memories";
import { createSdkReview } from "./sdk-review";
import type { LedgerDataSource } from "./source";
import { syncLineOf, targetStatus } from "./targets";
import { createSdkTargets, targetsOrNull } from "./targets-sdk";
import { createSdkToday } from "./today-sdk";
import type { KeepResult, SpaceOverview, Viewer } from "./types";

/**
 * The SDK source: memax.v2 for signed-in people.
 *
 * What /v2 serves today: the spaces list, Review's queue (its `total`
 * is the rail's ochre count), memories, receipts, agents, the Brief and
 * its compile targets (which feed the status line), and Remember's
 * near-duplicate check and Ask. Everything else the frame shows is marked
 * PLACEHOLDER below and returns "not served" (null) or a neutral value
 * until its endpoint lands: Handoffs and Dream. The demo source has
 * all of them, for comparison with the boards.
 */

// `auth` for API keys (V1's auth.keys), until /v2 serves them.
type V2Client = Pick<Memax, "v2" | "auth">;

export function createSdkSource({
  client,
  viewer,
}: {
  client: V2Client;
  /** From the session (AuthProvider); null while it loads. */
  viewer: Viewer | null;
}): LedgerDataSource {
  const viewerId = () => viewer?.id;
  const agents = createSdkAgents({ client, viewer });
  return {
    ...createSdkActivity({ client, viewer }),
    ...agents,
    kind: "sdk",
    now: () => new Date(),
    viewer,
    review: createSdkReview(client, viewerId),
    memories: createSdkMemories(client, viewerId),
    brief: createSdkBrief(client, viewerId),
    targets: createSdkTargets(client),
    today: createSdkToday({ client, viewer, agents }),
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
      const [review, memories, receipts, agents, targets] = await Promise.all([
        client.v2.review.list(space.slug, { limit: 1, signal }),
        client.v2.memories.list(space.slug, { limit: 1, signal }),
        client.v2.receipts.list(space.slug, { limit: 1, signal }),
        agentsOverview(client, space.slug, signal),
        targetsOrNull(client, space.slug, signal),
      ]);
      const anyMemory = memories.items.length > 0;
      const line = targets ? syncLineOf(targets) : null;
      return {
        waiting: review.total,
        // Review lists oldest first, so the first item is the oldest.
        oldestWaitingAt: review.items[0]?.created_at ?? null,
        memories: { any: anyMemory, kept: null, forgotten: null },
        activity: { any: receipts.items.length > 0 },
        brief: anyMemory
          ? { title: null, facts: null, rewrittenAt: null }
          : null,
        // The space's agents, and the status line (plan §6.4): a drifted
        // file first, then "No agents yet", then what the targets say.
        agents: agents?.counts ?? null,
        status: (line?.kind === "drifted" ? line : null) ??
          agents?.status ??
          line ?? { kind: "not-compiling" },
        targets: targets
          ? {
              total: targets.filter((t) => t.syncState !== "off").length,
              inSync: targets.filter((t) => targetStatus(t).kind === "in_sync")
                .length,
            }
          : null,
        // PLACEHOLDER from here down: not served by /v2 yet.
        reviewFilters: null,
        lastReview: null,
        waitingBreakdown: null,
        openHandoffs: null,
        handoffs: null,
        dream: null,
        decisions: null,
      };
    },
    // A cited answer, streamed over memax.v2.ask (plan §5.11).
    ask: createSdkAsk(client, () => viewer),
    // The near-duplicate check (plan §5.8), over memax.v2.memories.
    checkRemember: (input) => checkRememberOver(client, input),
    // No X-Memax-Via: the server records a keep as a person's on the web
    // only when it can tell (spec, Via), not because a client says so.
    async remember({ space, statement, section, idempotencyKey, cites }) {
      // A kept answer cites the memories it came from (kind memory), so
      // its trust is theirs.
      const sources = cites?.map((ref) => ({ kind: "memory" as const, ref }));
      const result = await client.v2.memories.remember(
        space.slug,
        sources?.length
          ? { statement, section, sources }
          : { statement, section },
        { idempotencyKey },
      );
      // The server journals no undo for a person's own Remember (its
      // receipt answers 409 not_undoable), so the toast offers none.
      return toKeepResult(result, false);
    },
    async keepProposal({ space, ref, idempotencyKey }) {
      const result = await client.v2.memories.keep(
        ref,
        {},
        { space: space.slug, idempotencyKey },
      );
      return toKeepResult(result, true);
    },
    async undo({ receipt, idempotencyKey }) {
      // The receipt names its space; the server finds it within the
      // person's spaces (another space's receipt is 404).
      const result = await client.v2.receipts.undo(
        receipt,
        {},
        { idempotencyKey },
      );
      return { refs: result.memories.map((m) => m.ref) };
    },
  };
}

export function toKeepResult(
  result: V2.CommandResult,
  undoable: boolean,
): KeepResult {
  const kept = result.outcome === "applied";
  return {
    ref: result.memory.ref,
    outcome: kept ? "kept" : "proposed",
    // PLACEHOLDER: compile runs aren't served to the frame yet.
    recompiled: null,
    receipt: kept && undoable ? (result.receipts[0]?.id ?? null) : null,
  };
}
