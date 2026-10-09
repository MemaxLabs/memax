#!/usr/bin/env node
// Copies the compiler files the daemon needs into the CLI, verbatim.
//
// memax-cli is published to npm and @memaxlabs/compiler isn't yet (it is
// published with the open-source launch, plan 25 §12, 3.7). So the CLI
// carries a copy of the one compiler file the daemon runs, the managed
// block, instead of a dependency. This script keeps that copy identical
// to the source: `--check` (part of `pnpm lint`) fails when they differ.
//
//   node scripts/sync-compiler.mjs          # rewrite the copies
//   node scripts/sync-compiler.mjs --check  # fail if a copy is stale
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const cli = join(dirname(fileURLToPath(import.meta.url)), "..");
const compiler = join(cli, "..", "compiler");

const FILES = [
  {
    from: join(compiler, "src", "managed-block.ts"),
    to: join(cli, "src", "lib", "daemon", "compiler", "managed-block.ts"),
    name: "packages/compiler/src/managed-block.ts",
  },
  // memax init strips hidden characters from what it imports exactly as
  // the compiler strips them from what it writes (plan 25 §5.7).
  {
    from: join(compiler, "src", "sanitize.ts"),
    to: join(cli, "src", "lib", "daemon", "compiler", "sanitize.ts"),
    name: "packages/compiler/src/sanitize.ts",
  },
];

const header = (name) =>
  `// Copied verbatim from ${name} by scripts/sync-compiler.mjs.\n` +
  `// Don't edit it here: change the compiler, then run the script.\n\n`;

const check = process.argv.includes("--check");
let stale = 0;
for (const f of FILES) {
  const want = header(f.name) + readFileSync(f.from, "utf8");
  let have = "";
  try {
    have = readFileSync(f.to, "utf8");
  } catch {
    // missing: stale
  }
  if (have === want) continue;
  if (check) {
    console.error(
      `${f.to} is out of date with ${f.name}. Run: node packages/cli/scripts/sync-compiler.mjs`,
    );
    stale++;
  } else {
    writeFileSync(f.to, want);
    console.log(`Copied ${f.name}`);
  }
}
process.exit(stale > 0 ? 1 : 0);
