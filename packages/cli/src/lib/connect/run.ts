// memax connect <agent> (plan 25 §7.2): one agent, everything it needs to
// share this repository's context, and safe to run again.
//
// 1. MCP: write its MCP settings for OAuth, as init does (left alone when
//    they already name Memax, or when the Memax plugin for Claude Code
//    brings the server).
// 2. Hook: install its session-start hook (memax hook session-start),
//    where the agent has one.
// 3. Connection: an agent connects when it signs in over OAuth, at
//    Propose (Cursor and Gemini CLI at Read). If it already has a
//    connection, connect it to this repository's space at that level, or
//    lower where it is lower elsewhere: never raised from the CLI.
// 4. Compile: in a linked repository whose files aren't in sync yet,
//    compile once and write the files here.
import type { Memax, V2 } from "memax-sdk";
import type { DaemonPaths } from "../daemon/paths.js";
import { findLinkedRepo } from "../daemon/registry.js";
import { AGENTS, type AgentEntry } from "../init/agents.js";
import { connectToSpaces } from "../init/connect.js";
import type { CompileOutcome, McpOutcome } from "../init/types.js";
import { gitRoot } from "../project-context.js";
import { resolveSpace, SpaceChoiceError } from "../v2-space.js";
import {
  hookFileLabel,
  hookSupport,
  installHook,
  hasClaudePlugin,
  type HookOutcome,
} from "./hooks.js";

export interface ConnectOptions {
  space?: string;
  /** --no-hook */
  hook?: boolean;
  /** --no-compile */
  compile?: boolean;
  format?: string;
  /** Seconds to wait for the first compile. */
  timeout?: string;
}

export interface ConnectDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  home: string;
  platform: NodeJS.Platform;
  hasCredentials: () => boolean;
  hasMcp: (agent: AgentEntry) => boolean;
  writeMcp: (agent: AgentEntry) => Promise<McpOutcome>;
  /** After writing MCP settings (Codex's one-time `codex mcp login`). */
  afterMcp?: (agent: AgentEntry) => Promise<void>;
  compile: (
    space: string,
    timeoutSeconds: number,
  ) => Promise<CompileOutcome | null>;
}

export interface ConnectReport {
  agent: V2.AgentKind;
  name: string;
  mcp:
    | "written"
    | "present"
    | "plugin"
    | "connector"
    | "unsupported"
    | { error: string };
  hook: {
    outcome: HookOutcome | "skipped" | "windows";
    file: string | null;
    note?: string;
  };
  connection: {
    state: "connected" | "already" | "on_sign_in" | "signed_out" | "no_space";
    space?: string;
    autonomy?: V2.Autonomy;
    note?: string;
  };
  compile: {
    state:
      | "compiled"
      | "in_sync"
      | "not_linked"
      | "skipped"
      | "timed_out"
      | "failed"
      | "no_targets";
    files?: string[];
    note?: string;
  };
}

const ALIASES: Record<string, V2.AgentKind> = {
  "claude-code": "claude-code",
  codex: "codex",
  cursor: "cursor",
  gemini: "gemini-cli",
  "gemini-cli": "gemini-cli",
  copilot: "copilot",
  opencode: "opencode",
  windsurf: "windsurf",
  chatgpt: "chatgpt",
};

/** The agent a `memax connect` argument names, or null. */
export function agentFor(arg: string): AgentEntry | null {
  const kind = ALIASES[arg.trim().toLowerCase()];
  return kind ? (AGENTS.find((a) => a.kind === kind) ?? null) : null;
}

export const CONNECT_AGENTS = Object.keys(ALIASES).filter(
  (k) => k !== "gemini-cli",
);

/** Whether every file target of the space is compiled and written here. */
function inSync(targets: V2.Target[]): boolean {
  const local = targets.filter(
    (t) => t.delivery === "local" && t.sync_state !== "off",
  );
  return (
    local.length > 0 &&
    local.every((t) => t.sync_state === "in_sync" && !!t.last_compile)
  );
}

export async function runConnect(
  a: AgentEntry,
  o: ConnectOptions,
  d: ConnectDeps,
): Promise<ConnectReport> {
  const r: ConnectReport = {
    agent: a.kind,
    name: a.name,
    mcp: "unsupported",
    hook: { outcome: "skipped", file: hookFileLabel(a.kind) },
    connection: { state: "signed_out" },
    compile: { state: "skipped" },
  };

  // 1. MCP settings.
  const plugin = a.kind === "claude-code" && hasClaudePlugin(d.home);
  if (a.connector) r.mcp = "connector";
  else if (plugin) r.mcp = "plugin";
  else if (!a.setupId) r.mcp = "unsupported";
  else if (d.hasMcp(a)) r.mcp = "present";
  else {
    const res = await d.writeMcp(a);
    r.mcp = res;
    if (res === "written") await d.afterMcp?.(a);
  }

  // 2. The session-start hook.
  const support = hookSupport(a.kind);
  if (o.hook === false) r.hook.outcome = "skipped";
  else if (!support.file) r.hook = { outcome: "unsupported", file: null };
  else if (d.platform === "win32") r.hook.outcome = "windows";
  else r.hook.outcome = installHook(a.kind, d.home);
  if (support.note) r.hook.note = support.note;

  // 3. The connection, in this repository's space.
  if (!d.hasCredentials()) return r;
  let space: V2.Space;
  try {
    space = (
      await resolveSpace(d.memax, {
        space: o.space,
        cwd: d.cwd,
        paths: d.paths,
      })
    ).space;
  } catch (err) {
    if (!(err instanceof SpaceChoiceError)) throw err;
    r.connection = { state: "no_space", note: err.message };
    return r;
  }
  r.connection = { state: "on_sign_in", space: space.slug, autonomy: a.start };
  if (!a.connector) {
    const conn = (await d.memax.v2.agents.list()).items.find(
      (c) => c.agent === a.kind && c.state === "active",
    );
    if (conn) {
      const res = await connectToSpaces(d.memax, conn, a.start, [space]);
      r.connection = res.autonomy
        ? {
            state: res.added > 0 ? "connected" : "already",
            space: space.slug,
            autonomy: res.autonomy,
          }
        : {
            state: "on_sign_in",
            space: space.slug,
            autonomy: a.start,
            note: "the space starts new agents lower; it connects on its next sign-in",
          };
    }
  }

  // 4. A first compile, in a linked repository.
  if (o.compile === false) return r;
  const root = gitRoot(d.cwd);
  const link = root ? findLinkedRepo(d.paths, root) : undefined;
  if (!link || link.space_id !== space.id) {
    r.compile = { state: "not_linked" };
    return r;
  }
  const targets = (await d.memax.v2.targets.list(space.id)).items;
  if (targets.length === 0) {
    r.compile = { state: "no_targets" };
    return r;
  }
  if (inSync(targets)) {
    r.compile = {
      state: "in_sync",
      files: targets
        .filter((t) => t.delivery === "local" && t.sync_state === "in_sync")
        .map((t) => `${t.label} ${t.last_compile?.ref ?? ""}`.trim()),
    };
    return r;
  }
  const out = await d.compile(space.slug, Math.max(1, Number(o.timeout ?? 30)));
  if (!out) r.compile = { state: "failed" };
  else
    r.compile = {
      state: out.timedOut ? "timed_out" : "compiled",
      files: out.targets.flatMap((t) => t.files),
    };
  return r;
}
