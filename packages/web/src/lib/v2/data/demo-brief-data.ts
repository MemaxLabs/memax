import type { BriefInputMemory } from "./brief-view";
import type { BriefStructure, BriefVersionView } from "./brief";
import { DEMO_MEMORY_PAGES } from "./demo-memories-data";
import { DREAM, JY, ZZ, agent, at } from "./demo-review-data";
import type { Actor } from "./records";
import type { Section } from "./types";

/**
 * The demo's Brief (Brief.png, BriefEdit.png, BriefHistory.png): the
 * "Memax V2 engineering brief", B-0043, rewritten by Dream at 03:12,
 * with the versions before it. The compiled files (demo-compiled.ts)
 * were compiled from exactly this record. Data, not copy: it stays in
 * English like the boards' own content.
 */

const NB = "‑";

const P1 =
  "Memax V2 is the context layer every agent on the team reads from: a cited record of decisions and conventions that compiles into `CLAUDE.md`, `AGENTS.md` and Cursor rules, and answers live over MCP.";
const P2 =
  "Writes from agents go to Review unless the agent is trusted to write.";
const OPEN_BEFORE = "Fly.io or Railway for the v2 API?";
const OPEN =
  "Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config.";
export const M0219_BEFORE = `Background jobs run on River, Postgres${NB}backed.`;
const M0219 = `${M0219_BEFORE} We do not use Temporal.`;

type Items = BriefStructure["sections"][number]["items"];

function brief(
  decisions: string[],
  conventions: string[],
  open: string,
): BriefStructure {
  const refs = (list: string[]): Items => list.map((ref) => ({ ref }));
  return {
    title: "Memax V2 engineering brief",
    summary: "What every agent on this project reads before it writes code.",
    sections: [
      {
        key: "overview",
        heading: "What this is",
        items: [
          { text: P1, cites: ["M-0001"] },
          { text: P2, cites: ["M-0012"] },
        ],
      },
      { key: "decisions", heading: "Decisions", items: refs(decisions) },
      { key: "conventions", heading: "Conventions", items: refs(conventions) },
      {
        key: "open",
        heading: "Open",
        items: [{ text: open, cites: ["M-0431", "M-0174"] }],
      },
    ],
  };
}

const DECISIONS = ["M-0219", "M-0102", "M-0098", "M-0187"];
const CONVENTIONS = ["M-0071", "M-0112"];

/** B-0038 … B-0043, newest first (BriefHistory.png). */
export const DEMO_VERSIONS: Readonly<Record<string, BriefVersionView[]>> = {
  "memax-v2": [
    {
      ref: "B-0043",
      version: 6,
      parent: 5,
      current: true,
      by: DREAM,
      at: at("03:12"),
      reason: "Edition No. 214",
      facts: 11,
      structure: brief(DECISIONS, [...CONVENTIONS, "M-0436"], OPEN),
      edition: [
        {
          kind: "reworded",
          ref: "M-0219",
          section: "decisions",
          before: M0219_BEFORE,
          after: M0219,
          note: "Dream folded 9 notes that all said Temporal was dropped",
        },
        {
          kind: "flagged",
          ref: "M-0187",
          section: "decisions",
          note: "its source changed in PR #198",
        },
      ],
      notes: { "M-0436": "written by Claude Code, which may write here" },
    },
    {
      ref: "B-0042",
      version: 5,
      parent: 4,
      current: false,
      by: ZZ,
      at: at("16:02", "2026-10-04"),
      reason: "Say what to do while the deploy target is open",
      facts: 10,
      structure: brief(DECISIONS, CONVENTIONS, OPEN),
    },
    {
      ref: "B-0041",
      version: 4,
      parent: 3,
      current: false,
      by: ZZ,
      at: at("10:58", "2026-10-02"),
      reason: null,
      facts: 10,
      structure: brief(DECISIONS, CONVENTIONS, OPEN_BEFORE),
    },
    {
      ref: "B-0040",
      version: 3,
      parent: 2,
      current: false,
      by: ZZ,
      at: at("18:40", "2026-09-30"),
      reason: null,
      facts: 9,
      structure: brief(DECISIONS.slice(1), CONVENTIONS, OPEN_BEFORE),
    },
    {
      ref: "B-0039",
      version: 2,
      parent: 1,
      current: false,
      by: JY,
      at: at("11:20", "2026-09-29"),
      reason: null,
      facts: 8,
      structure: brief(DECISIONS.slice(2), CONVENTIONS, OPEN_BEFORE),
    },
    {
      ref: "B-0038",
      version: 1,
      parent: null,
      current: false,
      by: ZZ,
      at: at("15:20", "2026-09-28"),
      reason: null,
      facts: 7,
      structure: brief(DECISIONS.slice(3), CONVENTIONS, OPEN_BEFORE),
    },
  ],
};

const kept = (by: Actor, when: string) => ({
  by,
  action: "kept" as const,
  at: when,
});

function memory(
  ref: string,
  text: string,
  section: Section,
  fields: Partial<BriefInputMemory> = {},
): BriefInputMemory {
  return {
    ref,
    text,
    section,
    state: "kept",
    lifecycle: "kept",
    version: 1,
    receipt: null,
    source: null,
    scope: [],
    ...fields,
  };
}

/** The memories the demo Brief rests on, and the proposals waiting in its sections. */
export const DEMO_BRIEF_MEMORIES: Readonly<Record<string, BriefInputMemory[]>> =
  {
    "memax-v2": [
      memory(
        "M-0001",
        "Memax V2 is the context layer every agent on the team reads from.",
        "decisions",
        { receipt: kept(ZZ, at("15:10", "2026-09-28")) },
      ),
      memory(
        "M-0012",
        "Agents at Propose send writes to Review; agents at Write keep directly.",
        "decisions",
        { receipt: kept(ZZ, at("15:12", "2026-09-28")) },
      ),
      memory("M-0219", M0219, "decisions", {
        version: 2,
        receipt: kept(ZZ, at("10:58", "2026-10-02")),
        source: "PR #212",
        changed: "We do not use Temporal.",
      }),
      memory(
        "M-0102",
        `Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026${NB}07${NB}28.`,
        "decisions",
        {
          receipt: kept(ZZ, at("18:40", "2026-09-30")),
          source: "session 3e1a",
        },
      ),
      memory(
        "M-0098",
        "API errors are RFC 9457 problem+json, never bare strings.",
        "decisions",
        { receipt: kept(JY, at("11:20", "2026-09-12")) },
      ),
      memory("M-0187", "Ask memax answers with the Haiku tier.", "decisions", {
        state: "stale",
        receipt: { by: DREAM, action: "flagged", at: at("03:12") },
      }),
      memory(
        "M-0071",
        "pnpm workspaces only. Never run `npm install` at the root.",
        "conventions",
        { receipt: kept(JY, at("09:30", "2026-08-29")) },
      ),
      memory(
        "M-0112",
        "Every write tool returns a receipt ID the caller can cite.",
        "conventions",
        { receipt: kept(ZZ, at("12:05", "2026-09-03")) },
      ),
      memory(
        "M-0436",
        "Compile adapters live in packages/compiler, one per target.",
        "conventions",
        {
          receipt: kept(agent("claude-code"), at("17:20", "2026-10-04")),
          source: "packages/compiler/README.md",
        },
      ),
      // Kept from the day-one cleanup on Oct 4, after B-0042 and not yet
      // placed: they compile at the end of Conventions, and into Cursor's rule.
      memory("M-0441", "Run tests with `pnpm test`.", "conventions", {
        receipt: kept(ZZ, at("09:05", "2026-10-04")),
        scope: ["packages/web/**"],
      }),
      memory(
        "M-0442",
        "Web screens use only packages/ledger components; no Tailwind under (ledger).",
        "conventions",
        {
          receipt: kept(ZZ, at("09:05", "2026-10-04")),
          scope: ["packages/web/**"],
        },
      ),
      memory(
        "M-0174",
        "Deploy the v2 API to Railway for its preview environments.",
        "decisions",
        { state: "conflict", receipt: kept(JY, at("16:45", "2026-09-21")) },
      ),
      memory(
        "M-0431",
        "Deploy the v2 API to Fly.io in iad and ams.",
        "decisions",
        {
          state: "conflict",
          lifecycle: "proposed",
          receipt: { by: agent("codex"), action: "proposed", at: at("14:26") },
        },
      ),
      memory(
        "M-0430",
        "MCP write tools must ask for confirmation with input_required.",
        "conventions",
        {
          state: "proposed",
          lifecycle: "proposed",
          receipt: {
            by: agent("claude-code"),
            action: "proposed",
            at: at("13:40"),
          },
        },
      ),
    ],
  };

/** Brief.png's "Read this week": 74 reads. */
export const DEMO_READS: Readonly<
  Record<string, { total: number; agents: { agent: string; reads: number }[] }>
> = {
  "memax-v2": {
    total: 74,
    agents: [
      { agent: "claude-code", reads: 41 },
      { agent: "codex", reads: 18 },
      { agent: "cursor", reads: 12 },
      { agent: "chatgpt", reads: 3 },
    ],
  },
};

/** Sources' short titles (Brief.png's Sources panel). */
export const DEMO_TITLES: Readonly<Record<string, Record<string, string>>> = {
  "memax-v2": {
    "M-0001": "Product thesis",
    "M-0012": "Autonomy levels",
    "M-0219": "Jobs on River",
    "M-0102": "MCP transport",
    "M-0098": "Error format",
    "M-0071": "Workspace rules",
    "M-0112": "Receipt IDs",
    "M-0436": "Compile adapters",
    "M-0441": "Web tests",
    "M-0442": "Web components",
  },
};

const SECTION_ORDER: Section[] = [
  "decisions",
  "conventions",
  "preferences",
  "open_question",
];
const HEADINGS: Record<Section, [string, string]> = {
  decisions: ["decisions", "Decisions"],
  conventions: ["conventions", "Conventions"],
  preferences: ["preferences", "Preferences"],
  open_question: ["open", "Open"],
};

/**
 * Another demo space's Brief: its first version, one section per kind
 * of memory, from the rows Memories shows. Null for a space with
 * nothing kept (memax-web).
 */
export function demoBriefOf(slug: string): {
  version: BriefVersionView;
  memories: BriefInputMemory[];
} | null {
  const rows = (DEMO_MEMORY_PAGES[slug] ?? [])
    .flat()
    .filter((row) => row.state === "kept" && row.statement);
  if (rows.length === 0) return null;
  const memories = rows.map((row) =>
    memory(row.ref, row.statement, row.section, {
      receipt: row.receipt && {
        by: row.receipt.by,
        action: row.receipt.action,
        at: row.receipt.at,
      },
      source: row.source,
    }),
  );
  const sections = SECTION_ORDER.flatMap((section) => {
    const items = rows
      .filter((row) => row.section === section)
      .map((row) => ({ ref: row.ref }));
    const [key, heading] = HEADINGS[section];
    return items.length ? [{ key, heading, items }] : [];
  });
  return {
    version: {
      ref: "B-0001",
      version: 1,
      parent: null,
      current: true,
      by: ZZ,
      at: at("09:00", "2026-09-28"),
      reason: null,
      facts: rows.length,
      structure: {
        title: slug === "personal" ? "Personal" : "Memax team",
        summary: null,
        sections,
      },
    },
    memories,
  };
}
