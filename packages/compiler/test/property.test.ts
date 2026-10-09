/**
 * Property-style tests over random inputs (seeded, so failures reproduce):
 *
 * - render, then parse the unmodified file: no changes, no drift;
 * - the same with line-ending, whitespace, list-marker and ordering noise: no
 *   proposals;
 * - change one cited line: exactly one edit, for that memory;
 * - compile twice, or with shuffled inputs: the same bytes.
 */
import { describe, expect, it } from "vitest";
import {
  adapters,
  compile,
  isDrifted,
  parseBack,
  type CompileInput,
  type Memory,
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

function randomInput(seed: number): CompileInput {
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

  const targets: TargetSettings[] = adapters.map((a) => {
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

  return {
    version: 1,
    compile: {
      id: `C-${String(seed).padStart(4, "0")}`,
      at: "2026-10-06T12:00:00Z",
    },
    space: {
      slug: "prop",
      name: "Property",
      kind: "project",
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
});

describe("render then parse", () => {
  it.each(SEEDS)(
    "seed %i: an unmodified file has no changes and no drift",
    (seed) => {
      for (const file of compile(randomInput(seed)).files) {
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
    for (const file of compile(randomInput(seed)).files) {
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
      for (const file of compile(randomInput(seed)).files) {
        const lines = file.content.split("\n");
        const cited = lines
          .map((l, i) => ({ l, i }))
          .filter(({ l }) => /^- .*\[M-\d+\]$/.test(l));
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

describe("determinism over random inputs", () => {
  it.each(SEEDS.slice(0, 50))(
    "seed %i: same bytes when compiled twice or shuffled",
    (seed) => {
      const base = JSON.stringify(compile(randomInput(seed)));
      expect(JSON.stringify(compile(randomInput(seed)))).toBe(base);
      const random = rng(seed + 1);
      const shuffled = randomInput(seed);
      shuffled.memories = shuffle(shuffled.memories, random);
      shuffled.targets = shuffle(shuffled.targets, random);
      expect(JSON.stringify(compile(shuffled))).toBe(base);
    },
  );

  it.each(SEEDS.slice(0, 50))(
    "seed %i: every file stays within its budget and caps",
    (seed) => {
      const input = randomInput(seed);
      const result = compile(input);
      for (const file of result.files) {
        const target = input.targets.find((t) => t.kind === file.target);
        const budget = Math.min(
          target?.size_budget ?? 32 * 1024,
          file.target === "agents_md" ? 32 * 1024 : Infinity,
        );
        if (!file.user_owned)
          expect(file.bytes, file.path).toBeLessThanOrEqual(budget);
        if (file.target === "windsurf")
          expect(file.content.length).toBeLessThanOrEqual(12_000);
      }
    },
  );
});
