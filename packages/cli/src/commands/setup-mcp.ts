import chalk from "chalk";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname } from "node:path";
import { execSync } from "node:child_process";
import { loadConfig } from "../lib/config.js";
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

  // Codex TOML
  if (agent.format === "toml") {
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
    content += `[mcp_servers.memax]\ntype = "url"\nurl = "${mcpUrl}"\n`;
    writeFileSync(agent.configPath, content);
    return;
  }

  // JSON-based agents — per-agent config shape, no auth header
  writeRemoteJsonConfig(agent, mcpUrl);
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

  // Codex TOML
  if (agent.format === "toml") {
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
    content += `[mcp_servers.memax]\ntype = "url"\nurl = "${mcpUrl}"\n\n[mcp_servers.memax.headers]\nAuthorization = "Bearer ${apiKey}"\n`;
    writeFileSync(agent.configPath, content);
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
  let content = "";
  if (existsSync(agent.configPath)) {
    content = readFileSync(agent.configPath, "utf-8");
  }

  // Remove existing memax section if present
  content = content.replace(
    /\[mcp_servers\.memax(?:\.\w+)*\][\s\S]*?(?=\n\[|$)/g,
    "",
  );

  const args = [...bin.args, "mcp", "serve", "--agent", agent.id]
    .map((a) => `"${a}"`)
    .join(", ");

  content = content.trim();
  if (content) content += "\n\n";
  content += `[mcp_servers.memax]\ncommand = "${bin.command}"\nargs = [${args}]\n`;

  writeFileSync(agent.configPath, content);
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
    console.log(chalk.gray(`\n  [mcp_servers.memax.headers]`));
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

    console.log(
      chalk.gray(
        "\n  All agents authenticate via OAuth when they first connect.\n  For API key mode: memax setup --print --api-key",
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
