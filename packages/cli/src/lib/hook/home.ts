// This machine's home link as the session-start hook reads it
// (~/.memax/daemon/home.json, written by memax link --home; the full
// reader is lib/daemon/home-link.ts, which the hook doesn't load). Node
// built-ins only: this is on the hook's critical path.
import { readFileSync } from "node:fs";

export interface HookHomeFile {
  target_id: string;
  kind: string;
  /** Relative to the home folder: `.claude/rules/memax-personal.md`. */
  path: string;
  abs: string;
}

export interface HookHome {
  space_id: string;
  space_slug: string;
  machine_id: string;
  files: HookHomeFile[];
}

/** The link, or null when this machine's home isn't linked (or the file is odd). */
export function readHookHome(path: string): HookHome | null {
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as Partial<HookHome> & {
      version?: number;
    };
    if (
      raw.version !== 1 ||
      typeof raw.space_id !== "string" ||
      typeof raw.space_slug !== "string" ||
      typeof raw.machine_id !== "string" ||
      !Array.isArray(raw.files)
    )
      return null;
    return {
      space_id: raw.space_id,
      space_slug: raw.space_slug,
      machine_id: raw.machine_id,
      files: raw.files.filter(
        (f): f is HookHomeFile =>
          typeof f?.target_id === "string" &&
          typeof f.kind === "string" &&
          typeof f.path === "string" &&
          typeof f.abs === "string" &&
          f.abs.startsWith("/"),
      ),
    };
  } catch {
    return null;
  }
}
