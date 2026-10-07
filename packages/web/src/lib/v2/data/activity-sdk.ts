import type { Memax, V2 } from "memax-sdk";
import type {
  ActivityActor,
  ActivityData,
  ActivityEntry,
  SealView,
  ViaPart,
} from "./activity";
import type { Autonomy } from "./agents";
import { registryKey } from "./agents-sdk";
import type { Viewer } from "./types";

/**
 * Activity over memax.v2: the receipts (memax.v2.receipts), the reads
 * beside them (memax.v2.reads: R-, with the week's count) and how far
 * the receipts are sealed (memax.v2.receipts.checkpoints). A receipt
 * names the actor, the action and the object, never the memory's words,
 * so the SDK's sentences say "kept a memory" with the ID beside it; a
 * read says how many memories it returned. What /v2 doesn't serve yet:
 *
 * - PLACEHOLDER: the week's receipt totals (`totals: null`): the page
 *   counts what it has loaded, and says so. The week's reads are served.
 * - Another person's name: a receipt carries only their id.
 */

type Client = Pick<Memax, "v2">;

const PAGE_SIZE = 50;
/** Reads come far more often than receipts: a longer page. */
const READS_PAGE_SIZE = 100;
const AUTONOMY_LEVELS = new Set(["read", "propose", "write"]);

function actorOf(r: V2.Receipt, viewer: Viewer | null): ActivityActor {
  switch (r.actor_kind) {
    case "person":
      return viewer?.id && r.actor_id === viewer.id
        ? { kind: "you", initials: viewer.initials }
        : { kind: "person" };
    case "agent":
      return {
        kind: "agent",
        agent: registryKey(r.agent ?? "other"),
        connectionId: r.actor_id,
      };
    case "dream":
      return { kind: "dream" };
    case "memax":
      return { kind: "memax" };
    case "repository":
      return { kind: "repository" };
  }
}

/** One receipt as an Activity row. */
export function receiptToEntry(
  r: V2.Receipt,
  viewer: Viewer | null,
): ActivityEntry {
  const autonomy =
    r.source?.kind === "autonomy" && AUTONOMY_LEVELS.has(r.source.ref)
      ? (r.source.ref as Autonomy)
      : null;
  const via: ViaPart[] = [];
  if (r.actor_kind === "repository") via.push({ kind: "repository" });
  else via.push({ kind: "via", via: r.via });
  if (r.session_ref) via.push({ kind: "session", ref: r.session_ref });
  // The autonomy "source" is the level, said in the sentence instead; an
  // action of Dream's cites its edition ("edition 214").
  const edition =
    r.source?.kind === "dream" ? /^D-0*(\d+)$/.exec(r.source.ref) : null;
  if (edition) via.push({ kind: "edition", n: Number(edition[1]) });
  else if (r.source && !autonomy)
    via.push({ kind: "source", ref: r.source.ref });
  const entry: ActivityEntry = {
    id: r.id,
    at: r.occurred_at,
    actor: actorOf(r, viewer),
    action: r.action,
    object: {
      kind: r.object_kind,
      // An agent connection's receipts carry its slug: stamp it from the registry.
      ref: r.object_kind === "agent" ? registryKey(r.object_ref) : r.object_ref,
      id: r.object_id,
    },
    via,
    rawVia: r.via,
    session: r.session_ref ?? null,
    source: r.source ?? null,
    reason: r.reason ?? null,
  };
  if (autonomy) entry.detail = { kind: "autonomy", level: autonomy };
  return entry;
}

/**
 * One read as an Activity row. An agent's read names its connection; a
 * person's CLI reporting a session-start load names the agent it ran,
 * so that reads as the agent too. A compile read (a session-start digest
 * or load) is a read of the Brief: "read 12 memories and the Brief".
 */
export function readToEntry(r: V2.Read, viewer: Viewer | null): ActivityEntry {
  const actor: ActivityActor = r.agent
    ? {
        kind: "agent",
        agent: registryKey(r.agent),
        ...(r.connection_id ? { connectionId: r.connection_id } : {}),
      }
    : viewer?.id && r.person_id === viewer.id
      ? { kind: "you", initials: viewer.initials }
      : { kind: "person" };
  const via: ViaPart[] = [{ kind: "via", via: r.via }];
  if (r.session_ref) via.push({ kind: "session", ref: r.session_ref });
  return {
    id: r.id,
    at: r.read_at,
    actor,
    action: "read",
    object: { kind: "read", ref: r.ref, id: r.id },
    via,
    rawVia: r.via,
    session: r.session_ref ?? null,
    source: r.compile ? { kind: "compile", ref: r.compile } : null,
    reason: null,
    detail: { kind: "read", memories: r.memories, brief: Boolean(r.compile) },
  };
}

/** Tombstones read for one page of Activity (the newest first). */
const TOMBSTONES_PAGE = 200;

function isForgot(e: ActivityEntry): boolean {
  return e.action === "forgot" && e.object.kind === "memory";
}

/**
 * Fills each forgot row's "removed from N files and M agents" from its
 * tombstone (a Forget's own memory; the ones carried with it say less).
 */
export function withForgetCounts(
  entries: ActivityEntry[],
  tombstones: readonly V2.Tombstone[],
): void {
  const byRef = new Map(
    tombstones.filter((t) => t.kind === "memory").map((t) => [t.ref, t]),
  );
  for (const e of entries) {
    if (!isForgot(e)) continue;
    const t = byRef.get(e.object.ref);
    // Nothing held it outside Memax: the plain sentence says enough.
    if (t && !t.carried && t.gone.files + t.agents > 0) {
      e.detail = { kind: "forgot", files: t.gone.files, agents: t.agents };
    }
  }
}

/** The seal from a page of checkpoints, newest first: how far, signed or not, and the last check. */
export function sealOf(page: V2.CheckpointPage): SealView {
  const { seal } = page;
  const newest = page.items[0];
  return {
    sealed: seal.sealed_receipts,
    sealedAt: seal.sealed_at ?? null,
    unsealed: seal.unsealed,
    signed: newest ? newest.signed : null,
    verified: seal.verified_at
      ? { at: seal.verified_at, problems: seal.verify_problems ?? 0 }
      : null,
  };
}

export function createSdkActivity({
  client,
  viewer,
}: {
  client: Client;
  viewer: Viewer | null;
}): ActivityData {
  return {
    async activity({ space, cursor, signal }) {
      const page = await client.v2.receipts.list(space.slug, {
        cursor,
        limit: PAGE_SIZE,
        signal,
      });
      const entries = page.items.map((r) => receiptToEntry(r, viewer));
      // "Removed from 4 files and 5 agents": a forgot receipt holds no
      // counts, its tombstone does. Read them only when the page has one.
      if (entries.some((e) => isForgot(e))) {
        const tombstones = await client.v2.memories
          .tombstones(space.slug, { limit: TOMBSTONES_PAGE, signal })
          .catch(() => null);
        if (tombstones) withForgetCounts(entries, tombstones.tombstones);
      }
      return {
        entries,
        nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
        // PLACEHOLDER: /v2 serves no weekly receipt totals.
        totals: null,
      };
    },
    async reads({ space, cursor, signal }) {
      const page = await client.v2.reads.list(space.slug, {
        cursor,
        limit: READS_PAGE_SIZE,
        signal,
      });
      return {
        entries: page.items.map((r) => readToEntry(r, viewer)),
        nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
        week: page.reads_7d,
      };
    },
    async seal({ space, signal }) {
      // The newest checkpoint says whether they're signed; `seal` says the rest.
      return sealOf(
        await client.v2.receipts.checkpoints(space.slug, { limit: 1, signal }),
      );
    },
  };
}
