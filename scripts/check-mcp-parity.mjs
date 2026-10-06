#!/usr/bin/env node
/**
 * MCP parity check — the Go remote MCP server and the CLI's local stdio
 * MCP server must expose the same tools: the same names, titles,
 * descriptions, input schemas, output schemas and annotations, and the
 * same server instructions. The rule lived only in AGENTS.md and was
 * violated twice (memax_topics + hint/project_context missing on one
 * side; the source_agent schema divergence that silently lost claude.ai
 * pushes), so it is a lint failure.
 *
 * Both sides are data, not prose to scrape:
 *   - Go: packages/server/internal/handler/mcp_tools.json (embedded and
 *     served by mcp_catalog.go), profiles "agent" and "chatgpt".
 *   - CLI: packages/cli/src/commands/mcp-tools.ts, imported here with
 *     Node's type stripping (erasable TypeScript only).
 * The comparison is on the served form: "#item" references expanded, and
 * output schemas that name another tool resolved, as both servers do.
 *
 * Known asymmetries:
 *   - The ChatGPT profile (search_memories, save_memory, …) is remote
 *     only. Each of its tools must alias an agent-profile tool.
 *   - source_agent: parsed by the Go server for the API-key claim path
 *     but deliberately NOT advertised in either schema.
 */

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual } from "node:util";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const goCatalogPath = resolve(
  root,
  "packages/server/internal/handler/mcp_tools.json",
);
const cliCatalogPath = resolve(root, "packages/cli/src/commands/mcp-tools.ts");
const cliServerPath = resolve(root, "packages/cli/src/commands/mcp.ts");

let failed = false;
function fail(msg) {
  console.error(`✖ mcp-parity: ${msg}`);
  failed = true;
}

/** Replace {"$ref": "#item"} with the item schema, as the servers do. */
function expand(schema, item) {
  if (Array.isArray(schema)) return schema.map((s) => expand(s, item));
  if (schema && typeof schema === "object") {
    const keys = Object.keys(schema);
    if (keys.length === 1 && schema.$ref === "#item") return item;
    return Object.fromEntries(
      Object.entries(schema).map(([k, v]) => [k, expand(v, item)]),
    );
  }
  return schema;
}

function servedGoProfiles(catalog) {
  const agentOutputs = new Map();
  const out = {};
  for (const name of ["agent", "chatgpt"]) {
    const profile = catalog.profiles?.[name];
    if (!profile) {
      fail(`mcp_tools.json has no "${name}" profile`);
      continue;
    }
    out[name] = {
      ...profile,
      tools: profile.tools.map((t) => {
        let outputSchema = t.outputSchema;
        if (typeof outputSchema === "string") {
          outputSchema = agentOutputs.get(outputSchema);
          if (!outputSchema)
            fail(
              `${t.name} names output schema "${t.outputSchema}", which isn't defined`,
            );
        }
        if (outputSchema) outputSchema = expand(outputSchema, catalog.item);
        if (name === "agent" && outputSchema)
          agentOutputs.set(t.name, outputSchema);
        return { ...t, outputSchema, canonical: t.canonical ?? t.name };
      }),
    };
  }
  return out;
}

// --- Load both sides ---

const goCatalog = JSON.parse(readFileSync(goCatalogPath, "utf8"));
const go = servedGoProfiles(goCatalog);
const cli = await import(pathToFileURL(cliCatalogPath).href);
const cliTools = cli.servedTools();
const cliServer = readFileSync(cliServerPath, "utf8");

if (!go.agent?.tools?.length)
  fail("extracted ZERO agent tools from mcp_tools.json");
if (!cliTools.length) fail("extracted ZERO tools from the CLI's mcp-tools.ts");

// The CLI server must serve the catalogue, not a copy of its own.
for (const symbol of ["servedTools()", "MCP_INSTRUCTIONS"]) {
  if (!cliServer.includes(symbol)) {
    fail(
      `packages/cli/src/commands/mcp.ts doesn't use ${symbol} from mcp-tools.ts`,
    );
  }
}

// --- Compare the agent profile with the CLI, field by field ---

const fields = [
  "title",
  "description",
  "inputSchema",
  "outputSchema",
  "annotations",
];
const goByName = new Map(go.agent.tools.map((t) => [t.name, t]));
const cliByName = new Map(cliTools.map((t) => [t.name, t]));

for (const [name, goTool] of goByName) {
  const cliTool = cliByName.get(name);
  if (!cliTool) {
    fail(
      `tool ${name} exists in the Go remote MCP but not in the CLI local MCP`,
    );
    continue;
  }
  for (const field of fields) {
    if (!isDeepStrictEqual(goTool[field] ?? null, cliTool[field] ?? null)) {
      fail(
        `tool ${name}: ${field} differs\n    go:  ${JSON.stringify(goTool[field])}\n    cli: ${JSON.stringify(cliTool[field])}`,
      );
    }
  }
  if (goTool.inputSchema?.properties?.source_agent) {
    fail(`tool ${name} advertises source_agent; the grant carries the agent`);
  }
}
for (const name of cliByName.keys()) {
  if (!goByName.has(name))
    fail(
      `tool ${name} exists in the CLI local MCP but not in the Go remote MCP`,
    );
}
if (go.agent.instructions !== cli.MCP_INSTRUCTIONS)
  fail(
    "the server instructions differ between mcp_tools.json and mcp-tools.ts",
  );
if (go.agent.server !== cli.MCP_SERVER_NAME)
  fail("the server name differs between mcp_tools.json and mcp-tools.ts");

// --- The ChatGPT profile aliases agent tools ---

for (const t of go.chatgpt?.tools ?? []) {
  if (!goByName.has(t.canonical))
    fail(
      `ChatGPT tool ${t.name} aliases ${t.canonical}, which the agent profile doesn't have`,
    );
  if (!t.title || !t.annotations || t.annotations.openWorldHint !== false)
    fail(`ChatGPT tool ${t.name} needs a title and openWorldHint: false`);
}

if (failed) {
  console.error(
    "\nMCP tool surfaces have drifted. Change BOTH packages/server/internal/handler/mcp_tools.json and packages/cli/src/commands/mcp-tools.ts in the same commit (AGENTS.md: MCP Tool Parity).",
  );
  process.exit(1);
}
console.log(
  `✓ mcp-parity: ${goByName.size} tools match across the Go remote and CLI local MCP (names, titles, descriptions, input and output schemas, annotations); ${go.chatgpt.tools.length} ChatGPT aliases map onto them`,
);
