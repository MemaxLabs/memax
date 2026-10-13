// OpenCode and the Personal space (personal-layer design §3.2; the load
// matrix, memax-internal docs/v2/adapter-load-matrix-2026-10.md). Memax
// writes no file of OpenCode's: memax connect opencode on a linked machine
// adds one entry to OpenCode's `instructions`, the canonical copy's
// absolute path (~/.memax/personal/AGENTS.md), and memax unlink --home
// takes it out. Creating ~/.config/opencode/AGENTS.md instead would stop
// OpenCode reading the person's ~/.claude/CLAUDE.md.
//
// From OpenCode's source at v1.18.35 (53d1eab), each point checked by
// running OpenCode 1.19.0 in a scratch home against a local model server
// that recorded the system prompt:
// - packages/opencode/src/session/instruction.ts L60–63, L115–120: of the
//   global files, OpenCode reads the first that exists:
//   ~/.config/opencode/AGENTS.md, else ~/.claude/CLAUDE.md, unless
//   OPENCODE_DISABLE_CLAUDE_CODE or OPENCODE_DISABLE_CLAUDE_CODE_PROMPT is
//   set (src/effect/runtime-flags.ts L23–26; true is "true", "yes", "on",
//   "1" or "y"). So where the Personal space reaches ~/.claude/CLAUDE.md
//   (claude_user's fallback block), an entry would load it twice.
// - L135–148: each entry, `~/` or absolute, is matched in its folder and
//   added to the same set as those files, so one path loads once.
// - packages/core/src/global.ts L13 (xdg-basedir): the folder is
//   $XDG_CONFIG_HOME/opencode, else ~/.config/opencode.
// - packages/opencode/src/config/config.ts L272–274: it merges config.json,
//   opencode.json, then opencode.jsonc there, and a later file's list
//   replaces an earlier one's (remeda's mergeDeep), so the last of them
//   that sets `instructions` is the list OpenCode uses; an empty one hides
//   the rest. L140–148: OpenCode edits the first of opencode.jsonc,
//   opencode.json, config.json that exists. L365–366: the project's and
//   OPENCODE_CONFIG's lists add to the global one.
// - L240: the files are JSONC, so they're edited in place (lib/jsonc.ts).
import {
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import type { Memax } from "memax-sdk";
import {
  readHomeLink,
  type HomeFile,
  type HomeLink,
} from "../daemon/home-link.js";
import { HOME_CANONICAL, tilde } from "../daemon/home-place.js";
import type { DaemonPaths } from "../daemon/paths.js";
import {
  appendJsonc,
  parseJsoncObject,
  removeFromJsonc,
  SettingsFileError,
  setJsonc,
} from "../jsonc.js";
import { linkedCanonical, setReaders, type HomeFileReport } from "./home.js";

export interface OpenCodeEnv {
  /** The home folder, a real path. */
  home: string;
  env: Record<string, string | undefined>;
}

/** OpenCode's settings folder: `$XDG_CONFIG_HOME/opencode`, else `~/.config/opencode`. */
export function openCodeDir(e: OpenCodeEnv): string {
  const xdg = e.env.XDG_CONFIG_HOME?.trim();
  return join(
    xdg && isAbsolute(xdg) ? xdg : join(e.home, ".config"),
    "opencode",
  );
}

/** OpenCode's global settings files, in the order it merges them. */
const MERGED = ["config.json", "opencode.json", "opencode.jsonc"];

/** The one OpenCode edits itself: the first of these that exists. */
const EDITED = ["opencode.jsonc", "opencode.json", "config.json"];

interface SettingsFile {
  path: string;
  text: string;
  settings: Record<string, unknown>;
}

function readSettings(path: string): SettingsFile | null {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch {
    return null;
  }
  const settings = parseJsoncObject(text);
  if (!settings)
    throw new SettingsFileError(
      path,
      "add your Personal space to its instructions",
    );
  return { path, text, settings };
}

/**
 * The file whose `instructions` OpenCode uses: the last that sets them,
 * else the one OpenCode edits, else a new opencode.jsonc. Throws
 * SettingsFileError when one of them isn't JSON (OpenCode then reads none
 * of its global settings) or the list isn't one.
 */
export function instructionsFile(dir: string): SettingsFile {
  const files = MERGED.flatMap((n) => readSettings(join(dir, n)) ?? []);
  const sets = files
    .filter((f) => f.settings.instructions !== undefined)
    .at(-1);
  if (sets) {
    if (!Array.isArray(sets.settings.instructions))
      throw new SettingsFileError(sets.path, "make instructions a list");
    return sets;
  }
  for (const n of EDITED) {
    const f = files.find((x) => x.path === join(dir, n));
    if (f) return f;
  }
  return { path: join(dir, "opencode.jsonc"), text: "", settings: {} };
}

/** Whether an `instructions` entry names `abs` (`~/` and absolute alike). */
function names(home: string, raw: unknown, abs: string): boolean {
  if (typeof raw !== "string") return false;
  const p = raw.startsWith("~/") ? join(home, raw.slice(2)) : raw;
  return isAbsolute(p) && resolve(p) === resolve(abs);
}

const TRUE = new Set(["true", "yes", "on", "1", "y"]);

/**
 * The home file through which OpenCode already reads the Personal space
 * here, or null: claude_user's block in ~/.claude/CLAUDE.md (the fallback
 * for an old Claude Code), while OpenCode has no AGENTS.md of its own and
 * its Claude Code fallback is on. With it, an entry would load it twice.
 */
export function readsThroughClaude(
  e: OpenCodeEnv,
  link: HomeLink,
): HomeFile | null {
  const f = link.files.find(
    (x) => x.kind === "claude_user" && x.path === ".claude/CLAUDE.md",
  );
  if (!f) return null;
  if (existsSync(join(openCodeDir(e), "AGENTS.md"))) return null;
  for (const v of [
    "OPENCODE_DISABLE_CLAUDE_CODE",
    "OPENCODE_DISABLE_CLAUDE_CODE_PROMPT",
  ])
    if (TRUE.has(e.env[v] ?? "")) return null;
  return f;
}

/** Adds `abs` to OpenCode's instructions; "present" when they name it already. */
export function addInstruction(
  e: OpenCodeEnv,
  abs: string,
): { state: "written" | "present"; file: string } {
  const f = instructionsFile(openCodeDir(e));
  const list = (f.settings.instructions as unknown[] | undefined) ?? [];
  if (list.some((raw) => names(e.home, raw, abs)))
    return { state: "present", file: f.path };
  const next = appendJsonc(f.text, ["instructions"], abs);
  mkdirSync(dirname(f.path), { recursive: true });
  writeFileSync(f.path, next);
  return { state: "written", file: f.path };
}

/**
 * Takes the entry Memax wrote (`abs`, exactly) out of every one of
 * OpenCode's global settings files, and a list it leaves empty with it, since
 * an empty list would hide an earlier file's. The files it changed.
 */
export function removeInstruction(e: OpenCodeEnv, abs: string): string[] {
  const changed: string[] = [];
  for (const n of MERGED) {
    const path = join(openCodeDir(e), n);
    let f: SettingsFile | null;
    try {
      f = readSettings(path);
    } catch {
      continue; // not JSON: Memax's entry can't be told apart, so it stays
    }
    const list = f?.settings.instructions;
    if (!f || !Array.isArray(list) || !list.includes(abs)) continue;
    let next = removeFromJsonc(f.text, ["instructions"], (v) => v === abs);
    const left = parseJsoncObject(next)?.instructions;
    if (Array.isArray(left) && left.length === 0)
      next = setJsonc(next, ["instructions"], undefined);
    writeFileSync(path, next);
    changed.push(path);
  }
  return changed;
}

/**
 * Whether OpenCode is set up here and doesn't read the Personal space yet:
 * its instructions don't name the canonical copy, nor does it read it from
 * ~/.claude/CLAUDE.md. memax link --home then says memax connect opencode
 * adds it.
 */
export function openCodeWithout(e: OpenCodeEnv, link: HomeLink): boolean {
  const dir = openCodeDir(e);
  if (!isDir(dir) || readsThroughClaude(e, link)) return false;
  try {
    const list = instructionsFile(dir).settings.instructions;
    const abs = join(link.home, HOME_CANONICAL);
    return !(
      Array.isArray(list) && list.some((raw) => names(e.home, raw, abs))
    );
  } catch {
    return true;
  }
}

function isDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

export interface OpenCodeDeps {
  memax: Memax;
  paths: DaemonPaths;
  env: Record<string, string | undefined>;
}

const without = (r: string[]) => r.filter((a) => a !== "opencode");
const withIt = (r: string[]) => [...r, "opencode"];
const READS = "OpenCode reads it on this machine";
const READS_ELSEWHERE =
  "OpenCode reads the Personal space from another file on this machine";

/**
 * memax connect opencode on a machine whose home is linked: OpenCode's
 * instructions name the canonical copy, which joins the link if it isn't
 * in it; or, where OpenCode reads the Personal space from
 * ~/.claude/CLAUDE.md already, they don't (an entry Memax wrote before
 * comes out). The canonical's read_by says whether OpenCode reads it.
 * `needsCompile`: the file OpenCode reads isn't on disk yet.
 */
export async function connectOpenCode(
  d: OpenCodeDeps,
): Promise<HomeFileReport & { needsCompile?: boolean }> {
  const linked = readHomeLink(d.paths);
  if (!linked) return { state: "not_linked" };
  const e = { home: linked.home, env: d.env };
  const dir = openCodeDir(e);
  if (!isDir(dir)) return { state: "absent", path: tilde(linked.home, dir) };
  const got = await linkedCanonical(d);
  if ("report" in got) return got.report;
  const { link, canonical } = got;

  const claude = readsThroughClaude(e, link);
  if (claude) {
    const removed = removeInstruction(e, canonical.abs);
    await setReaders(d, link, canonical, without, READS_ELSEWHERE);
    await setReaders(d, readHomeLink(d.paths) ?? link, claude, withIt, READS);
    return {
      state: "present",
      path: tilde(link.home, claude.abs),
      note: `OpenCode reads your Personal space there${
        removed.length > 0
          ? `, so Memax took ${tilde(link.home, canonical.abs)} out of its instructions (${removed.map((p) => tilde(link.home, p)).join(", ")}): it would load twice`
          : ""
      }`,
    };
  }

  let res: { state: "written" | "present"; file: string };
  try {
    res = addInstruction(e, canonical.abs);
  } catch (err) {
    if (!(err instanceof SettingsFileError)) throw err;
    return {
      state: "refused",
      path: tilde(link.home, err.path),
      note: err.message,
    };
  }
  await setReaders(d, link, canonical, withIt, READS);
  const file = tilde(link.home, res.file);
  const reads = tilde(link.home, canonical.abs);
  return {
    state: res.state === "written" ? "added" : "present",
    path: file,
    note:
      res.state === "written"
        ? `${reads}, your Personal space, added to its instructions`
        : `its instructions name ${reads}, your Personal space`,
    needsCompile: !existsSync(canonical.abs),
  };
}
