import type { V2 } from "memax-sdk";
import type { SearchHit, SearchNote, SearchSource } from "./search";
import { actorOf, listItemOf, type V2Client } from "./sdk-records";

/**
 * Search through memax.v2.search (GET /v2/search): one request for the
 * first page of 50, the person's notes beside it, so the page can show
 * and count every facet without asking again. Each hit comes with the
 * receipts that wrote it and last changed it, so its row needs no
 * second read.
 */

/** The search's whole window: the API ranks its best 50. */
const PAGE = 50;

export function hitOf(
  hit: V2.SearchHit,
  viewerId: string | undefined,
): SearchHit {
  const receipts = new Map(hit.receipts.map((r) => [r.id, r]));
  const m = hit.memory;
  const last = receipts.get(m.last_receipt_id);
  return {
    spaceId: m.space_id,
    kind: m.kind,
    item: listItemOf(m, receipts, viewerId),
    author: actorOf(receipts.get(m.created_receipt_id), viewerId),
    decision:
      m.kind !== "decision"
        ? null
        : m.decision?.status === "open"
          ? "open"
          : "in_force",
    at: last?.occurred_at ?? m.updated_at,
  };
}

export function noteOf(note: V2.Note): SearchNote {
  return {
    ref: note.ref ?? null,
    spaceId: note.space_id,
    title: note.title,
    excerpt: note.excerpt,
    at: note.created_at,
  };
}

export function createSdkSearch(
  client: V2Client,
  viewerId: () => string | undefined,
): SearchSource {
  return {
    async search({ query, signal }) {
      const page = await client.v2.search(query, {
        includeNotes: true,
        limit: PAGE,
        signal,
      });
      return {
        hits: page.items.map((hit) => hitOf(hit, viewerId())),
        notes: (page.notes ?? []).map(noteOf),
        more: page.has_more,
        lexicalOnly: page.lexical_only,
      };
    },
  };
}
