import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { CompileInput, Memory } from "../src/index.js";

export const FIXTURES = join(
  dirname(fileURLToPath(import.meta.url)),
  "fixtures",
);

export function loadInput(name: string): CompileInput {
  return JSON.parse(
    readFileSync(join(FIXTURES, name, "input.json"), "utf8"),
  ) as CompileInput;
}

/** A fresh copy of the demo `memax-v2` input. */
export function demo(): CompileInput {
  return loadInput("demo-memax-v2");
}

/** A small kept memory, for tests that build inputs by hand. */
export function memory(
  ref: string,
  statement: string,
  extra: Partial<Memory> = {},
): Memory {
  return {
    ref,
    statement,
    section: "conventions",
    kind: "fact",
    state: "kept",
    ...extra,
  };
}

/** A minimal valid input around the given memories and targets. */
export function input(
  memories: Memory[],
  targets: CompileInput["targets"] = [{ kind: "agents_md" }],
): CompileInput {
  return {
    version: 1,
    compile: { id: "C-0001", at: "2026-10-05T14:31:00Z" },
    space: {
      slug: "demo",
      name: "Demo",
      kind: "project",
      url: "https://memax.app/demo/brief",
    },
    brief: {
      id: "B-0001",
      title: "Demo brief",
      sections: [
        { key: "decisions", heading: "Decisions", items: [] },
        { key: "conventions", heading: "Conventions", items: [] },
        { key: "open", heading: "Open", items: [] },
      ],
    },
    memories,
    targets,
  };
}

/** Mulberry32: a tiny seeded PRNG, so "random" tests are reproducible. */
export function rng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function shuffle<T>(items: readonly T[], random: () => number): T[] {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}
