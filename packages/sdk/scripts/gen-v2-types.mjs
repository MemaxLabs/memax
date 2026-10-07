#!/usr/bin/env node
// Generates src/v2/schema.gen.ts from the /v2 contract,
// packages/server/openapi/v2.yaml, with openapi-typescript.
//
//   node scripts/gen-v2-types.mjs           write the file (pnpm gen:v2)
//   node scripts/gen-v2-types.mjs --check   fail if the committed file is stale
//
// The root `pnpm lint` runs the check (scripts/check-v2-types.mjs), so the
// spec and the SDK can't drift: change v2.yaml, run `pnpm --filter
// memax-sdk gen:v2`, and commit both.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";
import openapiTS, { astToString } from "openapi-typescript";

const here = dirname(fileURLToPath(import.meta.url));
export const SPEC = resolve(here, "../../server/openapi/v2.yaml");
export const OUTPUT = resolve(here, "../src/v2/schema.gen.ts");

const BANNER = `// SPDX-License-Identifier: Apache-2.0
//
// Generated from packages/server/openapi/v2.yaml by
// \`pnpm --filter memax-sdk gen:v2\`. Do not edit: change the spec and
// regenerate. \`pnpm lint\` fails when this file is stale.

`;

export async function generate() {
  const ast = await openapiTS(pathToFileURL(SPEC), { silent: true });
  return BANNER + astToString(ast);
}

export async function check() {
  const fresh = await generate();
  let committed = "";
  try {
    committed = readFileSync(OUTPUT, "utf-8");
  } catch {
    // missing counts as stale
  }
  return committed === fresh;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const rel = relative(process.cwd(), OUTPUT);
  if (process.argv.includes("--check")) {
    if (!(await check())) {
      console.error(
        `${rel} is out of date with packages/server/openapi/v2.yaml.\nRun: pnpm --filter memax-sdk gen:v2`,
      );
      process.exit(1);
    }
    console.log(`${rel} matches the /v2 spec.`);
  } else {
    writeFileSync(OUTPUT, await generate());
    console.log(`Wrote ${rel}.`);
  }
}
