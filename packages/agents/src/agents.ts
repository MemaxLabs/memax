// The agents Memax connects, one entry each: what it's called, what shows
// it's installed, the user-level file the Personal space goes into, its
// session-start hook and where it keeps its MCP servers. The files each
// one loads, with the evidence, are the load matrix (./loads.ts).
//
// Hooks were checked against each agent's hook docs (Oct 2026): Claude
// Code and Codex add plain stdout to the context (Codex must not start it
// with { or [; about 2,500 tokens by default, `additionalContextLimit`);
// Gemini CLI wants `hookSpecificOutput.additionalContext`, Cursor
// `additional_context` and Copilot CLI `additionalContext`, as JSON only.
// OpenCode has plugins, not command hooks; Windsurf has no session-start
// event; ChatGPT and Claude (chat) read Memax through their connectors.
//
// Node built-ins only, and none at all: the CLI's session-start hook reads
// this on its critical path (the CLI carries a verbatim copy until this
// package is published).
import type { Agent } from "./types.js";

/** The Personal space's canonical copy, relative to the home folder. */
export const HOME_CANONICAL = ".memax/personal/AGENTS.md";

/** Windsurf's one global rules file, relative to the home folder. */
export const WINDSURF_GLOBAL_RULES =
  ".codeium/windsurf/memories/global_rules.md";

export const AGENTS = [
  {
    id: "claude-code",
    key: "claude-code",
    name: "Claude Code",
    mark: "CC",
    surface: "cli",
    start: "propose",
    cli: true,
    connector: false,
    where: "~/.claude",
    detect: { home: [".claude"], repo: [], commands: ["claude"] },
    home: { kind: "claude_user", path: ".claude/rules/memax-personal.md" },
    hook: {
      file: ".claude/settings.json",
      event: "SessionStart",
      format: "plain",
      maxTokens: 3000,
      loads: ["claude_md", "agents_md"],
      prints: [],
      docs: "https://code.claude.com/docs/en/hooks",
    },
    cloudHook: { file: ".claude/settings.json", event: "SessionStart" },
    mcp: {
      root: "home",
      file: ".claude/settings.json",
      format: "json",
      key: "mcpServers",
    },
  },
  {
    id: "codex",
    key: "codex",
    name: "Codex",
    mark: "CX",
    surface: "cloud",
    start: "propose",
    cli: true,
    connector: false,
    where: "~/.codex",
    detect: { home: [".codex"], repo: [], commands: ["codex"] },
    home: { kind: "codex_user", path: ".codex/AGENTS.md" },
    hook: {
      file: ".codex/hooks.json",
      event: "SessionStart",
      format: "plain",
      maxTokens: 2400,
      loads: ["agents_md"],
      prints: [],
      docs: "https://learn.chatgpt.com/docs/hooks",
    },
    // Codex Cloud runs no session-start hook: it reads the committed AGENTS.md.
    cloudHook: null,
    mcp: {
      root: "home",
      file: ".codex/config.toml",
      format: "toml",
      key: "mcp_servers",
    },
  },
  {
    id: "cursor",
    key: "cursor",
    name: "Cursor",
    mark: "CU",
    surface: "ide",
    start: "read",
    cli: true,
    connector: false,
    where: "~/.cursor",
    detect: {
      home: [".cursor"],
      repo: [".cursor"],
      commands: ["cursor", "cursor-agent"],
    },
    // Its User Rules live in its settings, not a file: its hook prints the
    // canonical copy instead (`prints`).
    home: null,
    hook: {
      file: ".cursor/hooks.json",
      event: "sessionStart",
      format: "cursor",
      maxTokens: 3000,
      loads: ["agents_md"],
      prints: ["agents_md"],
      docs: "https://cursor.com/docs/agent/hooks",
    },
    cloudHook: null,
    mcp: {
      root: "home",
      file: ".cursor/mcp.json",
      format: "json",
      key: "mcpServers",
    },
  },
  {
    id: "gemini-cli",
    key: "gemini",
    name: "Gemini CLI",
    mark: "GM",
    surface: "cli",
    start: "read",
    cli: true,
    connector: false,
    where: "~/.gemini",
    detect: { home: [".gemini"], repo: [], commands: ["gemini"] },
    // Antigravity's file, which Gemini CLI reads once its context.fileName
    // lists AGENTS.md.
    home: { kind: "gemini_user", path: ".gemini/AGENTS.md" },
    hook: {
      file: ".gemini/settings.json",
      event: "SessionStart",
      format: "gemini",
      maxTokens: 3000,
      loads: ["gemini_md", "agents_md"],
      prints: [],
      docs: "https://geminicli.com/docs/hooks/reference/",
    },
    cloudHook: null,
    mcp: {
      root: "home",
      file: ".gemini/settings.json",
      format: "json",
      key: "mcpServers",
    },
  },
  {
    id: "copilot",
    key: "copilot",
    name: "Copilot",
    mark: "CP",
    surface: "ide",
    start: "propose",
    cli: true,
    connector: false,
    where: "~/.copilot",
    detect: { home: [".copilot"], repo: [], commands: ["copilot"] },
    home: {
      kind: "copilot_user",
      path: ".copilot/copilot-instructions.md",
    },
    hook: {
      file: ".copilot/hooks/memax.json",
      event: "sessionStart",
      format: "copilot",
      maxTokens: 3000,
      loads: ["agents_md"],
      prints: [],
      docs: "https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-hooks-reference",
    },
    // Copilot CLI loads a repository's hooks on people's machines too.
    cloudHook: { file: ".github/hooks/memax.json", event: "sessionStart" },
    mcp: {
      root: "home",
      file: ".copilot/mcp-config.json",
      format: "json",
      key: "mcpServers",
    },
  },
  {
    id: "opencode",
    key: "opencode",
    name: "OpenCode",
    mark: "OC",
    surface: "cli",
    start: "propose",
    cli: true,
    connector: false,
    where: "~/.config/opencode",
    detect: {
      home: [".config/opencode"],
      repo: [".opencode"],
      commands: ["opencode"],
    },
    // No file of its own: its `instructions` setting names the canonical copy.
    home: null,
    // Plugins, not command hooks (https://opencode.ai/docs/plugins/).
    hook: null,
    cloudHook: null,
    mcp: {
      root: "repo",
      file: ".opencode/opencode.jsonc",
      format: "json",
      key: "mcp",
    },
  },
  {
    id: "windsurf",
    key: "windsurf",
    name: "Windsurf",
    mark: "WS",
    surface: "ide",
    start: "propose",
    cli: true,
    connector: false,
    where: "~/.codeium/windsurf",
    detect: {
      home: [".codeium/windsurf"],
      repo: [".windsurf"],
      commands: ["windsurf"],
    },
    home: { kind: "windsurf_user", path: WINDSURF_GLOBAL_RULES },
    // No session-start event (https://docs.devin.ai/desktop/cascade/hooks).
    hook: null,
    cloudHook: null,
    mcp: {
      root: "home",
      file: ".codeium/windsurf/mcp_config.json",
      format: "json",
      key: "mcpServers",
    },
  },
  {
    id: "chatgpt",
    key: "chatgpt",
    name: "ChatGPT",
    mark: "GPT",
    surface: "chat",
    start: "propose",
    cli: true,
    connector: true,
    where: "connector",
    detect: { home: [], repo: [], commands: [] },
    home: null,
    hook: null,
    cloudHook: null,
    mcp: null,
  },
  {
    id: "claude",
    key: "claude",
    name: "Claude",
    mark: "CL",
    surface: "chat",
    start: "propose",
    cli: false,
    connector: true,
    where: "connector",
    detect: { home: [], repo: [], commands: [] },
    home: null,
    hook: null,
    cloudHook: null,
    mcp: null,
  },
] as const satisfies readonly Agent[];

/** An agent of the registry, with its literal types. */
export type KnownAgent = (typeof AGENTS)[number];

/** Every agent's id, in the registry's order. */
export const AGENT_IDS = AGENTS.map((a) => a.id);

/** The agent an id or a short key (`gemini`) names, or undefined. */
export function agentOf(idOrKey: string): Agent | undefined {
  return AGENTS.find((a) => a.id === idOrKey || a.key === idOrKey);
}

/** The agent an id names; throws for an id the registry doesn't have. */
export function agent(id: Agent["id"]): Agent {
  const a = AGENTS.find((x) => x.id === id);
  if (!a) throw new Error(`no agent ${id} in the registry`);
  return a;
}
