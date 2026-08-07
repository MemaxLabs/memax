/**
 * Agent handoff prompts for 接下来 (nextup) items.
 *
 * A nextup item is already a task with receipts — an imperative title,
 * a one-line why, and the verbatim memories proving the loop is open.
 * That is exactly the material an agent needs to start, so the handoff
 * is assembled client-side from the card payload: no extra API call,
 * no second LLM pass, nothing the user has to retype.
 *
 * Shape follows Anthropic's prompt guidance — an explicit task, the
 * evidence in tags, constraints that keep the agent from inventing
 * project detail, and a success bar it can check itself against. It is
 * deliberately NOT `formatContextBlock`: that envelope says "here is
 * background, use it if relevant", this one says "do this, and here is
 * what we actually know".
 *
 * The scaffolding is localized (a zh user hands their agent a zh
 * prompt); the tag names stay English because they are structure, not
 * prose.
 */

/** Quotes past this count are dropped — a handoff is a brief, not a dump. */
export const MAX_HANDOFF_QUOTES = 6;

/** Loose payload shapes: server data reaches this unvalidated. */
export interface HandoffQuoteLike {
  memory_id?: unknown;
  when?: unknown;
  excerpt?: unknown;
}

export interface HandoffItemLike {
  title?: unknown;
  why?: unknown;
  quotes?: unknown;
}

/** Where the task came from — rendered as `<context>` attributes. */
export interface HandoffCardMeta {
  /** Board card kind (`nextup`). Machine-readable, not translated. */
  kind: string;
  /** Hub the card belongs to, when known — the agent's project name. */
  hubName?: string;
  /** RFC3339 timestamp of when the card was written. */
  generatedAt?: string;
}

/**
 * Structural slice of the translations object — `t` from `useLocale()`
 * satisfies it, and tests can pass a literal.
 */
export interface HandoffTranslations {
  board: {
    handoffPreamble: string;
    handoffConstraints: string;
    handoffSuccess: string;
    handoffNoContext: string;
  };
}

interface NormalizedQuote {
  day: string;
  excerpt: string;
  memoryId: string;
}

interface NormalizedItem {
  title: string;
  why: string;
  quotes: NormalizedQuote[];
}

function str(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** Collapse a quoted excerpt to a single line so each receipt is one row. */
function oneLine(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

/**
 * ISO day for a quote's timestamp. Dates are rendered `YYYY-MM-DD`
 * rather than locale-formatted: the reader is an agent, and an
 * unambiguous date beats a pretty one. Unparseable values yield "" so
 * one bad payload can't poison the prompt.
 */
function isoDay(value: unknown): string {
  const raw = str(value);
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return "";
  return date.toISOString().slice(0, 10);
}

/** Attribute values are user data — keep them from breaking the tag. */
function escapeAttr(value: string): string {
  return value
    .replace(/[<>"&]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function normalizeItem(item: HandoffItemLike): NormalizedItem | null {
  const title = str(item.title);
  // No title, no task — there is nothing to hand off.
  if (!title) return null;
  const rawQuotes = Array.isArray(item.quotes)
    ? (item.quotes as HandoffQuoteLike[])
    : [];
  const quotes: NormalizedQuote[] = [];
  for (const raw of rawQuotes) {
    if (!raw || typeof raw !== "object") continue;
    const excerpt = oneLine(str(raw.excerpt));
    if (!excerpt) continue;
    quotes.push({
      day: isoDay(raw.when),
      excerpt,
      memoryId: str(raw.memory_id),
    });
    if (quotes.length >= MAX_HANDOFF_QUOTES) break;
  }
  return { title, why: str(item.why), quotes };
}

function renderContext(
  item: NormalizedItem,
  meta: HandoffCardMeta,
  t: HandoffTranslations,
): string {
  const attrs = [
    'source="memax"',
    `card="${escapeAttr(meta.kind)}"`,
    meta.hubName ? `hub="${escapeAttr(meta.hubName)}"` : "",
    meta.generatedAt ? `generated="${isoDay(meta.generatedAt)}"` : "",
  ].filter(Boolean);
  const lines = item.quotes.map((quote) => {
    const date = quote.day ? `${quote.day} — ` : "";
    const ref = quote.memoryId ? ` (memax_memory_id: ${quote.memoryId})` : "";
    return `- ${date}"${quote.excerpt}"${ref}`;
  });
  // An item with no usable receipt still gets a context block: the
  // agent must be told the evidence is missing, not left to assume it
  // was simply omitted.
  const body = lines.length > 0 ? lines.join("\n") : t.board.handoffNoContext;
  return `<context ${attrs.join(" ")}>\n${body}\n</context>`;
}

function renderTask(
  item: NormalizedItem,
  meta: HandoffCardMeta,
  t: HandoffTranslations,
  position?: { index: number; total: number },
): string {
  const attrs = position
    ? ` index="${position.index}" of="${position.total}"`
    : "";
  const parts = [item.title];
  if (item.why) parts.push(`<why_now>\n${item.why}\n</why_now>`);
  parts.push(renderContext(item, meta, t));
  return `<task${attrs}>\n${parts.join("\n\n")}\n</task>`;
}

function render(
  items: NormalizedItem[],
  meta: HandoffCardMeta,
  t: HandoffTranslations,
): string | null {
  if (items.length === 0) return null;
  const multi = items.length > 1;
  const tasks = items.map((item, index) =>
    renderTask(
      item,
      meta,
      t,
      multi ? { index: index + 1, total: items.length } : undefined,
    ),
  );
  return [
    t.board.handoffPreamble,
    ...tasks,
    `<constraints>\n${t.board.handoffConstraints}\n</constraints>`,
    `<success_criteria>\n${t.board.handoffSuccess}\n</success_criteria>`,
  ].join("\n\n");
}

/**
 * Build the handoff prompt for a single nextup item. Returns null when
 * the item has no title — better to hide the affordance than to hand
 * an agent an empty task.
 */
export function buildAgentHandoffPrompt(
  item: HandoffItemLike,
  meta: HandoffCardMeta,
  t: HandoffTranslations,
): string | null {
  const normalized = normalizeItem(item);
  if (!normalized) return null;
  return render([normalized], meta, t);
}

/**
 * Build one prompt covering every item on the card. The tasks are
 * numbered and each keeps its own receipts; the preamble, constraints
 * and success bar are stated once so the agent reads one brief rather
 * than three stapled copies.
 */
export function buildAgentHandoffBundle(
  items: HandoffItemLike[],
  meta: HandoffCardMeta,
  t: HandoffTranslations,
): string | null {
  const normalized = items
    .map(normalizeItem)
    .filter((item): item is NormalizedItem => item !== null);
  return render(normalized, meta, t);
}
