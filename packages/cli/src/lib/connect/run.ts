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
// 4. Gemini CLI reads AGENTS.md one of two ways, only when asked, never
//    both (lib/connect/gemini.ts geminiClash): --agents-md lists AGENTS.md
//    in its context.fileName, on this machine, which also brings the
//    Personal space's ~/.gemini/AGENTS.md; --gemini-md gives the space the
//    GEMINI.md shim, for everyone in the repository. Antigravity, which
//    replaced Gemini CLI for most people, reads AGENTS.md itself, so both
//    are opt-in (the compiler's OPT_IN_TARGET_KINDS).
// 5. Compile: in a linked repository whose files aren't in sync yet,
//    compile once and write the files here.
// 6. Home (personal-layer design §6.1, §7.2): on a machine whose home is
//    linked (memax link --home), connect the agent to the Personal space
//    too, and add its home file (Claude Code's rules file, Codex's block),
//    or move it to the file the agent reads now; then deliver it. Outside
//    a repository, the Personal space is the space.
import type { Memax, V2 } from "memax-sdk";
import type { DaemonPaths } from "../daemon/paths.js";
import { findLinkedRepo, readRegistry } from "../daemon/registry.js";
import { tilde } from "../daemon/home-place.js";
import {
  geminiClash,
  geminiSettings,
  readsAgentsMd,
  writeAgentsMd,
  type ShimSpace,
} from "./gemini.js";
import { AGENTS, type AgentEntry } from "../init/agents.js";
import { connectToSpaces } from "../init/connect.js";
import { readHomeLink } from "../daemon/home-link.js";
import {
  connectHomeFile,
  HOME_KIND,
  linkedPersonal,
  type HomeFileReport,
} from "./home.js";
import { handWritten } from "../init/finish.js";
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
  /** --gemini-md: give the space the GEMINI.md shim (Gemini CLI only). */
  geminiMd?: boolean;
  /** --agents-md: list AGENTS.md in Gemini CLI's context.fileName here. */
  agentsMd?: boolean;
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
  /** The person's shell, where CLAUDE_CONFIG_DIR and CODEX_HOME are set. */
  env?: Record<string, string | undefined>;
  /** The web app, for the pages a message points at. */
  appUrl?: string;
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
  /** On a machine whose home is linked: the agent in the Personal space. */
  personal?: {
    state: "connected" | "already" | "on_sign_in";
    space: string;
    autonomy?: V2.Autonomy;
  };
  /** The agent's home file on this machine (Claude Code, Codex). */
  home?: HomeFileReport & {
    compile?: "compiled" | "timed_out" | "failed";
  };
  /** Gemini CLI only: the space's GEMINI.md shim. */
  shim?: {
    state: "created" | "present" | "not_asked" | "refused";
    path?: string;
    note?: string;
  };
  /** Gemini CLI only: AGENTS.md in its context.fileName on this machine. */
  contextFile?: {
    state: "written" | "present" | "not_asked" | "refused";
    file: string;
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
  const homeLink = readHomeLink(d.paths);
  let space: V2.Space;
  try {
    space = (
      await resolveSpace(d.memax, {
        space: o.space,
        cwd: d.cwd,
        paths: d.paths,
      }).catch((err) => {
        // Outside a repository, the Personal space this home is linked to.
        if (!(err instanceof SpaceChoiceError) || o.space || !homeLink)
          throw err;
        return resolveSpace(d.memax, {
          space: homeLink.space_id,
          paths: d.paths,
        });
      })
    ).space;
  } catch (err) {
    if (!(err instanceof SpaceChoiceError)) throw err;
    r.connection = { state: "no_space", note: err.message };
    return r;
  }
  const personal =
    homeLink && homeLink.space_id !== space.id
      ? await linkedPersonal(d.memax, d.paths).catch(() => null)
      : null;
  r.connection = { state: "on_sign_in", space: space.slug, autonomy: a.start };
  if (!a.connector) {
    const conn = (await d.memax.v2.agents.list()).items.find(
      (c) => c.agent === a.kind && c.state === "active",
    );
    if (personal)
      r.personal = {
        state: "on_sign_in",
        space: personal.slug,
        autonomy: a.start,
      };
    if (conn && personal) {
      const res = await connectToSpaces(d.memax, conn, a.start, [personal]);
      if (res.autonomy)
        r.personal = {
          state: res.added > 0 ? "connected" : "already",
          space: personal.slug,
          autonomy: res.autonomy,
        };
    }
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

  // 4. Gemini CLI: context.fileName or the GEMINI.md shim, when asked.
  const root = gitRoot(d.cwd);
  let targets: V2.Target[] | undefined;
  if (a.kind === "gemini-cli") {
    targets = (await d.memax.v2.targets.list(space.id)).items;
    const g = { home: d.home, env: d.env ?? {}, platform: d.platform };
    const file = tilde(d.home, geminiSettings(g));
    const shims = await shimSpaces(d, space, targets);
    const reads = readsAgentsMd(g);
    if (o.agentsMd) {
      const clash = geminiClash("agents_md", { readsAgentsMd: reads, shims });
      try {
        r.contextFile = clash
          ? { state: "refused", file, note: clash }
          : { state: writeAgentsMd(g), file };
      } catch (err) {
        r.contextFile = {
          state: "refused",
          file,
          note: (err as Error).message,
        };
      }
    } else {
      r.contextFile = { state: reads ? "present" : "not_asked", file };
    }
    const shim = targets.find(
      (t) => t.kind === "gemini_md" && t.sync_state !== "off",
    );
    const geminiMdRefused = o.geminiMd
      ? geminiClash("gemini_md", { readsAgentsMd: reads, shims })
      : null;
    if (shim) {
      const both = geminiClash("both", { readsAgentsMd: reads, shims });
      r.shim = {
        state: "present",
        path: shim.path ?? "GEMINI.md",
        ...(both ? { note: both } : {}),
      };
    } else if (!o.geminiMd) r.shim = { state: "not_asked" };
    else if (geminiMdRefused)
      r.shim = { state: "refused", note: geminiMdRefused };
    else {
      // A GEMINI.md someone wrote stays theirs, with one Memax block in
      // it, as init does for CLAUDE.md.
      const own = !!root && handWritten(root, "GEMINI.md");
      await d.memax.v2.targets.create(
        space.id,
        own
          ? {
              kind: "gemini_md",
              settings: { user_owned: true },
              reason: "GEMINI.md already existed in the repository",
            }
          : { kind: "gemini_md", reason: "memax connect gemini --gemini-md" },
        {
          idempotencyKey: `connect-target:${space.id}:gemini_md`,
          via: "cli",
        },
      );
      targets = (await d.memax.v2.targets.list(space.id)).items;
      r.shim = { state: "created", path: "GEMINI.md" };
    }
  }

  // 6. The agent's home file, on a machine whose home is linked.
  const homeKind = HOME_KIND[a.kind];
  if (homeKind && homeLink) {
    r.home = await connectHomeFile(homeKind, {
      memax: d.memax,
      paths: d.paths,
      env: d.env ?? {},
    });
    if (
      o.compile !== false &&
      (r.home.state === "added" || r.home.state === "moved")
    ) {
      const out = await d.compile(
        homeLink.space_slug,
        Math.max(1, Number(o.timeout ?? 30)),
      );
      r.home.compile = !out
        ? "failed"
        : out.timedOut
          ? "timed_out"
          : "compiled";
    }
  } else if (homeKind) {
    r.home = { state: "not_linked" };
  }

  // 5. A first compile, in a linked repository.
  if (o.compile === false) return r;
  const link = root ? findLinkedRepo(d.paths, root) : undefined;
  if (!link || link.space_id !== space.id) {
    r.compile = { state: "not_linked" };
    return r;
  }
  targets ??= (await d.memax.v2.targets.list(space.id)).items;
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

/**
 * The spaces linked on this machine, and this one, whose GEMINI.md shim is
 * on: with AGENTS.md in context.fileName, Gemini CLI would load AGENTS.md
 * twice in their repositories.
 */
async function shimSpaces(
  d: ConnectDeps,
  space: V2.Space,
  targets: V2.Target[],
): Promise<ShimSpace[]> {
  const app = (d.appUrl ?? "https://memax.app").replace(/\/+$/, "");
  const ids = new Map<string, string>([[space.id, space.slug]]);
  for (const l of readRegistry(d.paths)) ids.set(l.space_id, l.space_slug);
  const out: ShimSpace[] = [];
  for (const [id, slug] of ids) {
    const list =
      id === space.id
        ? targets
        : await d.memax.v2.targets
            .list(id)
            .then((x) => x.items)
            .catch(() => []);
    const shim = list.find(
      (t) => t.kind === "gemini_md" && t.sync_state !== "off",
    );
    if (shim)
      out.push({ slug, url: `${app}/${slug}/brief/targets/${shim.id}` });
  }
  return out;
}
