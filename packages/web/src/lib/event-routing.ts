"use client";

import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { hydrateMemoryFromEvent } from "@/lib/memory-event-hydration";
import { patchChecklistItemInCache } from "@/hooks/use-notifications";

/**
 * Declarative event-to-invalidation registry.
 *
 * Central source of truth that answers "what queries go stale when
 * SSE event X arrives?". Replaces ad-hoc `switch` branches that
 * accumulate in memax-event-bridge.tsx as new event types land.
 *
 * PR B (this one) scaffolds the registry as an empty object with the
 * adapter wired into memax-event-bridge.tsx AFTER the existing
 * imperative switch. Unknown event types fall through to the
 * registry; known ones stay in the switch until PR D migrates each
 * family one at a time.
 *
 * PR C fills in the initial routes for the new server event types
 * (`agent.changed`, `key.changed`, `dream.run.changed`).
 *
 * PR D migrates existing cases (`hub.members.changed`, etc.) from
 * the switch to the registry, deleting each switch case in the same
 * PR as the registry entry.
 *
 * The registry shape supports three write patterns per event:
 *
 *   - invalidate: broad "refetch this query family" — the common
 *     case when we can't patch the cache in place because the event
 *     payload doesn't carry enough detail.
 *   - patch: synchronous in-place cache edit — used for fine-grained
 *     ticks (e.g. a dream's `state` field) where the payload fully
 *     determines the new value and a refetch would be wasteful.
 *   - hydrate: async fetch-and-merge — used when the payload tells
 *     us WHAT changed but not its full shape, and we need a
 *     round-trip to reconcile (e.g. the existing
 *     `hydrateMemoryFromEvent` pattern).
 *
 * A route MAY specify any combination; all three fire in order
 * (patch → hydrate → invalidate) so an optimistic patch can paint
 * before a hydrate or refetch overwrites it.
 */

export interface StreamEnvelope {
  id?: string;
  type?: string;
  hub_id?: string;
  actor_id?: string;
  entity_id?: string;
  state?: string;
  topic_id?: string;
  created_at?: string;
  private_only?: boolean;
  kind?: string;
  source_kind?: string;
  source_id?: string;
  recipient_user_id?: string;
  hub_member_role?: string;
  resolution?: string;
  change?: string;
  seen_at?: string;
  // Plan 18 §4.5 — inline super-notif item snapshot carried by
  // notification.updated change=item_updated events. Lets clients
  // patch payload.items[item_id] without a follow-up fetch.
  item_id?: string;
  item_viewed_at?: string;
  item_completed_at?: string;
  item_progress?: { current: number; target: number };
}

export interface EventRoute {
  /**
   * Human-readable title shown in the memax debugger when this event
   * lands. Falls back to the raw event type when absent. Registered
   * routes should always specify one so the debugger UI stays
   * readable — otherwise a dev sees `hub.members.changed` instead
   * of the legacy "Members changed" label.
   */
  label?: string;
  /**
   * Broad "refetch this query family" — fires last after patch/hydrate.
   * Either a static list of query keys (most common) or a function
   * that computes keys from the event payload (used when invalidation
   * depends on payload fields like topic_id or entity_id).
   */
  invalidate?:
    | readonly QueryKey[]
    | ((payload: StreamEnvelope) => readonly QueryKey[]);
  /** Async fetch-and-merge for per-entity hydration paths. */
  hydrate?: (payload: StreamEnvelope, qc: QueryClient) => Promise<void> | void;
  /** Synchronous in-place cache edit — used for fine-grained ticks. */
  patch?: (payload: StreamEnvelope, qc: QueryClient) => void;
}

/**
 * Event type → route registry.
 *
 * PR C populates the first user-axis routes (agent.changed, key.changed).
 * PR D migrates existing hub/notification switch cases here one family
 * at a time. Entries match the event-type constants in
 * `packages/server/internal/events/types.go`.
 */
export const EVENT_ROUTES: Record<string, EventRoute> = {
  // `agent.changed` — user-axis event. State carries the shape:
  // "upserted" (new/touched via CreateAPIKey, EnsureConnectedAgent)
  // or "disconnected" (cascade from AgentsHandler.Disconnect).
  // Broad invalidate across the three surfaces that render the agent:
  // the Settings → Your Agents list, the API-keys list (for the
  // assign-agent affordance), and agent-configs (for MCP config
  // cards). Coarse-by-design per the final-plan Codex review; per-row
  // patching becomes useful once a heatmap or activity-feed surface
  // demands sub-second updates.
  "agent.changed": {
    label: "Agent changed",
    // ["notifications"] added 2026-05-20 — server's onboarding
    // materializer derives `connect_agent` completion from the
    // connected_agents table on read. Invalidating the
    // notifications cache here triggers a refetch that runs the
    // materializer with the new agent visible.
    invalidate: [
      ["connected-agents"],
      ["api-keys"],
      ["agent-configs"],
      ["notifications"],
    ],
  },
  // `key.changed` — user-axis event. State is "created" | "updated"
  // | "revoked". Only the api-keys list renders keys, so a narrow
  // invalidation suffices. Agent-name reassignments that surface a
  // NEW agent slug also emit a separate `agent.changed` with
  // state="upserted" from the handler, so this route deliberately
  // does NOT fan out to connected-agents.
  "key.changed": {
    label: "Key changed",
    invalidate: [["api-keys"]],
  },
  // `hub.list.changed` — user-axis event for hub lifecycle
  // transitions that can't be reached through the subscriber's
  // frozen HubRoles snapshot (create / new membership / deletion /
  // ownership flip). Paired with hub.members.changed for in-hub
  // management surfaces.
  //
  // Invalidates ["hubs"] (the hub list / picker) PLUS the specific
  // ["hub-detail", hubID] cache when an entity_id is present. The
  // detail invalidation matters especially on state="deleted":
  // hub.members.changed is NOT emitted for deletes (all members
  // are gone post-cascade), so without this line a tab parked on
  // the deleted hub's detail page would keep rendering stale data
  // until manual navigation. Invalidating across all states is
  // fine — detail is a small cache and the invalidation cost on
  // inactive queries is negligible.
  "hub.list.changed": {
    label: "Hub list changed",
    invalidate: (payload) => {
      // ["notifications"] added 2026-05-20 — server's onboarding
      // materializer derives `first_hub_invite` completion from
      // ListUserHubs filtered to hub_type=team. A newly created
      // or accepted team hub needs the notifications cache to
      // refetch so the materializer picks up the new membership.
      const keys: QueryKey[] = [["hubs"], ["notifications"]];
      if (payload.entity_id) {
        keys.push(["hub-detail", payload.entity_id]);
      }
      return keys;
    },
  },
  // `hub.members.changed` — hub-axis event from NewHubsHandler's
  // member mutations (add/update-role/remove/leave/accept-invite/
  // accept-ownership-transfer). Two caches need refreshing: the hub
  // list at ["hubs"] (role/seat counts) and the hub detail at
  // ["hub-detail", hubID] (the actual member list rendered by the
  // Settings → Members tab). The detail-cache invalidate was added
  // in PR H.3 — without it, the Members tab stayed stale until
  // refresh even though the SSE event arrived, because the legacy
  // PR D.1 lift-and-shift only knew about the hub list.
  "hub.members.changed": {
    label: "Members changed",
    invalidate: (payload) => {
      const keys: QueryKey[] = [["hubs"]];
      if (payload.hub_id) {
        keys.push(["hub-detail", payload.hub_id]);
      }
      return keys;
    },
  },
  // `hub.topics.changed` — hub-axis. Conditional entity_id drives
  // per-topic invalidation; static list covers the broad surfaces.
  // Lift-and-shift from the legacy switch, with ["recent-memories"]
  // added in PR H.6 — a topic rename changes the label the recent-
  // memories view uses to group memories under that topic.
  "hub.topics.changed": {
    label: "Topics changed",
    invalidate: (payload) => {
      const keys: QueryKey[] = [
        ["topics"],
        ["memory-lists"],
        ["recent-memories"],
        ["hubs"],
      ];
      if (payload.entity_id) {
        keys.push(["topics", payload.entity_id]);
        keys.push(["topics", payload.entity_id, "memories"]);
      }
      return keys;
    },
  },
  // `hub.dreams.changed` — coarse event; the finer-grained
  // dream.run.changed is deferred to a follow-up (worker wiring not
  // yet in place). A dream completion can surface fresh memories and
  // tick usage counters, so the PR H.6 pass added ["recent-memories"]
  // and ["usage"] — both were missing from the PR D.4 lift-and-shift.
  "hub.dreams.changed": {
    label: "Dreams changed",
    invalidate: [
      ["dreams"],
      ["topics"],
      ["memory-lists"],
      ["memories"],
      ["recent-memories"],
      ["hubs"],
      ["usage"],
    ],
  },
  // `notification.created` / `.resolved` / `.updated` — the inbox /
  // badge / bar all converge on a broad ["notifications"] invalidate.
  // A dream_run_completed receipt additionally invalidates the dream
  // report tab so settings > dreams shows the new counts.
  "notification.created": {
    label: "Notification created",
    invalidate: (payload) => {
      const keys: QueryKey[] = [["notifications"]];
      if (payload.source_kind === "dream_run") {
        keys.push(["dreams", "report"]);
      }
      return keys;
    },
  },
  "notification.resolved": {
    label: "Notification resolved",
    invalidate: [["notifications"]],
  },
  "notification.updated": {
    label: "Notification updated",
    // Plan 18 §4.5 — change=item_updated carries the snapshot inline.
    // Patch the cached notification payload in place BEFORE the broad
    // invalidate so the checklist row updates without a refetch
    // round-trip; the invalidate is a fail-safe.
    patch: (payload, qc) => {
      if (
        payload.change !== "item_updated" ||
        !payload.entity_id ||
        !payload.item_id
      ) {
        return;
      }
      // `patchChecklistItemInCache` walks every cached
      // ["notifications", ...] query and updates payload.items[item_id]
      // with the snapshot fields the server sent. Idempotent.
      patchChecklistItemInCache(
        payload.entity_id,
        payload.item_id,
        {
          viewed_at: payload.item_viewed_at,
          completed_at: payload.item_completed_at,
          progress: payload.item_progress,
        },
        qc,
      );
    },
    invalidate: [["notifications"]],
  },
  // `hub.memories.changed` — the final migrated route (PR D.5).
  // Combines a custom hydrate path (fetches the fresh memory and
  // patches recent-memories + memory-lists + topic-memories +
  // memory-detail caches) with broad invalidation for the cross-cut
  // surfaces that don't reward per-row patching. See
  // lib/memory-event-hydration.ts for why the hydration lives
  // there — it touches too many cache shapes to live inline.
  "hub.memories.changed": {
    label: "Memories changed",
    hydrate: (payload, qc) => hydrateMemoryFromEvent(payload, qc),
    invalidate: (payload) => {
      const keys: QueryKey[] = [
        ["memory-lists"],
        ["recent-memories"],
        ["topics"],
        ["hubs"],
        ["usage"],
        // 2026-05-20 — server's onboarding materializer derives
        // first_memory + five_memories completion from
        // CountMemoriesByOwnerExcludingSeeds. A new memory
        // (including ones created via MCP / chat / any path
        // outside the REST handler) needs the notifications cache
        // to refetch so the checklist counter + completion state
        // stay live.
        ["notifications"],
      ];
      // With entity_id, invalidate the entity-scoped prefix so
      // descendant keys (e.g. ["memories", id, "related"]) also
      // refresh. The hydrate hook above patches the detail cache
      // in place on the happy path, but hydrate SWALLOWS fetch
      // failures (deleted memory, transient network error) — we
      // need this invalidation to guarantee stale data gets
      // dropped even when hydrate can't complete. Without it a
      // deleted-memory event leaves the detail page + related
      // sub-queries stale indefinitely. Flagged by the PR D.5
      // Codex review. ["recent-memories"] at the broad level was
      // added in PR H.6 for the same fail-safe reason: hydrate
      // patches per-window recent caches on the happy path, but a
      // swallowed fetch failure would leave them stale.
      if (payload.entity_id) {
        keys.push(["memories", payload.entity_id]);
      } else {
        // No entity_id → server fired a hub-level coarse event;
        // fall back to the broader ["memories"] prefix.
        keys.push(["memories"]);
      }
      if (payload.topic_id) {
        keys.push(["topics", payload.topic_id]);
        keys.push(["topics", payload.topic_id, "memories"]);
      }
      return keys;
    },
  },
};

/**
 * Apply the matching route for an event type. Returns true if a
 * route was found and applied (caller should stop further
 * switch/fallback processing); false if no route is registered for
 * this event type.
 *
 * Order: patch → hydrate (fire-and-forget) → invalidate. This lets
 * a patch paint an optimistic state before a subsequent hydrate or
 * refetch replaces it with the truth from the server.
 */
export function applyEventRoute(
  event: string,
  payload: StreamEnvelope,
  qc: QueryClient,
): boolean {
  const route = EVENT_ROUTES[event];
  if (!route) return false;

  if (route.patch) {
    route.patch(payload, qc);
  }
  if (route.hydrate) {
    void route.hydrate(payload, qc);
  }
  if (route.invalidate) {
    const keys =
      typeof route.invalidate === "function"
        ? route.invalidate(payload)
        : route.invalidate;
    keys.forEach((queryKey) => {
      qc.invalidateQueries({ queryKey });
    });
  }

  return true;
}

/**
 * Returns the human-readable debugger label for a registered event
 * type, or undefined if no route is registered or the route has no
 * label. Callers (memax-event-bridge debugger log) fall back to the
 * raw event name.
 */
export function routeLabel(event: string): string | undefined {
  return EVENT_ROUTES[event]?.label;
}

/**
 * Test-only: register a route temporarily. Called by tests that need
 * to verify applyEventRoute behavior without importing a real route
 * (none exist at PR B scaffold time). Returns an unregister fn.
 */
export function __registerRouteForTests(
  event: string,
  route: EventRoute,
): () => void {
  EVENT_ROUTES[event] = route;
  return () => {
    delete EVENT_ROUTES[event];
  };
}
