// Gemini on this machine (personal-layer design §3.2, and the load matrix,
// memax-internal docs/v2/adapter-load-matrix-2026-10.md):
//
// - Antigravity, in the IDE and as `agy`, reads ~/.gemini/AGENTS.md and
//   ~/.gemini/GEMINI.md in every session.
// - Gemini CLI reads the files `context.fileName` names in
//   ~/.gemini/settings.json, GEMINI.md by default: in ~/.gemini and in the
//   repository. Listing AGENTS.md there (memax connect gemini --agents-md)
//   makes it read the repository's AGENTS.md and the Personal space's
//   ~/.gemini/AGENTS.md itself.
// - Gemini CLI resolves each file's @imports on their own, so with
//   AGENTS.md listed, a repository's GEMINI.md shim (`@./AGENTS.md`) would
//   load AGENTS.md a second time. The two are never on together:
//   geminiClash says which one to keep.
// - Gemini's own memory tool writes to the first name in the list, so
//   GEMINI.md stays first and Memax's block in AGENTS.md is never its file.
//
// GEMINI_CLI_HOME moves the folder Gemini CLI calls home, which holds
// .gemini; the settings file allows comments (strip-json-comments), so it is
// edited in place.
import {
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, join } from "node:path";
import { onPath } from "../init/agents.js";
import { parseJsoncObject, SettingsFileError, setJsonc } from "../jsonc.js";

export interface GeminiEnv {
  home: string;
  env: Record<string, string | undefined>;
  platform?: NodeJS.Platform;
}

/** Gemini's folder: `$GEMINI_CLI_HOME/.gemini`, else `~/.gemini`. */
export function geminiDir(e: GeminiEnv): string {
  const moved = e.env.GEMINI_CLI_HOME?.trim();
  if (!moved) return join(e.home, ".gemini");
  const at = moved.startsWith("~/") ? join(e.home, moved.slice(2)) : moved;
  return join(isAbsolute(at) ? at : join(e.home, at), ".gemini");
}

export const geminiSettings = (e: GeminiEnv) =>
  join(geminiDir(e), "settings.json");

function readText(path: string): string {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return "";
  }
}

/** The context files Gemini CLI reads, as its settings name them. */
export function contextFiles(
  settings: Record<string, unknown> | null,
): string[] {
  const context = settings?.context;
  const names =
    context && typeof context === "object"
      ? (context as Record<string, unknown>).fileName
      : undefined;
  const list = (Array.isArray(names) ? names : [names]).filter(
    (n): n is string => typeof n === "string" && n.trim() !== "",
  );
  return list.length > 0 ? list : ["GEMINI.md"];
}

/** Whether Gemini CLI here reads AGENTS.md (its context.fileName lists it). */
export function readsAgentsMd(e: GeminiEnv): boolean {
  return contextFiles(parseJsoncObject(readText(geminiSettings(e)))).some(
    (n) => n.trim() === "AGENTS.md",
  );
}

function isDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

/**
 * Whether Antigravity is on this machine: its CLI on PATH, or its folder
 * under ~/.gemini (the CLI's `antigravity-cli` is documented; the IDE's
 * `antigravity` folder is what it creates, unverified against its docs).
 */
export function hasAntigravity(e: GeminiEnv): boolean {
  const dir = geminiDir(e);
  if (isDir(join(dir, "antigravity-cli")) || isDir(join(dir, "antigravity")))
    return true;
  const path = e.env.PATH ?? "";
  const platform = e.platform ?? process.platform;
  return ["agy", "antigravity"].some((c) => onPath(c, path, platform));
}

/** Who reads ~/.gemini/AGENTS.md here: the read_by of Gemini's home file. */
export function geminiReaders(e: GeminiEnv): string[] {
  return [
    ...(hasAntigravity(e) ? ["antigravity"] : []),
    ...(readsAgentsMd(e) ? ["gemini-cli"] : []),
  ];
}

/**
 * The settings text with AGENTS.md added to context.fileName, after the
 * names already there (GEMINI.md first when there were none), or null when
 * it is listed already. Throws SettingsFileError for a file that isn't JSON.
 */
export function withAgentsMd(path: string, text: string): string | null {
  const settings = parseJsoncObject(text);
  if (!settings)
    throw new SettingsFileError(path, 'add "AGENTS.md" to context.fileName');
  const names = contextFiles(settings);
  if (names.some((n) => n.trim() === "AGENTS.md")) return null;
  return setJsonc(text, ["context", "fileName"], [...names, "AGENTS.md"]);
}

/** Writes context.fileName; "present" when AGENTS.md was listed already. */
export function writeAgentsMd(e: GeminiEnv): "written" | "present" {
  const path = geminiSettings(e);
  const next = withAgentsMd(path, existsSync(path) ? readText(path) : "");
  if (next === null) return "present";
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, next);
  return "written";
}

/** A space whose repositories compile the GEMINI.md shim. */
export interface ShimSpace {
  slug: string;
  /** Its GEMINI.md target's page, where Stop compiling is. */
  url: string;
}

/**
 * The one rule: Gemini CLI reads AGENTS.md either through
 * context.fileName or through a repository's GEMINI.md shim, never both,
 * since each would load it once. Returns why the second can't be turned
 * on, and which to keep, or null when they don't clash.
 *
 * - `agents_md`: --agents-md was asked for while `shims` compile GEMINI.md.
 *   Keep context.fileName: it reads every repository's AGENTS.md and your
 *   Personal space, with no file in the repository; stop the shim first.
 * - `gemini_md`: --gemini-md was asked for while Gemini CLI here reads
 *   AGENTS.md itself. Keep context.fileName; the shim adds nothing here.
 * - `both`: they are on together already (someone set one by hand).
 */
export function geminiClash(
  asking: "agents_md" | "gemini_md" | "both",
  state: { readsAgentsMd: boolean; shims: ShimSpace[] },
): string | null {
  if (asking === "gemini_md") {
    return state.readsAgentsMd
      ? "Gemini CLI here reads AGENTS.md itself (context.fileName in ~/.gemini/settings.json), and GEMINI.md imports AGENTS.md, so it would load it twice. Keep context.fileName: GEMINI.md isn't needed. Teammates on Gemini CLI can run memax connect gemini --agents-md too."
      : null;
  }
  if (state.shims.length === 0) return null;
  if (asking === "both" && !state.readsAgentsMd) return null;
  const where = state.shims.map((s) => s.slug).join(", ");
  const stop = state.shims.map((s) => s.url).join(" and ");
  const lead =
    asking === "both"
      ? `Gemini CLI loads AGENTS.md twice in ${where}`
      : `Not changed: ${where} compiles GEMINI.md, which imports AGENTS.md, so with AGENTS.md in context.fileName Gemini CLI would load it twice`;
  return `${lead}. Keep context.fileName, which reads every repository's AGENTS.md and your Personal space with no file in the repository: stop GEMINI.md at ${stop}${asking === "both" ? "" : ", then run this again"}.`;
}
