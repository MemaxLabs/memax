import { MemaxError, type V2 } from "memax-sdk";
import {
  briefRefs,
  type BriefItem,
  type BriefRowReceipt,
  type BriefSource,
  type BriefStructure,
  type BriefVersionView,
} from "./brief";
import {
  buildBriefView,
  proseAuthorsOf,
  type BriefInputMemory,
} from "./brief-view";
import {
  actorOf,
  displayState,
  listItemOf,
  receiptsFor,
  type V2Client,
} from "./sdk-records";

/**
 * The Brief through memax.v2.briefs: the current version (404 is "no
 * Brief yet"), its versions, and revise with If-Match. A version holds
 * only display IDs and prose, so the page reads the memories too: the
 * kept ones (placed or not, as the compiler places them) and the
 * proposals waiting, with the receipts their margins show. Dream's
 * highlights and short source titles aren't served (null and none).
 */

const PAGE = 200;
/** At most this many memories are read for one Brief (5 pages). */
const MAX_PAGES = 5;
const VERSIONS = 50;
const SHOWN_STATES: V2.State[] = ["kept", "stale", "conflict", "proposed"];

function isNotFound(err: unknown): boolean {
  return (
    err instanceof MemaxError &&
    (err.status === 404 || err.code === "not_found")
  );
}

export function structureOf(brief: V2.Brief): BriefStructure {
  return {
    title: brief.title,
    summary: brief.summary ?? null,
    sections: brief.sections.map((section) => ({
      key: section.key,
      heading: section.heading,
      items: section.items.map(
        (item): BriefItem =>
          item.ref
            ? { ref: item.ref }
            : { text: item.text ?? "", cites: item.cites ?? [] },
      ),
    })),
  };
}

export function versionOf(
  brief: V2.Brief,
  viewerId: string | undefined,
): BriefVersionView {
  return {
    ref: brief.ref,
    version: brief.version,
    parent: brief.parent_version ?? null,
    current: brief.current,
    by: actorOf(brief.receipt, viewerId),
    at: brief.created_at,
    reason: brief.receipt?.reason ?? null,
    facts: brief.facts,
    structure: structureOf(brief),
  };
}

function scopeOf(memory: V2.Memory): string[] {
  const paths = (memory.scope as { paths?: unknown }).paths;
  return Array.isArray(paths)
    ? paths.filter((p): p is string => typeof p === "string")
    : [];
}

/** One memory as the Brief's builder reads it, with its margin's receipt. */
export function briefMemoryOf(
  memory: V2.Memory,
  receipts: ReadonlyMap<string, V2.Receipt>,
  viewerId: string | undefined,
): BriefInputMemory {
  const item = listItemOf(memory, receipts, viewerId);
  const receipt: BriefRowReceipt | null = item.receipt && {
    by: item.receipt.by,
    action: item.receipt.action,
    at: item.receipt.at,
  };
  const state = displayState(memory);
  return {
    ref: memory.ref,
    text: memory.statement,
    section: memory.section,
    state,
    lifecycle:
      memory.lifecycle === "kept"
        ? "kept"
        : memory.lifecycle === "proposed"
          ? "proposed"
          : "other",
    version: memory.version,
    receipt,
    source: item.source,
    scope: scopeOf(memory),
    changed: null,
  };
}

async function listMemories(
  client: V2Client,
  slug: string,
  signal?: AbortSignal,
): Promise<V2.Memory[]> {
  const all: V2.Memory[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_PAGES; page++) {
    const result = await client.v2.memories.list(slug, {
      state: SHOWN_STATES,
      limit: PAGE,
      cursor,
      signal,
    });
    all.push(...result.items);
    if (!result.has_more || !result.next_cursor) break;
    cursor = result.next_cursor;
  }
  return all;
}

/** The most memories whose receipts one Brief read joins (see marginsToRead). */
const MARGINS = 120;

/**
 * Which memories' receipts the margin reads. /v2 lists memories without
 * receipts, so each one outside the latest page of the log costs a
 * request: the ones the Brief places or cites and the proposals come
 * first, then kept memories it doesn't place yet, up to MARGINS. The
 * rest show their ID without a receipt (a server gap: an expanded Brief
 * read would carry them).
 */
export function marginsToRead(
  memories: readonly V2.Memory[],
  structure: BriefStructure,
): V2.Memory[] {
  const mentioned = new Set(briefRefs(structure));
  const first = memories.filter(
    (m) => mentioned.has(m.ref) || m.lifecycle === "proposed",
  );
  const rest = memories.filter((m) => !first.includes(m));
  return [...first, ...rest].slice(0, MARGINS);
}

export function createSdkBrief(
  client: V2Client,
  viewerId: () => string | undefined,
): BriefSource {
  return {
    async get({ space, signal }) {
      let brief: V2.Brief;
      try {
        brief = await client.v2.briefs.get(space.slug, { signal });
      } catch (err) {
        if (isNotFound(err)) return null;
        throw err;
      }
      const [memories, versions] = await Promise.all([
        listMemories(client, space.slug, signal),
        // Who wrote each line of prose; the page still loads without it.
        client.v2.briefs
          .versions(space.slug, { limit: VERSIONS, signal })
          .catch(() => null),
      ]);
      const viewer = viewerId();
      const current = versionOf(brief, viewer);
      const receipts = await receiptsFor(
        client,
        space.slug,
        marginsToRead(memories, current.structure),
        signal,
      );
      return buildBriefView({
        id: brief.id,
        ref: brief.ref,
        version: brief.version,
        structure: current.structure,
        by: current.by,
        at: current.at,
        reason: current.reason,
        memories: memories.map((m) => briefMemoryOf(m, receipts, viewer)),
        proseAuthors: versions
          ? proseAuthorsOf(
              current.structure,
              versions.items.map((v) => versionOf(v, viewer)),
            )
          : undefined,
      });
    },

    async versions({ space, cursor, signal }) {
      try {
        const page = await client.v2.briefs.versions(space.slug, {
          cursor,
          limit: VERSIONS,
          signal,
        });
        const viewer = viewerId();
        return {
          items: page.items.map((v) => versionOf(v, viewer)),
          nextCursor: page.has_more ? (page.next_cursor ?? null) : null,
        };
      } catch (err) {
        if (isNotFound(err)) return { items: [], nextCursor: null };
        throw err;
      }
    },

    async revise({ space, base, structure, reason, idempotencyKey }) {
      const body: V2.ReviseBriefInput = {
        title: structure.title,
        sections: structure.sections.map((section) => ({
          key: section.key,
          heading: section.heading,
          items: section.items.map((item) =>
            "ref" in item
              ? { ref: item.ref }
              : { text: item.text, cites: item.cites },
          ),
        })),
      };
      if (structure.summary) body.summary = structure.summary;
      if (reason) body.reason = reason;
      const result = await client.v2.briefs.revise(space.slug, body, {
        idempotencyKey,
        ifMatch: base ?? undefined,
      });
      return { ref: result.brief.ref, version: result.brief.version };
    },
  };
}
