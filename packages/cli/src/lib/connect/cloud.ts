// memax connect --cloud <agent> (cloud warm-start design §4.3, §5): a
// cloud agent's connection and start key for this repository's space, the
// committed hook file that runs the cloud start, and where the key goes.
//
// - Claude Code's cloud sessions run the repository's .claude/settings.json
//   hooks; the entry runs only where CLAUDE_CODE_REMOTE=true, so it never
//   runs on a person's machine.
// - Copilot's cloud agent runs .github/hooks/*.json. Copilot CLI loads the
//   same file on people's machines, so the entry runs only where the
//   sandbox has MEMAX_START_KEY: no npx on a laptop.
// - Codex Cloud documents no session-start hook, so it starts from the
//   committed AGENTS.md alone, and gets no key until it has a start.
//
// The key is printed once and never written to a file, copied or handed
// to another process: the hook files name the variable, not the key.
// Running it again for a connected agent leaves its key alone unless the
// person asks for a new one (a confirmation, or --yes without a terminal).
import { randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { Memax, V2 } from "memax-sdk";
import { agent as registered } from "../agents/agents.js";
import type { DaemonPaths } from "../daemon/paths.js";
import { gitRoot } from "../project-context.js";
import { resolveSpace, SpaceChoiceError } from "../v2-space.js";

export const CLOUD_AGENTS = ["claude-code", "copilot", "codex"] as const;
export type CloudAgent = (typeof CLOUD_AGENTS)[number];

/**
 * The cloud start's package, pinned exactly: npm versions are immutable,
 * so the committed command can't change under a team. Kept equal to
 * packages/start/package.json by test/cloud/connect-cloud.test.ts.
 */
export const START_PACKAGE = "@memaxlabs/start@1.0.0";

/** What marks Memax's cloud entry in a hook file, whatever its version. */
const MARK = "@memaxlabs/start";

export function cloudAgentFor(arg: string): CloudAgent | null {
  const key = arg.trim().toLowerCase();
  return (CLOUD_AGENTS as readonly string[]).includes(key)
    ? (key as CloudAgent)
    : null;
}

type Json = Record<string, unknown>;

/** The committed hook file an agent's cloud sandbox runs, and its entry. */
export interface CloudHook {
  /** Repository-relative, with forward slashes. */
  path: string;
  event: string;
  entry: Json;
}

export function cloudHook(agent: CloudAgent): CloudHook | null {
  // Where the agent's cloud sandbox reads hooks, from the agents registry.
  const at = registered(agent).cloudHook;
  if (!at) return null;
  switch (agent) {
    case "claude-code":
      return {
        path: at.file,
        event: at.event,
        entry: {
          matcher: "startup|resume|compact",
          hooks: [
            {
              type: "command",
              command: `[ "$CLAUDE_CODE_REMOTE" = true ] && npx -y ${START_PACKAGE} --agent claude-code || true`,
              timeout: 20,
            },
          ],
        },
      };
    case "copilot":
      return {
        path: at.file,
        event: at.event,
        entry: {
          type: "command",
          bash: `[ -n "$MEMAX_START_KEY" ] && npx -y ${START_PACKAGE} --agent copilot || true`,
          timeoutSec: 20,
        },
      };
    default:
      return null;
  }
}

const mentions = (v: unknown) => JSON.stringify(v ?? null).includes(MARK);

/**
 * `list` with `entry` in place of any other Memax cloud entry (an older
 * pin); a group that held other hooks too keeps them. Null when the list
 * already holds exactly `entry` and nothing else of ours.
 */
function upsert(list: unknown, entry: Json): unknown[] | null {
  const items = Array.isArray(list) ? list : [];
  const ours = items.filter(mentions);
  if (ours.length === 1 && JSON.stringify(ours[0]) === JSON.stringify(entry))
    return null;
  const others = items.flatMap((item) => {
    if (!mentions(item)) return [item];
    const g = item as Json;
    const rest = Array.isArray(g.hooks)
      ? g.hooks.filter((h) => !mentions(h))
      : [];
    return rest.length > 0 ? [{ ...g, hooks: rest }] : [];
  });
  return [...others, entry];
}

/** The whole file, as it reads with Memax's entry and nothing else. */
export function cloudHookFile(hook: CloudHook): Json {
  const file: Json = hook.path.startsWith(".github/") ? { version: 1 } : {};
  file.hooks = { [hook.event]: [hook.entry] };
  return file;
}

export type FileOutcome = "written" | "present" | { error: string };

/** Puts the entry in the repository's hook file, keeping everything else in it. */
export function writeCloudHook(root: string, hook: CloudHook): FileOutcome {
  const path = join(root, ...hook.path.split("/"));
  let json: Json = {};
  if (existsSync(path)) {
    try {
      const v: unknown = JSON.parse(readFileSync(path, "utf8"));
      if (!v || typeof v !== "object" || Array.isArray(v)) throw new Error();
      json = v as Json;
    } catch {
      return {
        error: `${hook.path} isn't a JSON object; fix it, or add the entry by hand`,
      };
    }
  }
  const hooks =
    json.hooks && typeof json.hooks === "object" && !Array.isArray(json.hooks)
      ? (json.hooks as Json)
      : {};
  const next = upsert(hooks[hook.event], hook.entry);
  if (!next) return "present";
  const out: Json = { ...json };
  if (hook.path.startsWith(".github/")) out.version ??= 1;
  out.hooks = { ...hooks, [hook.event]: next };
  try {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, `${JSON.stringify(out, null, 2)}\n`);
  } catch (err) {
    return { error: (err as Error).message };
  }
  return "written";
}

const CODEOWNERS = ["CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"];

/** Whether the repository's CODEOWNERS names the hook file (or its folder). */
export function ownedHere(root: string, path: string): boolean {
  const folder = path.slice(0, path.lastIndexOf("/") + 1);
  return CODEOWNERS.some((name) => {
    const file = join(root, ...name.split("/"));
    if (!existsSync(file)) return false;
    return readFileSync(file, "utf8")
      .split("\n")
      .map((l) => l.trim().split(/\s+/)[0] ?? "")
      .some((p) => {
        const bare = p.replace(/^\//, "");
        return bare === path || bare === folder || bare === `${folder}*`;
      });
  });
}

export interface CloudConnectOptions {
  space?: string;
  /** --no-hook: print the hook file instead of writing it. */
  hook?: boolean;
  /** --yes: a new key for a connected agent without asking. */
  yes?: boolean;
  format?: string;
}

export interface CloudConnectDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  /** The API the CLI talks to: the sandbox needs the same one. */
  apiUrl: string;
  /** A yes or no from a person at a terminal; absent without one. */
  confirm?: (question: string) => Promise<boolean>;
}

export interface CloudConnectReport {
  agent: CloudAgent;
  name: string;
  space: string;
  /** The cloud agent's connection; null for Codex, which has no start yet. */
  connection: {
    id: string;
    display_name: string;
    /** connected: new; rekeyed: a new key; already: left as it was. */
    state: "connected" | "rekeyed" | "already";
    autonomy: V2.Autonomy;
  } | null;
  /** The start key's secret, this once; null when no key was made. */
  key: V2.StartKey | null;
  /** The committed hook file; null for an agent with none (Codex). */
  hook: {
    path: string;
    /** written or present here, printed (--no-hook, or no repository), or an error. */
    outcome: FileOutcome | "printed";
    /** The file's content when printed. */
    content?: string;
  } | null;
  /** Where the key goes, and what else the sandbox needs, in order. */
  steps: string[];
  /** Whether CODEOWNERS names the hook file; null without one. */
  owned: boolean | null;
  note?: string;
}

const PRODUCTION_HOST = "api.memax.app";

/** Where the key goes for each agent, and what else its sandbox needs. */
export function cloudSteps(
  agent: CloudAgent,
  apiUrl: string,
  hookPath: string | null,
): string[] {
  const host = hostOf(apiUrl);
  const other = host !== PRODUCTION_HOST;
  switch (agent) {
    case "claude-code":
      return [
        `On Pro or Max: add a network secret for ${host} to the cloud environment, with the key as its Bearer token. The key never enters the sandbox.`,
        "On Team or Enterprise: set MEMAX_START_KEY to the key in a personal environment's variables. Not a shared one: everyone whose sessions use a shared environment can read its variables.",
        ...(other
          ? [
              `Set MEMAX_API_URL=${origin(apiUrl)} in the environment's variables, and allow ${host} in its network access.`,
            ]
          : []),
        `Commit ${hookPath}, and AGENTS.md with CLAUDE.md after each compile.`,
      ];
    case "copilot":
      return [
        "Add the key as the Agents secret MEMAX_START_KEY: the repository's (or organisation's) Settings › Secrets and variables › Agents.",
        `Let the agent reach ${host}: add it to the repository's Copilot internet access settings (the agent firewall).`,
        ...(other
          ? [
              `Add the Agents variable MEMAX_API_URL=${origin(apiUrl)} beside the secret.`,
            ]
          : []),
        `Commit ${hookPath}, and AGENTS.md after each compile.`,
      ];
    case "codex":
      return [
        "Codex Cloud runs no session-start hook Memax can use yet, so it starts from the committed AGENTS.md: commit it after each compile.",
      ];
  }
}

function hostOf(apiUrl: string): string {
  try {
    return new URL(apiUrl).host;
  } catch {
    return PRODUCTION_HOST;
  }
}

function origin(apiUrl: string): string {
  try {
    return new URL(apiUrl).origin;
  } catch {
    return `https://${PRODUCTION_HOST}`;
  }
}

/** A refusal or failure the command can't go on from, worded for a person. */
export class CloudConnectError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CloudConnectError";
  }
}

export async function runConnectCloud(
  agent: CloudAgent,
  o: CloudConnectOptions,
  d: CloudConnectDeps,
): Promise<CloudConnectReport> {
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
    if (err instanceof SpaceChoiceError)
      throw new CloudConnectError(err.message);
    throw err;
  }
  if (space.kind === "personal")
    throw new CloudConnectError(
      `${space.slug} is your Personal space, which reaches your own machines' files (memax link --home). A cloud agent starts in a project or team space: --space <slug>.`,
    );
  const root = gitRoot(d.cwd);
  const hook = cloudHook(agent);
  const r: CloudConnectReport = {
    agent,
    name: registered(agent).name,
    space: space.slug,
    connection: null,
    key: null,
    hook: null,
    steps: cloudSteps(agent, d.apiUrl, hook?.path ?? null),
    owned: null,
  };
  if (!hook) return r;

  // The connection: a new one, or a new key for the one there is.
  const mine = (await d.memax.v2.agents.list()).items.find(
    (c) =>
      c.surface === "cloud" &&
      c.agent === agent &&
      c.state !== "disconnected" &&
      c.credential.active &&
      c.spaces.some((s) => s.space_id === space.id),
  );
  if (mine) {
    const rekey =
      o.yes === true ||
      (!!d.confirm &&
        (await d.confirm(
          `  ${mine.display_name} is already connected. Give it a new start key? The old one stops working at once. [y/N] `,
        )));
    if (rekey) {
      const res = await d.memax.v2.agents.rekey(
        mine.id,
        {},
        { idempotencyKey: randomUUID(), via: "cli" },
      );
      r.connection = summary(res.agent, space, "rekeyed");
      r.key = res.key ?? null;
    } else {
      r.connection = summary(mine, space, "already");
      r.note = d.confirm
        ? "It keeps its start key."
        : `It keeps its start key. For a new one: memax connect --cloud ${agent} --yes (the old one stops working at once).`;
    }
  } else {
    const res = await d.memax.v2.agents.connectCloud(
      { agent, space: space.slug },
      { idempotencyKey: randomUUID(), via: "cli" },
    );
    r.connection = summary(res.agent, space, "connected");
    r.key = res.key ?? null;
  }

  // The committed hook file, which names the variable and never the key.
  if (o.hook === false || !root) {
    r.hook = {
      path: hook.path,
      outcome: "printed",
      content: `${JSON.stringify(cloudHookFile(hook), null, 2)}\n`,
    };
    if (!root)
      r.note = [r.note, "Not in a git repository, so nothing was written."]
        .filter(Boolean)
        .join(" ");
  } else {
    r.hook = { path: hook.path, outcome: writeCloudHook(root, hook) };
  }
  if (root) {
    const has = CODEOWNERS.some((n) => existsSync(join(root, ...n.split("/"))));
    r.owned = has ? ownedHere(root, hook.path) : null;
  }
  return r;
}

function summary(
  c: V2.AgentConnection,
  space: V2.Space,
  state: "connected" | "rekeyed" | "already",
): NonNullable<CloudConnectReport["connection"]> {
  return {
    id: c.id,
    display_name: c.display_name,
    state,
    autonomy: c.spaces.find((s) => s.space_id === space.id)?.autonomy ?? "read",
  };
}
