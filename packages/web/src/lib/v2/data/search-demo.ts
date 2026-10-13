import { DEMO_SPACES } from "./demo-dataset";
import { DEMO_MEMORY_PAGES } from "./demo-memories-data";
import { agent, at, JY, M0174, ZZ } from "./demo-review-data";
import type { MemoryListItem } from "./memories";
import type { Actor } from "./records";
import type { SearchHit, SearchNote, SearchSource } from "./search";
import { matchTerms } from "../search-view";

/**
 * The demo's search (Search.png): the board's rows for "deploy" first,
 * then every memory the demo's record shows, matched by word prefixes
 * the way the API's full-text lane matches them, so any query works in
 * the dev fixtures. Data, not copy: it stays in English like the boards.
 * The demo has no handoffs to find (they aren't searched yet).
 */

const spaceId = (slug: string) =>
  DEMO_SPACES.find((s) => s.slug === slug)?.id ?? slug;

function hit(
  slug: string,
  item: Omit<MemoryListItem, "source" | "note" | "forgotten"> &
    Partial<MemoryListItem>,
  author: Actor | null,
  kind: SearchHit["kind"] = "fact",
  decision: SearchHit["decision"] = null,
): SearchHit {
  return {
    spaceId: spaceId(slug),
    kind,
    item: { source: null, note: null, forgotten: null, ...item },
    author,
    decision,
    at: item.receipt?.at ?? at("09:00"),
  };
}

const kept = (by: Actor, when: string) => ({
  state: "kept" as const,
  receipt: { by, action: "kept" as const, at: when },
});

/** Search.png's rows, in its order. */
const BOARD: SearchHit[] = [
  hit(
    "memax-v2",
    {
      ref: "M-0431",
      statement: "Deploy the v2 API to Fly.io in iad and ams.",
      section: "decisions",
      state: "conflict",
      receipt: { by: agent("codex"), action: "proposed", at: at("14:26") },
    },
    agent("codex"),
  ),
  hit(
    "memax-v2",
    {
      ref: M0174.ref,
      statement: M0174.statement,
      section: "decisions",
      ...kept(JY, M0174.at),
    },
    JY,
  ),
  hit(
    "memax-v2",
    {
      ref: "M-0083",
      statement:
        "Workers deploy from the main branch only, after the parity check passes.",
      section: "conventions",
      ...kept(ZZ, at("10:12", "2026-08-20")),
    },
    ZZ,
  ),
  hit(
    "memax-v2",
    {
      ref: "M-0052",
      statement: "Never deploy on a Friday after 16:00.",
      section: "conventions",
      ...kept(JY, at("16:40", "2026-07-30")),
    },
    JY,
  ),
  hit(
    "memax-v2",
    {
      ref: "M-0061",
      statement: "Preview deploys expire after 7 days.",
      section: "conventions",
      ...kept(ZZ, at("11:05", "2026-08-11")),
    },
    agent("cursor"),
  ),
  hit(
    "memax-team",
    {
      ref: "M-0322",
      statement:
        "Staging deploys run from the release workflow, never by hand.",
      section: "conventions",
      ...kept(JY, at("09:30", "2026-09-24")),
    },
    agent("claude-code"),
  ),
  hit(
    "memax-team",
    {
      ref: "M-0318",
      statement: "Deploy previews to Railway",
      section: "decisions",
      ...kept(JY, at("15:10", "2026-09-18")),
    },
    JY,
    "decision",
    "open",
  ),
  hit(
    "memax-team",
    {
      ref: "M-0301",
      statement: "No Friday deploys after 16:00",
      section: "decisions",
      ...kept(JY, at("16:45", "2026-07-30")),
    },
    JY,
    "decision",
    "in_force",
  ),
];

/** The board's "9 raw notes hidden": V1 memories about deploys, now notes. */
const NOTES: SearchNote[] = [
  [
    "N-0871",
    "Fly deploy failed on the health check; raised the grace period to 30s.",
  ],
  [
    "N-0872",
    "Railway preview deploys need the DATABASE_URL of the branch database.",
  ],
  ["N-0873", "Deploying the worker before the API breaks River's job kinds."],
  ["N-0874", "The deploy workflow runs migrations first, then the API."],
  ["N-0875", "Codex suggested deploying from tags instead of main."],
  ["N-0876", "Roll a deploy back with wrangler rollback --env production."],
  ["N-0877", "Friday deploys were stopped after the July 26 incident."],
  ["N-0878", "Deploy previews for every pull request that touches web."],
  ["N-0879", "Notes on the iad and ams regions for the v2 API deploy."],
].map(([ref, excerpt], i) => ({
  ref,
  spaceId: spaceId("memax-v2"),
  title: excerpt.split(/[.;:]/)[0],
  excerpt,
  at: at("10:00", `2026-07-${String(10 + i).padStart(2, "0")}`),
}));

/** The rest of the demo's record, as Memories lists it; what a search can find. */
const RECORD: SearchHit[] = Object.entries(DEMO_MEMORY_PAGES).flatMap(
  ([slug, pages]) =>
    pages
      .flat()
      .filter(
        (item) =>
          ["kept", "proposed", "conflict", "stale"].includes(item.state) &&
          !BOARD.some((b) => b.item.ref === item.ref),
      )
      .map((item) => hit(slug, item, item.receipt?.by ?? null)),
);

const CORPUS = [...BOARD, ...RECORD];

/** The best first: more of the words matched, then the corpus's order. */
function rank<T>(items: T[], text: (item: T) => string, query: string): T[] {
  return items
    .map((item, i) => ({ item, i, n: matchTerms(text(item), query) }))
    .filter((x) => x.n > 0)
    .sort((a, b) => b.n - a.n || a.i - b.i)
    .map((x) => x.item);
}

export function createDemoSearch({
  delayMs = 0,
}: { delayMs?: number } = {}): SearchSource {
  return {
    async search({ query, signal }) {
      if (delayMs > 0) await new Promise((r) => setTimeout(r, delayMs));
      if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
      return {
        hits: rank(CORPUS, (h) => h.item.statement, query),
        notes: rank(NOTES, (n) => n.excerpt, query),
        more: false,
        lexicalOnly: true,
      };
    },
  };
}
