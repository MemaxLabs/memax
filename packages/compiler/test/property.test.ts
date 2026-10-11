/**
 * Property-style tests over random inputs (seeded, so failures reproduce):
 *
 * - render, then parse the unmodified file: no changes, no drift;
 * - the same with line-ending, whitespace, list-marker and ordering noise: no
 *   proposals;
 * - change one cited line: exactly one edit, for that memory;
 * - compile twice, or with shuffled inputs: the same bytes.
 *
 * Each runs over a project's targets and over a personal space's home files.
 */
import { describe, expect, it } from "vitest";
import {
  adapters,
  compile,
  extractManagedBlock,
  findManagedBlock,
  HOME_BUDGET,
  isDrifted,
  MANAGED_END,
  MANAGED_START,
  parseBack,
  type CompileInput,
  type Memory,
  type SpaceKind,
  type TargetSettings,
} from "../src/index.js";
import { rng, shuffle } from "./helpers.js";

const WORDS = [
  "River",
  "jobs",
  "Postgres",
  "never",
  "use",
  "`pnpm test`",
  "the",
  "API",
  "returns",
  "problem+json",
  "OAuth",
  "2.1",
  "部署",
  "Fly.io",
  "Railway",
  "receipts",
  "(see",
  "ADR-4)",
  "M-0219",
  "[draft]",
  "café",
  "#tag",
  "50%",
  "*literal*",
  "->",
  "a:b",
  "-",
];
const SECTIONS = ["decisions", "conventions", "open", "preferences"];
const GLOBS = [
  "packages/web/**",
  "packages/server/**",
  "**/*.test.ts",
  "docs/*.md",
];
const AGENTS = [
  "claude-code",
  "gemini",
  "cursor",
  "copilot",
  "windsurf",
  "chatgpt",
];

function pick<T>(random: () => number, items: readonly T[]): T {
  return items[Math.floor(random() * items.length)];
}

/** A person's own file around a block: their lines, sometimes V1's block. */
function personFile(random: () => number): string {
  if (random() < 0.4) return "";
  const eol = random() < 0.7 ? "\n" : "\r\n";
  const lines = Array.from({ length: Math.floor(random() * 8) }, () =>
    pick(random, [
      "# My own notes",
      "- my own line [M-0001]",
      "",
      "Prefer small commits.  ",
      "@AGENTS.md",
      "部署 café",
      "## Heading",
    ]),
  );
  if (random() < 0.3)
    lines.splice(
      Math.floor(random() * (lines.length + 1)),
      0,
      MANAGED_START,
      "V1's guidance",
      MANAGED_END,
    );
  const text = lines.join(eol);
  return random() < 0.8 && text !== "" ? text + eol : text;
}

/** Every adapter but the home ones, which compile only for a personal space. */
function projectTargets(random: () => number): TargetSettings[] {
  return adapters
    .filter((a) => a.role !== "home")
    .map((a) => {
      const t: TargetSettings = { kind: a.kind };
      if (random() < 0.3) t.include = "kept_only";
      if (random() < 0.3) t.stale = "omit";
      if (random() < 0.5) t.size_budget = 1024 + Math.floor(random() * 1200);
      if ((a.kind === "agents_md" || a.kind === "chatgpt") && random() < 0.3)
        t.scoped = "omit";
      if (a.kind === "claude_md" && random() < 0.4) {
        t.user_owned = true;
        t.current = random() < 0.5 ? "# Mine\r\n\r\nKeep it short.  \r\n" : "";
      }
      return t;
    });
}

/** A personal space's files on one machine: the canonical, Claude's, Codex's. */
function homeTargets(random: () => number): TargetSettings[] {
  const tune = (t: TargetSettings) => {
    if (random() < 0.3) t.include = "kept_only";
    if (random() < 0.3) t.stale = "omit";
    if (random() < 0.5) t.size_budget = 1024 + Math.floor(random() * 3000);
    return t;
  };
  const claude: TargetSettings =
    random() < 0.5
      ? { kind: "claude_user" }
      : {
          kind: "claude_user",
          path: ".claude/CLAUDE.md",
          current: personFile(random),
        };
  return [
    tune({ kind: "agents_md", path: ".memax/personal/AGENTS.md" }),
    tune(claude),
    tune({
      kind: "codex_user",
      path: random() < 0.7 ? ".codex/AGENTS.md" : ".codex/AGENTS.override.md",
      current: personFile(random),
    }),
  ];
}

function randomInput(seed: number, kind: SpaceKind = "project"): CompileInput {
  const random = rng(seed);
  const sentence = () =>
    Array.from({ length: 2 + Math.floor(random() * 12) }, () =>
      pick(random, WORDS),
    ).join(" ") + ".";

  const memories: Memory[] = Array.from(
    { length: 3 + Math.floor(random() * 60) },
    (_, i) => {
      const section = pick(random, SECTIONS);
      const m: Memory = {
        ref: `M-${String(i + 1).padStart(4, "0")}`,
        statement: sentence(),
        section,
        kind: random() < 0.5 ? "fact" : "decision",
        state: section === "open" && random() < 0.7 ? "open" : "kept",
        read_score: Math.floor(random() * 50),
      };
      if (random() < 0.2)
        m.flags = random() < 0.5 ? ["stale"] : ["conflict", "stale"];
      if (random() < 0.1) m.stale_after = "2026-01-01T00:00:00Z";
      if (random() < 0.3)
        m.scope = {
          paths: shuffle(GLOBS, random).slice(0, 1 + Math.floor(random() * 2)),
        };
      if (random() < 0.15)
        m.scope = { ...m.scope, agents: [pick(random, AGENTS)] };
      return m;
    },
  );

  const placed = shuffle(memories, random).slice(
    0,
    Math.floor(memories.length * random()),
  );
  const sections = SECTIONS.filter(() => random() < 0.8).map((key) => ({
    key,
    heading: key[0].toUpperCase() + key.slice(1),
    items: [
      ...(random() < 0.4
        ? [
            {
              text: sentence(),
              cites: [
                pick(random, memories).ref,
                `M-9${Math.floor(random() * 900)}`,
              ],
            },
          ]
        : []),
      ...placed.filter((m) => m.section === key).map((m) => ({ ref: m.ref })),
    ],
  }));

  const targets =
    kind === "personal" ? homeTargets(random) : projectTargets(random);
  if (kind === "personal") {
    // Prose that cites two real memories (the made-up cite would keep it out
    // of every home file), and facts for Codex and Claude Code, so home
    // files see prose and routing. Two cites, as in project inputs: parse-
    // back pairs cited lines by their cites, so a prose line citing M-0012
    // alone beside M-0012's own line would pair by order, and shuffling
    // them would read as edits. Project inputs are as they always were.
    for (const s of sections)
      for (const item of s.items)
        if ("cites" in item) {
          const at = memories.findIndex((m) => m.ref === item.cites[0]);
          item.cites = [
            item.cites[0],
            memories[(at + 1) % memories.length].ref,
          ];
        }
    for (const m of memories)
      if (m.scope?.agents && random() < 0.6)
        m.scope.agents = [random() < 0.5 ? "codex" : "claude-code"];
  }

  return {
    version: 1,
    compile: {
      id: `C-${String(seed).padStart(4, "0")}`,
      at: "2026-10-06T12:00:00Z",
    },
    space: {
      slug: "prop",
      name: "Property",
      kind,
      url: "https://memax.app/prop/brief",
    },
    brief: { id: "B-0001", title: sentence(), sections },
    memories,
    targets,
  };
}

/** Shuffles each run of consecutive list items. */
function shuffleItems(content: string, random: () => number): string {
  const lines = content.split("\n");
  for (let i = 0; i < lines.length; ) {
    let j = i;
    while (j < lines.length && lines[j].startsWith("- ")) j++;
    if (j - i > 1)
      lines.splice(i, j - i, ...shuffle(lines.slice(i, j), random));
    i = Math.max(j, i + 1);
  }
  return lines.join("\n");
}

const SEEDS = Array.from({ length: 150 }, (_, i) => i + 1);
const KINDS: SpaceKind[] = ["project", "personal"];

describe("the random inputs", () => {
  it("exercise cited lines, scoped files, budget drops and managed blocks", () => {
    const seen = {
      files: 0,
      cited: 0,
      scoped: 0,
      dropped: 0,
      owned: 0,
      prose: 0,
    };
    for (const seed of SEEDS) {
      for (const f of compile(randomInput(seed)).files) {
        seen.files += 1;
        if (/\[M-\d+\]$/m.test(f.content)) seen.cited += 1;
        if (/\[M-\d+, M-\d+\]$/m.test(f.content)) seen.prose += 1;
        if (f.path.includes("/memax-")) seen.scoped += 1;
        if (f.dropped_for_budget.length > 0) seen.dropped += 1;
        if (f.user_owned) seen.owned += 1;
      }
    }
    expect(seen.files).toBeGreaterThan(600);
    for (const [what, n] of Object.entries(seen))
      expect(n, what).toBeGreaterThan(30);
  });

  it("exercise home files: routing, prose, budget drops and the person's own lines", () => {
    const seen = {
      files: 0,
      cited: 0,
      dropped: 0,
      blocks: 0,
      aroundTheirLines: 0,
      replacedV1: 0,
      prose: 0,
      codexOnly: 0,
      claudeOnly: 0,
    };
    for (const seed of SEEDS) {
      const input = randomInput(seed, "personal");
      const only = (agent: string) =>
        new Set(
          input.memories
            .filter((m) => m.scope?.agents?.includes(agent))
            .map((m) => m.ref),
        );
      const [codex, claude] = [only("codex"), only("claude-code")];
      for (const f of compile(input).files) {
        seen.files += 1;
        if (/\[M-\d+\]$/m.test(f.content)) seen.cited += 1;
        if (f.dropped_for_budget.length > 0) seen.dropped += 1;
        if (f.user_owned) seen.blocks += 1;
        const target = input.targets.find((t) => t.kind === f.target);
        if (f.user_owned && target?.current) seen.aroundTheirLines += 1;
        if (target?.current?.includes("V1's guidance")) seen.replacedV1 += 1;
        if (f.cites.some((r) => !f.refs.includes(r))) seen.prose += 1;
        // Agent-only facts land only in that agent's own file.
        for (const r of f.refs) {
          if (codex.has(r)) {
            expect(f.target, r).toBe("codex_user");
            seen.codexOnly += 1;
          }
          if (claude.has(r)) {
            expect(f.target, r).toBe("claude_user");
            seen.claudeOnly += 1;
          }
        }
        // Never a path-scoped fact: home files load in every project.
        for (const r of f.cites) {
          const m = input.memories.find((x) => x.ref === r);
          expect(m?.scope?.paths ?? [], `${f.path} ${r}`).toEqual([]);
        }
      }
    }
    expect(seen.files).toBe(SEEDS.length * 3);
    for (const [what, n] of Object.entries(seen))
      expect(n, what).toBeGreaterThan(20);
  });
});

/** The 1-based line range inside a file's block, or the whole file. */
function compared(content: string, owned: boolean): (line: number) => boolean {
  const at = owned ? findManagedBlock(content) : null;
  return at ? (n) => n > at.start && n < at.end : () => true;
}

describe.each(KINDS)("render then parse, %s spaces", (kind) => {
  it.each(SEEDS)(
    "seed %i: an unmodified file has no changes and no drift",
    (seed) => {
      for (const file of compile(randomInput(seed, kind)).files) {
        expect(isDrifted(file.drift_sha256, file.content)).toBe(false);
        const { changes, drift } = parseBack(file, file.content);
        expect(changes, file.path).toEqual([]);
        expect(drift.changed, file.path).toBe(false);
        expect(
          drift.header_edited ||
            drift.layout_edited ||
            drift.frontmatter_edited,
        ).toBe(false);
      }
    },
  );

  it.each(SEEDS)("seed %i: noise alone proposes nothing", (seed) => {
    const random = rng(seed * 7919);
    for (const file of compile(randomInput(seed, kind)).files) {
      const noisy = shuffleItems(file.content, random)
        .replace(/^- /gm, () => pick(random, ["- ", "* ", "+ ", "-   "]))
        .replace(/\n/g, () => pick(random, ["\n", "\r\n", "  \n", "\t\r\n"]));
      expect(parseBack(file, noisy).changes, file.path).toEqual([]);
      expect(
        isDrifted(file.drift_sha256, file.content.replace(/\r?\n/g, "\r\n")),
      ).toBe(false);
    }
  });

  it.each(SEEDS)(
    "seed %i: one changed cited line is one edit of that memory",
    (seed) => {
      const random = rng(seed * 104729);
      for (const file of compile(randomInput(seed, kind)).files) {
        const lines = file.content.split("\n");
        // In a file the person owns, only the block is Memax's to read.
        const ours = compared(file.content, file.user_owned);
        const cited = lines
          .map((l, i) => ({ l, i }))
          .filter(({ l, i }) => ours(i + 1) && /^- .*\[M-\d+\]$/.test(l));
        if (cited.length === 0) continue;
        const { l, i } = pick(random, cited);
        lines[i] = l.replace(/ \[M-\d+\]$/, " (edited) $&");
        const changes = parseBack(file, lines.join("\n")).changes;
        expect(changes, file.path).toMatchObject([
          { kind: "edit", ref: /M-\d+/.exec(l.slice(-8))?.[0] },
        ]);
        expect(isDrifted(file.drift_sha256, lines.join("\n"))).toBe(true);
      }
    },
  );
});

describe.each(KINDS)("determinism over random inputs, %s spaces", (kind) => {
  it.each(SEEDS.slice(0, 50))(
    "seed %i: same bytes when compiled twice or shuffled",
    (seed) => {
      const base = JSON.stringify(compile(randomInput(seed, kind)));
      expect(JSON.stringify(compile(randomInput(seed, kind)))).toBe(base);
      const random = rng(seed + 1);
      const shuffled = randomInput(seed, kind);
      shuffled.memories = shuffle(shuffled.memories, random);
      shuffled.targets = shuffle(shuffled.targets, random);
      expect(JSON.stringify(compile(shuffled))).toBe(base);
    },
  );

  it.each(SEEDS.slice(0, 50))(
    "seed %i: every file stays within its budget and caps",
    (seed) => {
      const input = randomInput(seed, kind);
      const result = compile(input);
      const utf8 = (s: string) => new TextEncoder().encode(s).length;
      const markers = utf8(`${MANAGED_START}\n${MANAGED_END}\n`);
      for (const file of result.files) {
        const target = input.targets.find((t) => t.kind === file.target);
        const budget = Math.min(
          target?.size_budget ??
            (kind === "personal" ? HOME_BUDGET : 32 * 1024),
          file.target === "agents_md" ? 32 * 1024 : Infinity,
        );
        // A block's budget is its lines; the markers and the person's own
        // lines come on top.
        const body = file.user_owned
          ? utf8(extractManagedBlock(file.content) ?? "") - markers
          : file.bytes;
        expect(body, file.path).toBeLessThanOrEqual(budget);
        if (file.target === "windsurf")
          expect(file.content.length).toBeLessThanOrEqual(12_000);
      }
    },
  );
});
