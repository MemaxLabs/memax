// Copied verbatim from packages/agents/src/index.ts by scripts/sync-agents.mjs.
// Don't edit it here: change @memaxlabs/agents, then run the script.

// @memaxlabs/agents: the coding agents Memax knows, in one place. Each
// agent's name and mark, what shows it's installed, the user-level file
// the Personal space goes into, its session-start hook and its MCP
// settings (./agents.ts); and which of Memax's files each tool loads,
// with the evidence (the load matrix, ./loads.ts). Data only: no
// dependencies, no I/O.
export * from "./types.js";
export {
  AGENTS,
  AGENT_IDS,
  HOME_CANONICAL,
  WINDSURF_GLOBAL_RULES,
  agent,
  agentOf,
  type KnownAgent,
} from "./agents.js";
export {
  READERS,
  REPO_FILES,
  USER_FILES,
  capOf,
  readerMark,
  readerOf,
} from "./loads.js";
