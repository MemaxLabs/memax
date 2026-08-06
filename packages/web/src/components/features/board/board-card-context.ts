import type { BoardSlot } from "memax-sdk";
import { formatContextBlock, type ContextEntry } from "@/lib/format-context";

/**
 * Turning a board card into portable context (plan 25 P3).
 *
 * A card already carries everything an external agent needs — the
 * claim (title + body) and the receipts (quoted excerpts) — so the
 * copy path reads the slot payload directly instead of round-tripping
 * to fetch the cited memories. What the user sees on the card is
 * exactly what the agent gets.
 */

interface PayloadQuote {
  excerpt?: unknown;
  when?: unknown;
  memory_id?: unknown;
}

function str(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function quotesFromPayload(payload: Record<string, unknown> | undefined) {
  if (!payload) return [] as PayloadQuote[];
  const quotes: PayloadQuote[] = [];
  // Lane B wow kinds carry `quotes`; echo carries `then` / `now`.
  if (Array.isArray(payload.quotes)) {
    quotes.push(...(payload.quotes as PayloadQuote[]));
  }
  for (const key of ["then", "now"] as const) {
    const value = payload[key];
    if (value && typeof value === "object") quotes.push(value as PayloadQuote);
  }
  return quotes;
}

/**
 * Build the copy-to-agent payload for a card. Returns the
 * `<memax-context>` block (the same envelope the memory batch-copy
 * uses, so pasting into any agent behaves identically) or null when
 * the card has no quotable substance.
 */
export function buildBoardCardContext(slot: BoardSlot): string | null {
  const payload = slot.payload as Record<string, unknown> | undefined;
  const body = str(payload?.body) || str(payload?.description);
  const quotes = quotesFromPayload(payload)
    .map((q) => ({ excerpt: str(q.excerpt), when: str(q.when) }))
    .filter((q) => q.excerpt !== "");

  const entries: ContextEntry[] = [];
  if (body) {
    entries.push({
      title: slot.title,
      content: body,
      kind: slot.kind,
      source: "memax board",
    });
  }
  for (const quote of quotes) {
    entries.push({
      title: quote.when
        ? `Cited memory (${quote.when.slice(0, 10)})`
        : "Cited memory",
      content: quote.excerpt,
      source: "memax memory",
    });
  }
  // A card with neither a body nor quotes (e.g. a pure-count Lane A
  // card) has nothing an agent could act on — better to hide the
  // affordance than paste an empty block.
  if (entries.length === 0) return null;

  return formatContextBlock(entries, `board card · ${slot.kind}`);
}
