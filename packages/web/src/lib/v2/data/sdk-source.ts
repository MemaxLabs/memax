import type { Memax, V2 } from "memax-sdk";
import { createSdkAccount } from "./account-sdk";
import { createSdkActivity } from "./activity-sdk";
import { createSdkAsk } from "./ask-sdk";
import { agentsOverview, createSdkAgents } from "./agents-sdk";
import { createSdkBrief } from "./brief-sdk";
import type { WebSession } from "./gates";
import { createSdkGates } from "./gates-sdk";
import { createSdkDevices } from "./devices-sdk";
import { createSdkDream } from "./dream-sdk";
import { createSdkImports } from "./imports-sdk";
import { checkRememberOver } from "./remember-sdk";
import { createSdkMemories } from "./sdk-memories";
import { createSdkReview } from "./sdk-review";
import { createSdkSettings } from "./settings-sdk";
import { createSdkSwitch } from "./switch-sdk";
import { spaceSyncLine, targetCounts } from "./home-files";
import { createSdkMachines, machinesOrNull } from "./machines-sdk";
import type { LedgerDataSource } from "./source";
import { createSdkTargets, targetsOrNull } from "./targets-sdk";
import { createSdkToday } from "./today-sdk";
import { createSdkSearch } from "./search-sdk";
import type { KeepResult, SpaceOverview, SpaceSummary, Viewer } from "./types";

/**
 * The SDK source: memax.v2 for signed-in people.
 *
 * What /v2 serves today: the spaces list (with each one's counts),
 * making a space, search across spaces, Review's queue and the
 * decision gates waiting on an answer (together the rail's ochre count),
 * memories, receipts, agents, the Brief and its compile targets (which
 * feed the status line), Remember's near-duplicate check and Ask.
 * Dream's editions come from memax.v2.dream, and the person's
 * notification settings and Security page from memax.v2.settings.
 * Everything else the frame
 * shows is marked PLACEHOLDER below and returns "not served" (null) or a
 * neutral value until its endpoint lands: Handoffs. The demo source has
 * all of them, for comparison with the boards.
 */

// `auth` for API keys (V1's auth.keys), until /v2 serves them.
type V2Client = Pick<Memax, "v2" | "auth">;

export function createSdkSource({
  client,
  viewer,
  webSession = () => null,
}: {
  client: V2Client;
  /** From the session (AuthProvider); null while it loads. */
  viewer: Viewer | null;
  /** Whether the session was issued to the web app (web-session.ts); null when unknown. */
  webSession?: () => WebSession;
}): LedgerDataSource {
  const viewerId = () => viewer?.id;
  const agents = createSdkAgents({ client, viewer });
  const gates = createSdkGates({ client, viewerId, webSession });
  const dream = createSdkDream(client, viewerId);
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
    machines: createSdkMachines(client),
    today: createSdkToday({ client, viewer, agents, gates, dream }),
    gates,
    imports: createSdkImports(client),
    devices: createSdkDevices(client),
    dream,
    switch: createSdkSwitch(client),
    settings: createSdkSettings(client),
    account: createSdkAccount(client),
    async spaces(signal) {
      const { items } = await client.v2.spaces.list({ signal });
      return items.map(spaceSummaryOf);
    },
    async createSpace({ name, kind, repository, idempotencyKey }) {
      const space = await client.v2.spaces.create(
        repository ? { name, kind, repository } : { name, kind },
        { idempotencyKey },
      );
      // A new space has nothing in it yet; the list says the same.
      return { ...spaceSummaryOf(space), kept: 0, agents: 0, waiting: 0 };
    },
    search: createSdkSearch(client, viewerId),
    async overview(space, signal): Promise<SpaceOverview> {
      const [review, memories, receipts, agents, targets, asked, machines] =
        await Promise.all([
          client.v2.review.list(space.slug, { limit: 1, signal }),
          client.v2.memories.list(space.slug, { limit: 1, signal }),
          client.v2.receipts.list(space.slug, { limit: 1, signal }),
          agentsOverview(client, space.slug, signal),
          targetsOrNull(client, space.slug, signal),
          // Not counted rather than failing the frame.
          gates.waiting({ space, signal }).catch(() => null),
          // Only a personal space is written to machines.
          space.kind === "personal"
            ? machinesOrNull(client, space.slug, signal)
            : Promise.resolve(null),
        ]);
      const anyMemory = memories.items.length > 0;
      const now = new Date();
      const line = targets
        ? spaceSyncLine(space, targets, machines, now)
        : null;
      return {
        waiting: review.total,
        // Review lists them first, and the rail counts them (waitingOnYou).
        gatesWaiting: asked?.length ?? null,
        // Review lists oldest first, so the first item is the oldest.
        oldestWaitingAt: review.items[0]?.created_at ?? null,
        memories: { any: anyMemory, kept: null, forgotten: null },
        activity: { any: receipts.items.length > 0 },
        brief: anyMemory
          ? { title: null, facts: null, rewrittenAt: null }
          : null,
        // The space's agents, and the status line (plan §6.4): a drifted
        // file first, then "No agents yet", then what the targets say
        // (for Personal, how its machines stand).
        agents: agents?.counts ?? null,
        status: (line?.kind === "drifted" || line?.kind === "machine-drifted"
          ? line
          : null) ??
          agents?.status ??
          line ?? { kind: "not-compiling" },
        targets: targets ? targetCounts(targets, machines, now) : null,
        // PLACEHOLDER from here down: not served by /v2 yet.
        reviewFilters: null,
        lastReview: null,
        // /v2 doesn't break Review's total down by kind. Today words its
        // lede from its own read (TodayData.waiting), gates included.
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
      // A person's own Remember is undoable for 10 minutes, kept or sent to
      // Review: undoing it withdraws the memory, by its receipt (through
      // /api/proxy, like every command).
      return {
        ...toKeepResult(result, true),
        receipt: result.receipts[0]?.id ?? null,
      };
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

/**
 * A space as the switcher lists it. The list counts what's kept, waiting
 * and connected in each space on V2 (a space on V1 has no counts: its
 * record is V1's); how many people share one isn't served yet
 * (PLACEHOLDER: null).
 */
export function spaceSummaryOf(space: V2.Space): SpaceSummary {
  return {
    id: space.id,
    slug: space.slug,
    name: space.name,
    kind: space.kind,
    role: space.role,
    repository: space.repository,
    onV2: Boolean(space.v2_enabled_at),
    createdAt: space.created_at,
    kept: space.counts?.kept ?? null,
    agents: space.counts?.agents ?? null,
    people: null,
    waiting: space.counts?.waiting ?? null,
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
