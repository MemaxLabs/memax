import type { Memax, V2 } from "memax-sdk";
import type {
  ConditionLine,
  LineageEntry,
  MemoryListItem,
  MemoryRecord,
  SourceLine,
} from "./memories";
import type { Actor, DisplayState, RailReceipt, RecordAction } from "./records";
import type { ReviewItem } from "./review";

/**
 * Mapping /v2 records (V2.Memory, V2.Receipt, V2.Source) onto what the
 * screens read. Pure, so the mapping rules are unit-tested; the one
 * effect, joining receipts to memories, takes the client.
 *
 * /v2 lists memories without their receipts, so the proposer and the
 * rail's "verb · time" come from a join: one page of the space's
 * receipts, then one memory's history for any it didn't cover.
 */

export type V2Client = Pick<Memax, "v2">;

/** The Ledger registry's key for a /v2 agent kind. */
export function agentKey(kind: string | undefined): string | undefined {
  if (!kind) return undefined;
  return kind === "gemini-cli" ? "gemini" : kind;
}

export function actorOf(
  receipt: V2.Receipt | undefined,
  viewerId: string | undefined,
): Actor | null {
  if (!receipt) return null;
  switch (receipt.actor_kind) {
    case "agent": {
      const agent = agentKey(receipt.agent);
      return agent ? { kind: "agent", agent } : null;
    }
    case "dream":
      return { kind: "dream" };
    case "memax":
      return { kind: "memax" };
    case "repository":
      return { kind: "repository" };
    case "person":
      // A Keep confirmed inside an agent is still the person's ("kept
      // by you via CC"): the receipt names the agent as the channel.
      return {
        kind: "person",
        self: Boolean(viewerId) && receipt.actor_id === viewerId,
      };
  }
}

const RECORD_ACTIONS = new Set<string>([
  "proposed",
  "kept",
  "edited",
  "rejected",
  "merged",
  "flagged",
  "resolved",
  "verified",
  "faded",
  "restored",
  "forgot",
  "moved",
  "compiled",
  "handed_off",
  "answered",
  "undid",
]);

export function isRecordAction(action: string): action is RecordAction {
  return RECORD_ACTIONS.has(action);
}

export function railOf(
  receipt: V2.Receipt | undefined,
  viewerId: string | undefined,
): RailReceipt | null {
  if (!receipt || !isRecordAction(receipt.action)) return null;
  return {
    by: actorOf(receipt, viewerId),
    action: receipt.action,
    at: receipt.occurred_at,
  };
}

/** "3e1a" from "3e1a" or "session 3e1a". */
export function sessionOf(receipt: V2.Receipt | undefined): string | null {
  const ref = receipt?.session_ref?.trim();
  if (!ref) return null;
  return ref.replace(/^session\s+/i, "");
}

/** The kept memory a downgraded edit supersedes: its receipt points at it. */
export function updatesOf(created: V2.Receipt | undefined): string | null {
  return created?.source?.kind === "memory" ? created.source.ref : null;
}

export function displayState(memory: V2.Memory): DisplayState {
  return memory.state === "rejected" ? "proposed" : memory.state;
}

export function reviewItemOf(
  memory: V2.Memory,
  receipts: ReadonlyMap<string, V2.Receipt>,
  viewerId: string | undefined,
): ReviewItem {
  const created = receipts.get(memory.created_receipt_id);
  const last = receipts.get(memory.last_receipt_id) ?? created;
  const updates = updatesOf(created);
  const state =
    memory.state === "conflict" || memory.state === "stale"
      ? memory.state
      : "proposed";
  const flagged = last?.action === "flagged";
  return {
    ref: memory.ref,
    version: memory.version,
    statement: memory.statement,
    section: memory.section,
    state,
    lifecycle: memory.lifecycle === "proposed" ? "proposed" : "kept",
    external:
      memory.trust === "external" ||
      Boolean(memory.sources?.some((s) => s.external)),
    by: actorOf(flagged ? last : created, viewerId),
    action: flagged ? "flagged" : updates ? "updated" : "proposed",
    at: (flagged ? last : created)?.occurred_at ?? memory.created_at,
    session: sessionOf(created),
    updates,
    // PLACEHOLDER: /v2 doesn't serve memory links, so a conflict has
    // nothing to compare against until the judge links it.
    conflictsWith: null,
    intoSpace: null,
  };
}

/** The rail's second line after the ID: a source, or the session. */
function railSource(receipt: V2.Receipt | undefined): string | null {
  if (receipt?.source && receipt.source.kind !== "memory") {
    return receipt.source.ref;
  }
  const session = sessionOf(receipt);
  return session ? `session ${session}` : null;
}

export function listItemOf(
  memory: V2.Memory,
  receipts: ReadonlyMap<string, V2.Receipt>,
  viewerId: string | undefined,
): MemoryListItem {
  const last = receipts.get(memory.last_receipt_id);
  const created = receipts.get(memory.created_receipt_id);
  const state = displayState(memory);
  return {
    ref: memory.ref,
    statement: memory.statement,
    section: memory.section,
    state,
    receipt: railOf(last, viewerId) ?? railOf(created, viewerId),
    source: railSource(created),
    note:
      state === "stale" && last?.action === "flagged"
        ? {
            kind: "stale",
            changedAt: last.occurred_at,
            source: last.source?.ref ?? null,
          }
        : null,
    forgotten:
      state === "forgotten"
        ? {
            at: last?.occurred_at ?? memory.updated_at,
            by: null,
            detail: null,
          }
        : null,
  };
}

const SOURCE_URL = /^https?:\/\//i;

export function sourceLineOf(source: V2.Source): SourceLine {
  return {
    key: source.id,
    kind: source.kind,
    label: source.ref,
    provider: null,
    url: source.uri && SOURCE_URL.test(source.uri) ? source.uri : null,
  };
}

/**
 * "Stays true while" conditions. The spec leaves their shape open, so
 * this reads the forms a writer is likely to send: a sentence, or an
 * object with a `text` and optionally a `code` (a dependency, a path)
 * set in mono before it.
 */
export function conditionsOf(raw: unknown[]): ConditionLine[] {
  return raw.flatMap((item, i): ConditionLine[] => {
    if (typeof item === "string" && item.trim()) {
      return [{ key: String(i), code: null, text: item.trim() }];
    }
    if (item && typeof item === "object") {
      const o = item as Record<string, unknown>;
      const code =
        typeof o.code === "string"
          ? o.code
          : typeof o.subject === "string"
            ? o.subject
            : null;
      const text =
        typeof o.text === "string"
          ? o.text
          : typeof o.rest === "string"
            ? o.rest
            : typeof o.description === "string"
              ? o.description
              : null;
      if (text || code) {
        return [{ key: String(i), code, text: text ?? "" }];
      }
    }
    return [];
  });
}

export function lineageOf(
  receipts: readonly V2.Receipt[],
  viewerId: string | undefined,
): LineageEntry[] {
  return [...receipts]
    .sort((a, b) => a.seq - b.seq)
    .flatMap((r): LineageEntry[] =>
      isRecordAction(r.action)
        ? [
            {
              key: r.id,
              action: r.action,
              by: actorOf(r, viewerId),
              at: r.occurred_at,
              detail: r.reason ?? null,
              count: null,
              to: null,
            },
          ]
        : [],
    );
}

export function recordOf(
  detail: V2.MemoryDetail,
  viewerId: string | undefined,
): MemoryRecord {
  const { memory } = detail;
  const receipts = detail.receipts.items;
  const newest = [...receipts].sort((a, b) => b.seq - a.seq);
  const keptReceipt = newest.find((r) => r.action === "kept");
  const verified = newest.find((r) => r.action === "verified");
  const state = displayState(memory);
  const lifecycle =
    memory.lifecycle === "rejected" ? "proposed" : memory.lifecycle;
  return {
    ref: memory.ref,
    version: memory.version,
    statement: memory.statement,
    section: memory.section,
    state,
    lifecycle,
    kept:
      lifecycle === "kept" && keptReceipt
        ? { by: actorOf(keptReceipt, viewerId), at: keptReceipt.occurred_at }
        : null,
    latest: railOf(newest[0], viewerId),
    // PLACEHOLDER: reads, reach, merged notes and compiled files aren't
    // served by /v2 yet.
    reads: null,
    reach: null,
    merged: null,
    reaches: null,
    lineage: lineageOf(receipts, viewerId),
    sources: (memory.sources ?? []).map(sourceLineOf),
    conditions: conditionsOf(memory.conditions ?? []),
    checked: verified
      ? { at: verified.occurred_at, by: actorOf(verified, viewerId) }
      : null,
    forgotten:
      state === "forgotten"
        ? {
            at: newest[0]?.occurred_at ?? memory.updated_at,
            by: null,
            detail: null,
          }
        : null,
  };
}

const JOIN_PAGE = 200;
const JOIN_CONCURRENCY = 4;

/**
 * The receipts the memories' rails need (each one's first and latest),
 * by receipt id: one page of the space's log, then each uncovered
 * memory's own history.
 */
export async function receiptsFor(
  client: V2Client,
  space: string,
  memories: readonly V2.Memory[],
  signal?: AbortSignal,
): Promise<Map<string, V2.Receipt>> {
  const byId = new Map<string, V2.Receipt>();
  if (memories.length === 0) return byId;
  const page = await client.v2.receipts.list(space, {
    limit: JOIN_PAGE,
    signal,
  });
  for (const r of page.items) byId.set(r.id, r);
  const missing = memories.filter(
    (m) => !byId.has(m.created_receipt_id) || !byId.has(m.last_receipt_id),
  );
  for (let i = 0; i < missing.length; i += JOIN_CONCURRENCY) {
    const batch = missing.slice(i, i + JOIN_CONCURRENCY);
    const histories = await Promise.all(
      batch.map((m) =>
        client.v2.receipts.list(space, { memory: m.id, limit: 50, signal }),
      ),
    );
    for (const history of histories) {
      for (const r of history.items) byId.set(r.id, r);
    }
  }
  return byId;
}
