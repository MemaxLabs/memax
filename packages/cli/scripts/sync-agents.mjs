#!/usr/bin/env node
// Copies @memaxlabs/agents' sources into the CLI, verbatim.
//
// memax-cli is published to npm and @memaxlabs/agents isn't yet (it is
// published with the open-source launch, plan 25 §12, 3.7; the open-source
// design §2). So the CLI carries a copy of the registry instead of a
// dependency, as it does of the compiler files it runs
// (scripts/sync-compiler.mjs). This script keeps the copy identical to the
// source: `--check` (part of `pnpm lint`) fails when it differs.
//
//   node scripts/sync-agents.mjs          # rewrite the copy
//   node scripts/sync-agents.mjs --check  # fail if the copy is stale
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const cli = join(dirname(fileURLToPath(import.meta.url)), "..");
const src = join(cli, "..", "agents", "src");
const out = join(cli, "src", "lib", "agents");

const header = (name) =>
  `// Copied verbatim from ${name} by scripts/sync-agents.mjs.\n` +
  `// Don't edit it here: change @memaxlabs/agents, then run the script.\n\n`;

const check = process.argv.includes("--check");
const files = readdirSync(src).filter((f) => f.endsWith(".ts")).sort();
let stale = 0;
for (const f of files) {
  const name = `packages/agents/src/${f}`;
  const want = header(name) + readFileSync(join(src, f), "utf8");
  let have = "";
  try {
    have = readFileSync(join(out, f), "utf8");
  } catch {
    // missing: stale
  }
  if (have === want) continue;
  if (check) {
    console.error(
      `${join(out, f)} is out of date with ${name}. Run: node packages/cli/scripts/sync-agents.mjs`,
    );
    stale++;
  } else {
    mkdirSync(out, { recursive: true });
    writeFileSync(join(out, f), want);
    console.log(`Copied ${name}`);
  }
}
// A file the package no longer has goes from the copy too.
let copies = [];
try {
  copies = readdirSync(out).filter((f) => f.endsWith(".ts"));
} catch {
  // no copy yet
}
for (const f of copies) {
  if (files.includes(f)) continue;
  console.error(
    `${join(out, f)} isn't in packages/agents/src: delete it, then run: node packages/cli/scripts/sync-agents.mjs`,
  );
  stale++;
}
process.exit(stale > 0 ? 1 : 0);
