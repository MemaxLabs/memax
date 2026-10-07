// Which V2 space a command is about: `--space`, the repository's
// `.memax.yml`, or its link on this device; for `memax link`, also the
// space whose repository matches the git remote, or a choice.
import type { Memax, V2 } from "memax-sdk";
import { findLinkedRepo } from "./daemon/registry.js";
import type { DaemonPaths } from "./daemon/paths.js";
import { ask } from "./prompt.js";
import {
  detectProjectContext,
  gitRoot,
  normalizeRepoUrl,
  readMemaxYmlConfig,
} from "./project-context.js";

export class SpaceChoiceError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SpaceChoiceError";
  }
}

export interface ResolvedSpace {
  space: V2.Space;
  /** The repository's root (git top level), when the command runs in one. */
  root: string | null;
  source: "flag" | "memax_yml" | "linked" | "repository" | "chosen";
}

export interface ResolveOptions {
  space?: string;
  cwd?: string;
  paths: DaemonPaths;
  /** `memax link`: fall back to the git remote, then a prompt. */
  pick?: boolean;
}

function list(spaces: V2.Space[]): string {
  return spaces.map((s) => s.slug).join(", ") || "none yet";
}

export function findSpace(
  spaces: V2.Space[],
  key: string,
): V2.Space | undefined {
  return spaces.find((s) => s.id === key || s.slug === key);
}

export async function resolveSpace(
  memax: Memax,
  o: ResolveOptions,
): Promise<ResolvedSpace> {
  const cwd = o.cwd ?? process.cwd();
  const root = gitRoot(cwd);
  const spaces = (await memax.v2.spaces.list()).items;
  const named: Array<[string | undefined, ResolvedSpace["source"]]> = [
    [o.space, "flag"],
    [readMemaxYmlConfig(cwd)?.space, "memax_yml"],
    [root ? findLinkedRepo(o.paths, root)?.space_id : undefined, "linked"],
  ];
  for (const [key, source] of named) {
    if (!key) continue;
    const space = findSpace(spaces, key);
    if (!space) {
      const where = source === "memax_yml" ? " (from .memax.yml)" : "";
      throw new SpaceChoiceError(
        `${key}${where} isn't one of your spaces. Your spaces: ${list(spaces)}.`,
      );
    }
    return { space, root, source };
  }
  if (!o.pick) {
    throw new SpaceChoiceError(
      `Say which space: --space <slug>, or link this repository with memax link. Your spaces: ${list(spaces)}.`,
    );
  }
  const remote = detectProjectContext(cwd).repo;
  if (remote) {
    const repo = normalizeRepoUrl(remote);
    const matches = spaces.filter(
      (s) => s.repository && normalizeRepoUrl(s.repository) === repo,
    );
    if (matches.length === 1)
      return { space: matches[0], root, source: "repository" };
  }
  const projects = spaces.filter((s) => s.kind !== "personal");
  if (!process.stdin.isTTY || projects.length === 0) {
    throw new SpaceChoiceError(
      `Say which space: memax link --space <slug>. Your spaces: ${list(spaces)}.`,
    );
  }
  console.log("");
  projects.forEach((s, i) =>
    console.log(
      `  ${i + 1}. ${s.slug}  ${s.name !== s.slug ? s.name : ""}`.trimEnd(),
    ),
  );
  const answer = (
    await ask("\n  Link this repository to which space? ")
  ).trim();
  const picked = projects[Number(answer) - 1] ?? findSpace(projects, answer);
  if (!picked)
    throw new SpaceChoiceError(
      "No space picked. Run memax link --space <slug>.",
    );
  return { space: picked, root, source: "chosen" };
}
