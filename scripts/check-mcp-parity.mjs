#!/usr/bin/env node
/**
 * MCP parity check (F4) — the Go remote MCP and the CLI local MCP must
 * expose the same canonical memax_* tools with the same parameter
 * sets. This rule lived only in AGENTS.md and was violated twice
 * (memax_topics + hint/project_context missing on one side; the
 * source_agent schema divergence that silently lost claude.ai pushes).
 * A rule that only exists as prose is a rule that gets violated —
 * this script makes the drift a lint failure.
 *
 * Method: extract tool name → property-key set from both files.
 *   - Go: InputSchema is a JSON string literal — parsed properly.
 *   - CLI: inputSchema is an object literal — brace-scanned for the
 *     `properties` block's top-level keys. Fragile by construction,
 *     so the script FAILS LOUDLY if it extracts zero tools from
 *     either side (a refactor that breaks extraction must break lint,
 *     not silently pass).
 *
 * Known asymmetries (allow-listed, with reasons):
 *   - Go additionally exposes ChatGPT-alias names (search_memories,
 *     save_memory, …) — connector requirement, remote-only.
 *   - source_agent: parsed by the Go server for the API-key TOFU
 *     claim path but deliberately NOT advertised in either schema.
 */

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const goSrc = readFileSync(
  resolve(root, "packages/server/internal/handler/mcp.go"),
  "utf8",
);
const cliSrc = readFileSync(
  resolve(root, "packages/cli/src/commands/mcp.ts"),
  "utf8",
);

/** Go side: `Name: "memax_x"` … `InputSchema: json.RawMessage(`{…}`)` */
function extractGoTools(src) {
  const tools = new Map();
  const re =
    /Name:\s+"(memax_[a-z_]+)",[\s\S]*?InputSchema:\s*json\.RawMessage\(`([\s\S]*?)`\)/g;
  for (const m of src.matchAll(re)) {
    const [, name, schemaText] = m;
    let props = [];
    try {
      const schema = JSON.parse(schemaText);
      props = Object.keys(schema.properties ?? {});
    } catch (err) {
      fail(`Go InputSchema for ${name} is not valid JSON: ${err.message}`);
    }
    tools.set(name, new Set(props));
  }
  return tools;
}

/** CLI side: `name: "memax_x"` … `inputSchema: { … properties: { … } }`
 *  via brace scanning from the properties block. */
function extractCliTools(src) {
  const tools = new Map();
  const nameRe = /name:\s*"(memax_[a-z_]+)"/g;
  const nameHits = [...src.matchAll(nameRe)];
  for (let i = 0; i < nameHits.length; i++) {
    const name = nameHits[i][1];
    const start = nameHits[i].index;
    const end = i + 1 < nameHits.length ? nameHits[i + 1].index : src.length;
    let block = src.slice(start, end);
    // Indirection: `inputSchema: someSharedSchema` — resolve the
    // identifier to its `const someSharedSchema = {...}` definition
    // and scan that instead of the (absent) inline object.
    const indirect = block.match(/inputSchema:\s*([A-Za-z_$][\w$]*)\s*[,}]/);
    if (indirect) {
      const defRe = new RegExp(`const\\s+${indirect[1]}\\s*=`);
      const defMatch = src.match(defRe);
      if (!defMatch) {
        fail(`CLI tool ${name}: inputSchema references ${indirect[1]} but no const definition found`);
        tools.set(name, new Set());
        continue;
      }
      block = src.slice(defMatch.index, defMatch.index + 4000);
    }
    const propsIdx = block.indexOf("properties:");
    if (propsIdx === -1) {
      tools.set(name, new Set());
      continue;
    }
    const open = block.indexOf("{", propsIdx);
    let depth = 0;
    let close = open;
    for (let j = open; j < block.length; j++) {
      if (block[j] === "{") depth++;
      else if (block[j] === "}") {
        depth--;
        if (depth === 0) {
          close = j;
          break;
        }
      }
    }
    const propsBlock = block.slice(open + 1, close);
    // Top-level keys of the properties object = identifiers followed
    // by ":" at brace depth 0 within the block.
    const keys = new Set();
    depth = 0;
    for (const line of splitTopLevel(propsBlock)) {
      const km = line.match(/^\s*(\w+)\s*:/);
      if (km) keys.add(km[1]);
    }
    tools.set(name, keys);
  }
  return tools;
}

/** Split an object body into top-level entries by tracking depth. */
function splitTopLevel(body) {
  const entries = [];
  let depth = 0;
  let current = "";
  for (const ch of body) {
    if (ch === "{" || ch === "[" || ch === "(") depth++;
    if (ch === "}" || ch === "]" || ch === ")") depth--;
    if (ch === "," && depth === 0) {
      entries.push(current);
      current = "";
    } else {
      current += ch;
    }
  }
  if (current.trim()) entries.push(current);
  return entries;
}

let failed = false;
function fail(msg) {
  console.error(`✖ mcp-parity: ${msg}`);
  failed = true;
}

const goTools = extractGoTools(goSrc);
const cliTools = extractCliTools(cliSrc);

// Extraction sanity — a refactor that breaks parsing must break lint.
if (goTools.size === 0) fail("extracted ZERO tools from Go mcp.go — extractor broken?");
if (cliTools.size === 0) fail("extracted ZERO tools from CLI mcp.ts — extractor broken?");

for (const [name, goProps] of goTools) {
  if (!cliTools.has(name)) {
    fail(`tool ${name} exists in Go remote MCP but not in CLI local MCP`);
    continue;
  }
  const cliProps = cliTools.get(name);
  for (const p of goProps) {
    if (!cliProps.has(p)) {
      fail(`tool ${name}: param "${p}" in Go but missing in CLI`);
    }
  }
  for (const p of cliProps) {
    if (!goProps.has(p)) {
      fail(`tool ${name}: param "${p}" in CLI but missing in Go`);
    }
  }
}
for (const name of cliTools.keys()) {
  if (!goTools.has(name)) {
    fail(`tool ${name} exists in CLI local MCP but not in Go remote MCP`);
  }
}

if (failed) {
  console.error(
    "\nMCP tool surfaces have drifted. Fix BOTH packages/server/internal/handler/mcp.go and packages/cli/src/commands/mcp.ts in the same commit (AGENTS.md: MCP Tool Parity).",
  );
  process.exit(1);
}
console.log(
  `✓ mcp-parity: ${goTools.size} canonical tools match across Go remote and CLI local MCP`,
);
