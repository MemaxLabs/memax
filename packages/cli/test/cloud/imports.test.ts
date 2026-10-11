// The local session-start hook stays local (cloud warm-start design §3.1):
// nothing hook-main.ts imports reaches the cloud start's code, and what it
// loads on its critical path (its static imports, all the way down) is
// Node built-ins without crypto or any network module. The cloud start,
// for its part, imports nothing but Node's built-ins and this CLI's own
// files, so @memaxlabs/start can bundle it with no dependencies.
import { readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const src = join(import.meta.dirname, "..", "..", "src");

interface Graph {
  files: Set<string>;
  /** Bare specifiers (node:fs, memax-sdk), with a file that imports each. */
  bare: Map<string, string>;
  /** Files reached only through a dynamic import(). */
  lazy: Set<string>;
}

const STATIC =
  /^\s*(?:import|export)\s+(?!type\b)(?:[^"';]*?\sfrom\s+)?["']([^"']+)["']/gm;
const DYNAMIC = /\bimport\(\s*["']([^"']+)["']\s*\)/g;

function resolveTs(from: string, spec: string): string {
  return resolve(dirname(from), spec.replace(/\.js$/, ".ts"));
}

/** The static import graph from entry, and where its dynamic imports lead. */
function graph(entry: string): Graph {
  const g: Graph = { files: new Set(), bare: new Map(), lazy: new Set() };
  const walk = (file: string) => {
    if (g.files.has(file)) return;
    g.files.add(file);
    const text = readFileSync(file, "utf8");
    for (const m of text.matchAll(STATIC)) {
      const spec = m[1];
      if (spec.startsWith(".")) walk(resolveTs(file, spec));
      else if (!g.bare.has(spec)) g.bare.set(spec, relative(src, file));
    }
    for (const m of text.matchAll(DYNAMIC))
      if (m[1].startsWith(".")) g.lazy.add(resolveTs(file, m[1]));
  };
  walk(entry);
  return g;
}

describe("the import graphs", () => {
  it("keeps the local hook's critical path free of the cloud start, crypto and the network", () => {
    const g = graph(join(src, "hook-main.ts"));
    const files = [...g.files].map((f) => relative(src, f));
    expect(
      files.filter(
        (f) => f.startsWith("lib/cloud-start/") || f === "cloud-start-main.ts",
      ),
    ).toEqual([]);
    expect(files).toContain("lib/hook/stdio.ts");
    const network = [
      "node:crypto",
      "node:http",
      "node:https",
      "node:net",
      "node:tls",
      "node:dgram",
      "node:dns",
    ];
    expect(
      [...g.bare.keys()].filter(
        (b) => network.includes(b) || !b.startsWith("node:"),
      ),
    ).toEqual([]);
    // Its one lazy module is `memax hook flush`, which runs in another process.
    expect(
      [...g.lazy]
        .map((f) => relative(src, f))
        .filter((f) => f.includes("cloud")),
    ).toEqual([]);
  });

  it("builds the cloud start from Node's built-ins and the CLI's own files only", () => {
    const g = graph(join(src, "cloud-start-main.ts"));
    // The walk reaches the request and the drift hash, deep down.
    expect(g.bare.has("node:https") && g.bare.has("node:crypto")).toBe(true);
    expect([...g.bare.keys()].filter((b) => !b.startsWith("node:"))).toEqual(
      [],
    );
    expect([...g.files].every((f) => f.startsWith(src))).toBe(true);
    expect([...g.lazy]).toEqual([]);
  });
});
