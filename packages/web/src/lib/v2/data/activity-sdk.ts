import type { Memax, V2 } from "memax-sdk";
import type {
  ActivityActor,
  ActivityData,
  ActivityEntry,
  ViaPart,
} from "./activity";
import type { Autonomy } from "./agents";
import { registryKey } from "./agents-sdk";
import type { Viewer } from "./types";

/**
 * Activity over memax.v2.receipts. A receipt names the actor, the action
 * and the object, never the memory's words, so the SDK's sentences say
 * "kept a memory" with the ID beside it. What /v2 doesn't serve yet:
 *
 * - PLACEHOLDER: weekly totals (`totals: null`): the page counts what it
 *   has loaded, and says so.
 * - Reads (R-) aren't receipts (plan §5.3): they never appear here.
 * - Another person's name: a receipt carries only their id.
 */

type Client = Pick<Memax, "v2">;

const PAGE_SIZE = 50;
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
  // The autonomy "source" is the level, said in the sentence instead.
  if (r.source && !autonomy) via.push({ kind: "source", ref: r.source.ref });
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
      return {
        entries: page.items.map((r) => receiptToEntry(r, viewer)),
        nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
        // PLACEHOLDER: /v2 serves no weekly totals.
        totals: null,
      };
    },
  };
}
