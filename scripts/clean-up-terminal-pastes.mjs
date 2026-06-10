#!/usr/bin/env node

import { access, readdir, stat, unlink } from "node:fs/promises";
import { join, resolve } from "node:path";

const DIR = resolve(import.meta.dirname, "../.tmp/terminal-pastes");
const KEEP = 5;

try {
  await access(DIR);
} catch {
  console.log(`${DIR} does not exist, nothing to clean.`);
  process.exit(0);
}

const entries = await readdir(DIR);
const files = await Promise.all(
  entries.map(async (name) => {
    const path = join(DIR, name);
    const s = await stat(path);
    return s.isFile() ? { path, name, birthtime: s.birthtimeMs } : null;
  }),
);

const sorted = files.filter(Boolean).sort((a, b) => b.birthtime - a.birthtime);
const toDelete = sorted.slice(KEEP);

await Promise.all(toDelete.map((f) => unlink(f.path)));

if (toDelete.length) {
  console.log(`Deleted ${toDelete.length} file(s), kept ${KEEP} newest.`);
} else {
  console.log(`Nothing to delete (${sorted.length} file(s) present).`);
}
