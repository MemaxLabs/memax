import chalk from "chalk";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname } from "node:path";
import { execSync } from "node:child_process";
import { loadConfig } from "../lib/config.js";
import { confirmDefault } from "../lib/prompt.js";
import { getLocalAgentKey, saveLocalAgentKey } from "../lib/credentials.js";
import { resolveHubID } from "../lib/hubs.js";
import {
  type AgentDef,
  type MemaxBin,
  commandExists,
  resolveMemaxBin,
} from "./setup-types.js";

// --- Remote MCP setup ---

export function getApiUrl(): string {
  return loadConfig().api_url;
}

/**
 * Sets up MCP with OAuth discovery (no API key in config).
 *
 * The MCP client discovers auth endpoints via .well-known/oauth-protected-resource
 * and .well-known/oauth-authorization-server, then completes the OAuth flow in
 * the user's browser when it first connects. Agent identity is auto-detected
 * from the OAuth client_name.
 *
 * Each agent gets its own config shape via agent.remoteEntry() — some use
 * { type: "url" }, some use { type: "http" }, some use { httpUrl }, etc.
 */
export function setupMcpOAuth(agent: AgentDef): void {
  const mcpUrl = `${getApiUrl()}/mcp`;

  // Claude Code uses its own CLI
  if (agent.id === "claude-code") {
    if (!commandExists("claude")) {
      throw new Error("claude CLI not found in PATH");
    }
    try {
      execSync("claude mcp remove memax --scope user", { stdio: "pipe" });
    } catch {
      // Not installed yet
    }
    // Claude Code HTTP transport — no auth header, OAuth auto-discovery
    // --scope user so it's available across all projects
    execSync(`claude mcp add memax --transport http ${mcpUrl} --scope user`, {
      stdio: "pipe",
    });
    return;
  }

  // Hermes YAML — remote schema undocumented, write the stdio form
  // (memax CLI is on PATH: the user is running it right now).
  if (agent.format === "yaml-mcp-servers") {
    writeHermesYamlMcp(agent, { command: "memax", args: [], shell: "" });
    return;
  }

  // Codex TOML
  if (agent.format === "toml") {
    upsertMemaxTomlSection(agent, codexMcpTomlSection(mcpUrl));
    return;
  }

  // JSON-based agents — per-agent config shape, no auth header
  writeRemoteJsonConfig(agent, mcpUrl);
}

/**
 * Renders the [mcp_servers.memax] TOML section for Codex.
 *
 * Codex only reads HTTP headers from a nested `http_headers` table — a plain
 * `headers` table is silently ignored, which leaves the server configured but
 * rejected with 401 on every connection. Verify with `codex mcp list --json`:
 * the key must show up under transport.http_headers.
 */
export function codexMcpTomlSection(mcpUrl: string, apiKey?: string): string {
  let section = `[mcp_servers.memax]\ntype = "url"\nurl = "${mcpUrl}"\n`;
  if (apiKey) {
    section += `\n[mcp_servers.memax.http_headers]\nAuthorization = "Bearer ${apiKey}"\n`;
  }
  return section;
}

/**
 * Replaces the memax section (and any of its sub-tables) in a TOML config,
 * preserving everything else in the file.
 */
export function upsertMemaxTomlSection(agent: AgentDef, section: string): void {
  mkdirSync(dirname(agent.configPath), { recursive: true });
  let content = "";
  if (existsSync(agent.configPath)) {
    content = readFileSync(agent.configPath, "utf-8");
  }
  content = content.replace(
    /\[mcp_servers\.memax(?:\.\w+)*\][\s\S]*?(?=\n\[|$)/g,
    "",
  );
  content = content.trim();
  if (content) content += "\n\n";
  content += section;
  writeFileSync(agent.configPath, content);
}

// --- Codex OAuth login ---
//
// Unlike Claude Code / Cursor / VS Code, Codex does not start the OAuth flow
// when it first connects to a url-type MCP server — it stays "Not logged in"
// until the user runs `codex mcp login <name>`. Setup walks the user through
// that step so the default OAuth mode is usable out of the box.

export type CodexAuthStatus = "ok" | "needs_login" | "unknown";

/** Parses `codex mcp list --json` output into a memax auth status. */
export function parseCodexAuthStatus(json: string): CodexAuthStatus {
  try {
    const servers: unknown = JSON.parse(json);
    if (!Array.isArray(servers)) return "unknown";
    const memax = servers.find(
      (s: unknown) =>
        typeof s === "object" &&
        s !== null &&
        (s as { name?: unknown }).name === "memax",
    );
    if (!memax) return "unknown";
    const auth = (memax as { auth_status?: unknown }).auth_status;
    // Older Codex versions don't report auth_status — can't tell.
    if (typeof auth !== "string" || auth === "") return "unknown";
    // Anything else ("oauth", "bearer_token", ...) means credentials exist.
    return auth === "not_logged_in" ? "needs_login" : "ok";
  } catch {
    return "unknown";
  }
}

export function codexAuthStatus(): CodexAuthStatus {
  if (!commandExists("codex")) return "unknown";
  try {
    // Timeout so a wedged codex (config lock, unexpected prompt) can't
    // hang setup — on timeout we fall through to "unknown".
    const out = execSync("codex mcp list --json", {
      stdio: "pipe",
      timeout: 10000,
    });
    return parseCodexAuthStatus(out.toString("utf-8"));
  } catch {
    return "unknown";
  }
}

function runCodexLogin(): boolean {
  try {
    // Inherit stdio so Codex can print its authorization URL / open a browser.
    execSync("codex mcp login memax", { stdio: "inherit" });
    return true;
  } catch {
    return false;
  }
}

/**
 * Called after OAuth-mode setup configured Codex. Checks login state and, in
 * interactive sessions, offers to run `codex mcp login memax` on the spot.
 * Never fails setup — worst case it prints the command to run manually.
 */
export async function finalizeCodexOAuthLogin(): Promise<void> {
  const status = codexAuthStatus();
  if (status === "ok") return;

  const hint = "codex mcp login memax";
  if (status === "unknown") {
    console.log(
      chalk.gray(
        `\n  Codex CLI: if Memax tools don't load, authorize with: ${hint}`,
      ),
    );
    return;
  }

  const interactive =
    process.stdin.isTTY === true && process.stdout.isTTY === true;
  if (!interactive) {
    console.log(chalk.yellow("\n  Codex CLI is not logged in to Memax yet."));
    console.log(chalk.gray(`  Run: ${hint}`));
    return;
  }

  console.log(
    chalk.white("\n  Codex CLI needs a one-time OAuth login to connect."),
  );
  const proceed = await confirmDefault(
    "  Log in now (opens your browser)? [Y/n] ",
  );
  if (!proceed) {
    console.log(chalk.gray(`  Skipped — run later: ${hint}`));
    return;
  }
  if (runCodexLogin() && codexAuthStatus() !== "needs_login") {
    console.log(chalk.green("  ✓ Codex CLI logged in to Memax"));
  } else {
    console.log(chalk.yellow(`  Login didn't complete — run: ${hint}`));
  }
}

export async function ensureApiKey(
  hubId?: string,
  agentName?: string,
  opts: {
    readOnly?: boolean;
    allowDelete?: boolean;
    allowOrganize?: boolean;
    agentSync?: boolean;
  } = {},
): Promise<string | undefined> {
  try {
    const resolvedHubID = hubId ? await resolveHubID(hubId) : undefined;
    if (hubId && !resolvedHubID) {
      return undefined;
    }
    if (resolvedHubID && opts.agentSync) {
      return undefined;
    }
    const { getClient } = await import("../lib/client.js");
    const name = agentName
      ? `mcp-${agentName}${resolvedHubID ? `-hub-${resolvedHubID.slice(0, 8)}` : ""}`
      : resolvedHubID
        ? `mcp-setup-hub-${resolvedHubID.slice(0, 8)}`
        : "mcp-setup";
    const scopes = ["read"];
    if (!opts.readOnly) scopes.push("write");
    if (opts.allowOrganize) scopes.push("organize");
    if (opts.allowDelete) scopes.push("delete");
    if (opts.agentSync) scopes.push("agent-sync");
    const result = await getClient().auth.createKey({
      name,
      hubId: resolvedHubID,
      agentName: agentName || undefined,
      expiresInDays: 90,
      scopes,
      trustLevel: resolvedHubID ? "standard" : "elevated",
    });
    return result.key;
  } catch {
    return undefined;
  }
}

export async function ensureLocalAgentKey(
  agentName: string,
): Promise<string | undefined> {
  const existing = getLocalAgentKey(agentName);
  if (existing) {
    return existing;
  }
  const created = await ensureApiKey(undefined, agentName);
  if (!created) {
    return undefined;
  }
  console.log(
    chalk.yellow(
      `  Creating new local key for ${agentName}. If you had a previous key, revoke it from Memax Settings.`,
    ),
  );
  saveLocalAgentKey(agentName, created);
  return created;
}

export function setupMcpRemote(agent: AgentDef, apiKey: string): void {
  const mcpUrl = `${getApiUrl()}/mcp`;
  const authHeaders = { Authorization: `Bearer ${apiKey}` };

  // Claude Code uses its own CLI
  if (agent.id === "claude-code") {
    if (!commandExists("claude")) {
      throw new Error("claude CLI not found in PATH");
    }
    try {
      execSync("claude mcp remove memax --scope user", { stdio: "pipe" });
    } catch {
      // Not installed yet
    }
    // --scope user so it's available across all projects
    execSync(
      `claude mcp add memax --transport http ${mcpUrl} --header "Authorization: Bearer ${apiKey}" --scope user`,
      { stdio: "pipe" },
    );
    return;
  }

  // Hermes YAML — remote schema undocumented, write the stdio form
  // (memax CLI is on PATH: the user is running it right now).
  if (agent.format === "yaml-mcp-servers") {
    writeHermesYamlMcp(agent, { command: "memax", args: [], shell: "" });
    return;
  }

  // Codex TOML
  if (agent.format === "toml") {
    upsertMemaxTomlSection(agent, codexMcpTomlSection(mcpUrl, apiKey));
    return;
  }

  // JSON-based agents — per-agent config shape with auth headers
  writeRemoteJsonConfig(agent, mcpUrl, authHeaders);
}

/**
 * Shared JSON config writer for remote MCP. Uses agent.remoteEntry() to
 * produce the correct schema shape for each agent.
 */
function writeRemoteJsonConfig(
  agent: AgentDef,
  mcpUrl: string,
  authHeaders?: Record<string, string>,
): void {
  mkdirSync(dirname(agent.configPath), { recursive: true });
  let config: Record<string, unknown> = {};
  if (existsSync(agent.configPath)) {
    try {
      config = JSON.parse(readFileSync(agent.configPath, "utf-8"));
    } catch {
      // Start fresh
    }
  }

  const servers = (getNestedKey(config, agent.mcpKey) ?? {}) as Record<
    string,
    unknown
  >;
  servers.memax = agent.remoteEntry(mcpUrl, authHeaders);
  setNestedKey(config, agent.mcpKey, servers);
  writeFileSync(agent.configPath, JSON.stringify(config, null, 2) + "\n");
}

/**
 * Upsert the memax entry under a root-level `mcp_servers:` block in a YAML
 * config, editing line-wise instead of pulling in a YAML dependency for one
 * agent (Hermes). Preserves everything else in the file — other servers,
 * comments, unrelated top-level keys — and adapts to the file's existing
 * child indentation (2 spaces, 4 spaces, tabs).
 *
 * Returns null when the file cannot be edited safely (flow-style
 * `mcp_servers: {...}`) — callers must fall back to manual instructions
 * rather than corrupt the config.
 */
export function upsertYamlMcpServersBlock(
  content: string,
  entry: { command: string; args: string[] },
): string | null {
  const renderEntry = (childIndent: string): string[] => {
    const grandchildIndent = childIndent + childIndent;
    return [
      `${childIndent}memax:`,
      `${grandchildIndent}command: "${entry.command}"`,
      `${grandchildIndent}args: [${entry.args.map((a) => `"${a}"`).join(", ")}]`,
    ];
  };

  const lines = content.length > 0 ? content.split("\n") : [];
  const isRootKey = (line: string) => /^\S/.test(line);

  // Flow-style `mcp_servers: {...}` (or any inline value) — bail out.
  if (
    lines.some(
      (l) => /^mcp_servers:\s*\S/.test(l) && !/^mcp_servers:\s*#/.test(l),
    )
  ) {
    return null;
  }

  const rootIdx = lines.findIndex((l) => /^mcp_servers:\s*(#.*)?$/.test(l));
  if (rootIdx === -1) {
    const out = [...lines];
    while (out.length > 0 && out[out.length - 1].trim() === "") out.pop();
    if (out.length > 0) out.push("");
    out.push("mcp_servers:", ...renderEntry("  "), "");
    return out.join("\n");
  }

  // Bounds of the mcp_servers block: up to the next root-level key.
  let blockEnd = lines.length;
  for (let i = rootIdx + 1; i < lines.length; i++) {
    if (lines[i].trim() !== "" && isRootKey(lines[i])) {
      blockEnd = i;
      break;
    }
  }

  // Adopt the block's existing child indentation so we never mix widths.
  // Comment lines are skipped — a `  # note` above 4-space children would
  // otherwise poison the detected width and duplicate the memax entry.
  let childIndent = "  ";
  for (let i = rootIdx + 1; i < blockEnd; i++) {
    const trimmed = lines[i].trim();
    if (trimmed === "" || trimmed.startsWith("#")) continue;
    childIndent = lines[i].match(/^\s*/)?.[0] ?? "  ";
    break;
  }

  // Remove an existing memax sub-block (its line + all deeper-indented
  // or blank lines that follow it).
  const kept: string[] = [];
  let skipping = false;
  for (let i = rootIdx + 1; i < blockEnd; i++) {
    const line = lines[i];
    const rest = line.startsWith(`${childIndent}memax:`)
      ? line.slice(childIndent.length + "memax:".length).trim()
      : null;
    if (rest !== null && (rest === "" || rest.startsWith("#"))) {
      skipping = true;
      continue;
    }
    if (skipping) {
      const indent = line.match(/^\s*/)?.[0] ?? "";
      if (line.trim() === "" || indent.length > childIndent.length) continue;
      skipping = false;
    }
    kept.push(line);
  }

  return [
    ...lines.slice(0, rootIdx + 1),
    ...renderEntry(childIndent),
    ...kept,
    ...lines.slice(blockEnd),
  ].join("\n");
}

/**
 * Hermes MCP config (~/.hermes/config.yaml). Hermes documents stdio server
 * entries (command/args) under `mcp_servers`; its remote/SSE YAML schema is
 * not documented, so BOTH setup modes write the stdio form — the local CLI
 * (`memax mcp serve`) reaches the same data once the user is logged in.
 */
function writeHermesYamlMcp(agent: AgentDef, bin: MemaxBin): void {
  mkdirSync(dirname(agent.configPath), { recursive: true });
  const entry = {
    command: bin.command,
    args: [...bin.args, "mcp", "serve", "--agent", agent.id],
  };
  let content = "";
  if (existsSync(agent.configPath)) {
    content = readFileSync(agent.configPath, "utf-8");
  }
  const updated = upsertYamlMcpServersBlock(content, entry);
  if (updated === null) {
    // Flow-style mcp_servers — editing would corrupt the file. Tell the
    // user exactly what to add instead of guessing.
    console.log(
      chalk.yellow(
        `  ${agent.configPath} uses an inline mcp_servers mapping — add this entry manually:`,
      ),
    );
    console.log(
      chalk.gray(
        `    memax: { command: "${entry.command}", args: [${entry.args
          .map((a) => `"${a}"`)
          .join(", ")}] }\n`,
      ),
    );
    return;
  }
  writeFileSync(agent.configPath, updated);
}

// --- Local MCP setup per agent ---

export function setupMcp(agent: AgentDef, bin: MemaxBin): void {
  // Claude Code has its own CLI for MCP management
  if (agent.id === "claude-code") {
    setupMcpClaudeCode(bin);
    return;
  }

  mkdirSync(dirname(agent.configPath), { recursive: true });

  if (agent.format === "toml") {
    setupMcpToml(agent, bin);
    return;
  }

  if (agent.format === "yaml-mcp-servers") {
    writeHermesYamlMcp(agent, bin);
    return;
  }

  // JSON-based agents
  let config: Record<string, unknown> = {};
  if (existsSync(agent.configPath)) {
    try {
      config = JSON.parse(readFileSync(agent.configPath, "utf-8"));
    } catch {
      // Start fresh
    }
  }

  const servers = (getNestedKey(config, agent.mcpKey) ?? {}) as Record<
    string,
    unknown
  >;
  const allArgs = [...bin.args, "mcp", "serve", "--agent", agent.id];
  servers.memax = agent.localEntry
    ? agent.localEntry(bin.command, allArgs)
    : { command: bin.command, args: allArgs };
  setNestedKey(config, agent.mcpKey, servers);

  writeFileSync(agent.configPath, JSON.stringify(config, null, 2) + "\n");
}

export function setupMcpClaudeCode(bin: MemaxBin): void {
  // Claude Code uses its own CLI for MCP — settings.json mcpServers is ignored
  if (!commandExists("claude")) {
    throw new Error("claude CLI not found in PATH");
  }

  // Remove existing first (idempotent)
  try {
    execSync("claude mcp remove memax --scope user", { stdio: "pipe" });
  } catch {
    // Not installed yet — fine
  }

  // claude mcp add <name> -- <command> [args...]
  // --scope user so it's available across all projects
  const allArgs = [...bin.args, "mcp", "serve", "--agent", "claude-code"];
  const cmd = `claude mcp add memax --scope user -- ${bin.command} ${allArgs.join(" ")}`;

  try {
    execSync(cmd, { stdio: "pipe" });
  } catch (err) {
    throw new Error(`claude mcp add failed: ${(err as Error).message}`);
  }
}

export function setupMcpToml(agent: AgentDef, bin: MemaxBin): void {
  // Codex uses TOML — append or update the memax section
  const args = [...bin.args, "mcp", "serve", "--agent", agent.id]
    .map((a) => `"${a}"`)
    .join(", ");
  upsertMemaxTomlSection(
    agent,
    `[mcp_servers.memax]\ncommand = "${bin.command}"\nargs = [${args}]\n`,
  );
}

export async function printMcpConfigs(opts: {
  local: boolean;
  apiKey: boolean;
  hub?: string;
  readOnly?: boolean;
  allowDelete?: boolean;
  allowOrganize?: boolean;
  agentSync?: boolean;
}): Promise<void> {
  const mcpUrl = `${getApiUrl()}/mcp`;
  const indent = (json: unknown) =>
    JSON.stringify(json, null, 2)
      .split("\n")
      .map((l) => "  " + l)
      .join("\n");

  console.log(chalk.bold("\n  Memax MCP Configuration\n"));

  if (opts.local) {
    const bin = resolveMemaxBin();
    const cmd = bin ? bin.command : "memax";
    const baseArgs = bin ? [...bin.args, "mcp", "serve"] : ["mcp", "serve"];

    console.log(chalk.gray("  Mode: local (stdio)\n"));
    console.log(
      chalk.gray(
        "  Note: add --agent <name> for agent identity attribution.\n" +
          "  Example: memax mcp serve --agent claude-code\n",
      ),
    );
    // Include --agent placeholder so manual setups get attribution
    const args = [...baseArgs, "--agent", "<agent-id>"];

    console.log(
      chalk.white("  For most agents (Claude Code, Cursor, Gemini, etc.):\n"),
    );
    console.log(indent({ mcpServers: { memax: { command: cmd, args } } }));

    console.log(chalk.white("\n  For Copilot CLI:\n"));
    console.log(
      indent({
        mcpServers: {
          memax: { command: cmd, args, tools: [{ type: "function" }] },
        },
      }),
    );

    console.log(chalk.white("\n  For VS Code (.vscode/mcp.json):\n"));
    console.log(indent({ servers: { memax: { command: cmd, args } } }));

    console.log(chalk.white("\n  For OpenCode:\n"));
    console.log(
      indent({ mcp: { memax: { type: "local", command: cmd, args } } }),
    );
  } else if (opts.apiKey) {
    let apiKey: string | undefined;
    try {
      apiKey = await ensureApiKey(opts.hub, undefined, {
        readOnly: opts.readOnly,
        allowDelete: opts.allowDelete,
        allowOrganize: opts.allowOrganize,
        agentSync: opts.agentSync,
      });
    } catch {
      // Not logged in
    }
    const keyDisplay = apiKey ?? "mxk_your_api_key_here";
    const authHeaders = { Authorization: `Bearer ${keyDisplay}` };

    console.log(chalk.gray("  Mode: remote server (API key)\n"));

    console.log(chalk.white("  For Claude Code:\n"));
    console.log(
      chalk.gray(
        `  claude mcp add memax --transport http ${mcpUrl} --header "Authorization: Bearer ${keyDisplay}" --scope user`,
      ),
    );

    console.log(chalk.white("\n  For Cursor, Windsurf:\n"));
    console.log(
      indent({
        mcpServers: {
          memax: { type: "url", url: mcpUrl, headers: authHeaders },
        },
      }),
    );

    console.log(chalk.white("\n  For Gemini CLI:\n"));
    console.log(
      indent({
        mcpServers: { memax: { httpUrl: mcpUrl, headers: authHeaders } },
      }),
    );

    console.log(chalk.white("\n  For Copilot CLI:\n"));
    console.log(
      indent({
        mcpServers: {
          memax: {
            type: "http",
            url: mcpUrl,
            tools: [{ type: "function" }],
            headers: authHeaders,
          },
        },
      }),
    );

    console.log(chalk.white("\n  For VS Code (.vscode/mcp.json):\n"));
    console.log(
      indent({
        servers: { memax: { type: "http", url: mcpUrl, headers: authHeaders } },
      }),
    );

    console.log(chalk.white("\n  For Codex CLI (~/.codex/config.toml):\n"));
    console.log(chalk.gray(`  [mcp_servers.memax]`));
    console.log(chalk.gray(`  type = "url"`));
    console.log(chalk.gray(`  url = "${mcpUrl}"`));
    console.log(chalk.gray(`\n  [mcp_servers.memax.http_headers]`));
    console.log(chalk.gray(`  Authorization = "Bearer ${keyDisplay}"`));

    if (apiKey) {
      console.log(chalk.yellow("\n  API key created: mcp-setup"));
    } else {
      console.log(
        chalk.yellow(
          "\n  Not logged in — replace mxk_your_api_key_here with a real key.",
        ),
      );
      console.log(
        chalk.gray("  Run: memax login && memax auth create-key --name mcp"),
      );
    }
  } else {
    console.log(chalk.gray("  Mode: remote server (OAuth — recommended)\n"));

    console.log(chalk.white("  For Claude Code:\n"));
    console.log(
      chalk.gray(
        `  claude mcp add memax --transport http ${mcpUrl} --scope user`,
      ),
    );
    console.log(
      chalk.gray("  (OAuth auto-discovery — authenticates via browser)\n"),
    );

    console.log(chalk.white("  For Cursor, Windsurf:\n"));
    console.log(
      indent({ mcpServers: { memax: { type: "url", url: mcpUrl } } }),
    );

    console.log(chalk.white("\n  For Gemini CLI:\n"));
    console.log(indent({ mcpServers: { memax: { httpUrl: mcpUrl } } }));

    console.log(chalk.white("\n  For Copilot CLI:\n"));
    console.log(
      indent({
        mcpServers: {
          memax: { type: "http", url: mcpUrl, tools: [{ type: "function" }] },
        },
      }),
    );

    console.log(chalk.white("\n  For VS Code (.vscode/mcp.json):\n"));
    console.log(indent({ servers: { memax: { type: "http", url: mcpUrl } } }));

    console.log(chalk.white("\n  For Codex CLI (~/.codex/config.toml):\n"));
    console.log(chalk.gray(`  [mcp_servers.memax]`));
    console.log(chalk.gray(`  type = "url"`));
    console.log(chalk.gray(`  url = "${mcpUrl}"`));
    console.log(chalk.gray(`\n  Then authorize: codex mcp login memax`));

    console.log(
      chalk.gray(
        "\n  Most agents authenticate via OAuth when they first connect;\n  Codex requires the explicit login above.\n  For API key mode: memax setup --print --api-key",
      ),
    );
  }

  console.log();
}

// --- Teardown helpers ---

export function removeMcpJson(agent: AgentDef): boolean {
  if (!existsSync(agent.configPath)) return false;

  try {
    const config = JSON.parse(readFileSync(agent.configPath, "utf-8"));
    const servers = getNestedKey(config, agent.mcpKey);
    if (!servers?.memax) return false;

    delete servers.memax;
    if (Object.keys(servers).length === 0)
      deleteNestedKey(config, agent.mcpKey);

    writeFileSync(agent.configPath, JSON.stringify(config, null, 2) + "\n");
    console.log(chalk.gray(`  Removed MCP from ${agent.name}`));
    return true;
  } catch {
    return false;
  }
}

export function removeMcpToml(agent: AgentDef): boolean {
  if (!existsSync(agent.configPath)) return false;

  let content = readFileSync(agent.configPath, "utf-8");
  const before = content;
  content = content.replace(
    /\[mcp_servers\.memax(?:\.\w+)*\][\s\S]*?(?=\n\[|$)/g,
    "",
  );

  if (content === before) return false;

  writeFileSync(agent.configPath, content.trim() + "\n");
  console.log(chalk.gray(`  Removed MCP from ${agent.name}`));
  return true;
}

// --- Nested key helpers for configs like openclaw's "mcp.servers" ---

export function getNestedKey(
  obj: Record<string, unknown>,
  key: string,
): Record<string, unknown> | undefined {
  const parts = key.split(".");
  let current: unknown = obj;
  for (const part of parts) {
    if (current == null || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current as Record<string, unknown> | undefined;
}

export function setNestedKey(
  obj: Record<string, unknown>,
  key: string,
  value: unknown,
): void {
  const parts = key.split(".");
  let current: Record<string, unknown> = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    if (!(parts[i] in current) || typeof current[parts[i]] !== "object") {
      current[parts[i]] = {};
    }
    current = current[parts[i]] as Record<string, unknown>;
  }
  current[parts[parts.length - 1]] = value;
}

export function deleteNestedKey(
  obj: Record<string, unknown>,
  key: string,
): void {
  const parts = key.split(".");
  let current: Record<string, unknown> = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    if (!(parts[i] in current) || typeof current[parts[i]] !== "object") return;
    current = current[parts[i]] as Record<string, unknown>;
  }
  delete current[parts[parts.length - 1]];
}
