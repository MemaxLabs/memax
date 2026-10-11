// This machine's home link (~/.memax/daemon/home.json), written by
// `memax link --home` and read by the daemon: the Personal space, the
// machine Memax knows this computer as, and every home file the person
// approved here, with the absolute path it resolved to in their shell.
// The daemon runs under launchd or systemd without CLAUDE_CONFIG_DIR or
// CODEX_HOME, so the paths are fixed here, at link time, and the daemon
// writes exactly these files and no others.
import { statSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { readJson, writeJsonAtomic } from "./json-file.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";

export interface HomeFile {
  target_id: string;
  kind: V2.TargetKind;
  /** Relative to the home folder: `.claude/rules/memax-personal.md`. */
  path: string;
  /** The home folder joined with `path`. */
  abs: string;
}

export interface HomeLink {
  version: 1;
  space_id: string;
  space_slug: string;
  machine_id: string;
  /** The home folder, a real path; every `abs` is inside it. */
  home: string;
  files: HomeFile[];
  created_at: string;
}

function isHomeFile(home: string, v: unknown): v is HomeFile {
  const f = v as HomeFile;
  return (
    typeof f === "object" &&
    f !== null &&
    typeof f.target_id === "string" &&
    f.target_id !== "" &&
    typeof f.kind === "string" &&
    typeof f.path === "string" &&
    f.path.startsWith(".") &&
    typeof f.abs === "string" &&
    f.abs === join(home, f.path)
  );
}

/** The link, or null when this machine's home isn't linked. */
export function readHomeLink(paths: DaemonPaths): HomeLink | null {
  const raw = readJson<Partial<HomeLink> | null>(paths.home, null);
  if (
    !raw ||
    raw.version !== 1 ||
    typeof raw.space_id !== "string" ||
    raw.space_id === "" ||
    typeof raw.space_slug !== "string" ||
    typeof raw.machine_id !== "string" ||
    raw.machine_id === "" ||
    typeof raw.home !== "string" ||
    !raw.home.startsWith("/") ||
    !Array.isArray(raw.files)
  ) {
    return null;
  }
  const home = raw.home;
  return {
    version: 1,
    space_id: raw.space_id,
    space_slug: raw.space_slug,
    machine_id: raw.machine_id,
    home,
    files: raw.files.filter((f) => isHomeFile(home, f)),
    created_at: typeof raw.created_at === "string" ? raw.created_at : "",
  };
}

export function writeHomeLink(
  paths: DaemonPaths,
  link: Omit<HomeLink, "version" | "created_at">,
  now: Date = new Date(),
): HomeLink {
  ensureDaemonDir(paths);
  const previous = readHomeLink(paths);
  const created_at =
    previous && previous.machine_id === link.machine_id
      ? previous.created_at
      : now.toISOString();
  const next: HomeLink = {
    version: 1,
    ...link,
    files: [...link.files].sort((a, b) => a.path.localeCompare(b.path)),
    created_at,
  };
  writeJsonAtomic(paths.home, next);
  return next;
}

/** Takes the link away; true when there was one. */
export function removeHomeLink(paths: DaemonPaths): boolean {
  try {
    unlinkSync(paths.home);
    return true;
  } catch {
    return false;
  }
}

/** Changes whenever home.json is written or removed (see registryStamp). */
export function homeLinkStamp(paths: DaemonPaths): string {
  try {
    const st = statSync(paths.home);
    return `${st.ino}:${st.mtimeMs}:${st.size}`;
  } catch {
    return "none";
  }
}
