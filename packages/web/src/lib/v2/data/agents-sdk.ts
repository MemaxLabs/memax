import {
  MemaxError,
  refusalOf,
  type ApiKeyListItem,
  type Memax,
  type V2,
} from "memax-sdk";
import {
  AgentCommandError,
  type AgentConnectionView,
  type AgentDetailView,
  type AgentRefusal,
  type AgentsData,
  type AgentWrite,
  type ApiKeyView,
} from "./agents";
import type { SyncLine, Viewer } from "./types";

/**
 * The agents domain over memax.v2.agents (and V1's auth keys for API
 * keys). Reads (R-, migration 038) come from the connection, each space,
 * the week and each session as /v2 counts them. What /v2 doesn't serve
 * yet is marked PLACEHOLDER and comes back as null or undefined, never
 * as demo data:
 *
 * - compile targets ("Compiles to"): built on another branch.
 * - the week's questions, handoffs received and held external writes.
 * - a space's rules (the autonomy new agents start at): Propose.
 */

type Client = Pick<Memax, "v2" | "auth">;

/** The Ledger registry's key for a /v2 agent kind; unknown kinds stamp from their name. */
export function registryKey(kind: string, name?: string): string {
  if (kind === "gemini-cli") return "gemini";
  if (kind === "other" || kind === "windsurf") return name || kind;
  return kind;
}

export function toConnection(
  c: V2.AgentConnection,
  viewer: Viewer | null,
): AgentConnectionView {
  const you = viewer?.id;
  return {
    id: c.id,
    agent: registryKey(c.agent, c.display_name),
    name: c.display_name,
    surface: c.surface,
    state: c.state,
    credential: { kind: c.credential.kind, active: c.credential.active },
    maxAutonomy: c.max_autonomy,
    mine: you !== undefined && c.person_id === you,
    clientId: c.client_id ?? null,
    spaces: c.spaces.map((s) => ({
      spaceId: s.space_id,
      slug: s.slug,
      name: s.name,
      kind: s.kind,
      autonomy: s.autonomy,
      reads7d: s.reads_7d,
      writes7d: s.writes_7d,
    })),
    reads7d: c.reads_7d,
    writes7d: c.writes_7d,
    lastSeenAt: c.last_seen_at ?? null,
    connectedAt: c.created_at,
    connectedBy:
      c.connected_by_kind === "memax"
        ? "memax"
        : c.connected_by && c.connected_by === you
          ? "you"
          : c.connected_by_kind === "person"
            ? "person"
            : null,
    // PLACEHOLDER: compile targets aren't served by /v2 yet.
    target: undefined,
  };
}

const MEMORY_STATES = new Set<AgentWrite["state"]>([
  "proposed",
  "kept",
  "merged",
  "stale",
  "faded",
  "conflict",
  "forgotten",
]);

/** A receipt of the agent's on a memory, with the memory's words when they loaded. */
export function toAgentWrite(
  receipt: V2.Receipt,
  memory: V2.Memory | null,
  agent: string,
): AgentWrite {
  const state =
    memory && MEMORY_STATES.has(memory.state as AgentWrite["state"])
      ? (memory.state as AgentWrite["state"])
      : "proposed";
  const write: AgentWrite = {
    receiptId: receipt.id,
    ref: receipt.object_ref,
    statement: memory && memory.state !== "forgotten" ? memory.statement : null,
    state,
    actor: { agent },
    action: receipt.action,
    at: receipt.occurred_at,
  };
  // A proposal a person has since kept: say where it came from.
  if (
    state === "kept" &&
    receipt.action === "proposed" &&
    receipt.session_ref
  ) {
    write.note = { kind: "proposed-in", agent, session: receipt.session_ref };
  }
  return write;
}

/** How many of the agent's latest writes Recent writes reads the words of. */
const RECENT_WRITES = 5;

/** Normalises a failed agent command into what the UI explains. */
export function toAgentCommandError(err: unknown): AgentCommandError {
  if (err instanceof AgentCommandError) return err;
  if (!(err instanceof MemaxError)) return new AgentCommandError("failed");
  const policy = refusalOf(err);
  if (policy) {
    const byCode: Partial<Record<string, AgentRefusal>> = {
      autonomy_needs_web: "needs_web",
      key_max_propose: "key_max_propose",
      not_your_agent: "not_your_agent",
      autonomy_not_allowed: "not_allowed",
      person_must_manage: "person_must_manage",
      not_member: "not_member",
    };
    return new AgentCommandError(
      byCode[policy.code ?? ""] ?? "failed",
      policy.message,
    );
  }
  const byError: Partial<Record<string, AgentRefusal>> = {
    surface_unverified: "surface_unverified",
    invalid_transition: "invalid_transition",
    not_found: "not_found",
  };
  return new AgentCommandError(byError[err.code] ?? "failed", err.message);
}

export function toApiKey(key: ApiKeyListItem): ApiKeyView {
  const canPropose =
    key.scopes?.includes("write") ||
    key.default_permissions?.includes("memory:write");
  const spaceIds =
    key.hub_scope_mode === "hub_allowlist" || key.hub_id
      ? (key.hub_ids?.length ? key.hub_ids : [key.hub_id]).filter(
          (id): id is string => Boolean(id),
        )
      : null;
  return {
    id: key.id,
    name: key.name,
    // V1 stores a key's first characters, not its last four.
    masked: `${key.prefix}…`,
    spaceIds,
    may: canPropose ? "propose" : "read",
    createdAt: key.created_at,
    lastUsedAt: key.last_used,
  };
}

/**
 * What the frame's overview knows from a space's agents: how many are
 * connected and active, and "No agents yet" for the status line when
 * there are none. "5 agents in sync" needs compile targets, so with
 * agents the status stays the caller's. Null when the list didn't load:
 * the overview never fails for it.
 */
export async function agentsOverview(
  client: Pick<Memax, "v2">,
  slug: string,
  signal?: AbortSignal,
): Promise<{
  counts: { connected: number; active: number };
  status: SyncLine | null;
} | null> {
  try {
    const { items } = await client.v2.agents.listInSpace(slug, { signal });
    const connected = items.filter((a) => a.state !== "disconnected");
    return {
      counts: {
        connected: connected.length,
        active: connected.filter((a) => a.state === "active").length,
      },
      status: connected.length === 0 ? { kind: "no-agents" } : null,
    };
  } catch {
    return null;
  }
}

export function createSdkAgents({
  client,
  viewer,
}: {
  client: Client;
  viewer: Viewer | null;
}): AgentsData {
  const command = async (
    run: () => Promise<V2.AgentCommandResult>,
  ): Promise<AgentConnectionView> => {
    try {
      return toConnection((await run()).agent, viewer);
    } catch (err) {
      throw toAgentCommandError(err);
    }
  };

  return {
    async spaceAgents(space, signal) {
      const { items } = await client.v2.agents.listInSpace(space.slug, {
        signal,
      });
      return items.map((c) => toConnection(c, viewer));
    },
    async myAgents(signal) {
      const { items } = await client.v2.agents.list({ signal });
      return items.map((c) => toConnection(c, viewer));
    },
    async agent(id, signal) {
      let detail: V2.AgentDetail;
      try {
        detail = await client.v2.agents.get(id, { signal });
      } catch (err) {
        if (err instanceof MemaxError && (err.isNotFound || err.status === 400))
          return null;
        throw err;
      }
      const connection = toConnection(detail.agent, viewer);
      // Receipts never hold the words: read the latest few memories for them.
      const receipts = detail.recent_writes
        .filter((r) => r.object_kind === "memory")
        .slice(0, RECENT_WRITES);
      const memories = await Promise.all(
        receipts.map((r) =>
          client.v2.memories
            .get(r.object_id, { signal })
            .then((m) => m.memory)
            .catch(() => null),
        ),
      );
      const week = detail.this_week;
      const view: AgentDetailView = {
        connection,
        week: {
          reads: week.reads,
          writes: week.writes,
          proposals: week.proposals,
          kept: week.kept,
          rejected: week.rejected,
          waiting: week.waiting,
          // PLACEHOLDER: not in AgentWeek.
          questions: null,
          handoffsReceived: null,
          heldExternal: null,
        },
        recentWrites: receipts.map((r, i) =>
          toAgentWrite(r, memories[i] ?? null, connection.agent),
        ),
        sessions: detail.sessions.map((s) => ({
          ref: s.session_ref,
          reads: s.reads,
          writes: s.writes,
          lastAt: s.last_at,
        })),
      };
      return view;
    },
    setAutonomy({ agent, space, autonomy, idempotencyKey }) {
      // No X-Memax-Via: the API records a change as the person's on the
      // web only when the web proxy signed it (WEB_SURFACE_SECRET).
      return command(() =>
        client.v2.agents.setAutonomy(
          agent,
          space.slug,
          { autonomy },
          { idempotencyKey },
        ),
      );
    },
    pauseAgent({ agent, idempotencyKey }) {
      return command(() =>
        client.v2.agents.pause(agent, {}, { idempotencyKey }),
      );
    },
    resumeAgent({ agent, idempotencyKey }) {
      return command(() =>
        client.v2.agents.resume(agent, {}, { idempotencyKey }),
      );
    },
    disconnectAgent({ agent, idempotencyKey }) {
      return command(() =>
        client.v2.agents.disconnect(agent, {}, { idempotencyKey }),
      );
    },
    // PLACEHOLDER: /v2 spaces don't carry their rules yet; Propose is the default.
    newAgentAutonomy: () => "propose",
    // PLACEHOLDER: compile targets are built on another branch.
    targetFor: () => undefined,
    async apiKeys() {
      const keys = await client.auth.listKeys();
      return keys.map(toApiKey);
    },
    async createApiKey({ name, may, space }) {
      const created = await client.auth.createKey({
        name,
        hubIds: [space.id],
        // "This key is me": a script of the person's, not a named agent.
        standalone: true,
        scopes: may === "read" ? ["read"] : ["read", "write"],
      });
      return {
        secret: created.key,
        key: toApiKey({
          ...created,
          scopes: may === "read" ? ["read"] : ["read", "write"],
          last_used: null,
        }),
      };
    },
    async revokeApiKey(id) {
      const result = await client.auth.revokeKey(id);
      // not_found is the idempotent success: it's already gone.
      if (result.skipped.some((s) => s.reason === "revoke_failed")) {
        throw new Error("revoke_failed");
      }
    },
  };
}
