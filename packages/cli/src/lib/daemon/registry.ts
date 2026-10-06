// The repositories linked on this device (~/.memax/daemon/repos.json),
// written by `memax link` and read by the daemon.
import { statSync } from "node:fs";
import { readJson, writeJsonAtomic } from "./json-file.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";

export interface LinkedRepo {
  /** The repository's root, a real path (symlinks resolved). */
  root: string;
  space_id: string;
  space_slug: string;
  created_at: string;
}

interface RegistryFile {
  version: 1;
  repos: LinkedRepo[];
}

function isLinkedRepo(v: unknown): v is LinkedRepo {
  const r = v as LinkedRepo;
  return (
    typeof r === "object" &&
    r !== null &&
    typeof r.root === "string" &&
    r.root.startsWith("/") &&
    typeof r.space_id === "string" &&
    r.space_id !== "" &&
    typeof r.space_slug === "string" &&
    typeof r.created_at === "string"
  );
}

export function readRegistry(paths: DaemonPaths): LinkedRepo[] {
  const file = readJson<Partial<RegistryFile>>(paths.repos, {});
  return Array.isArray(file.repos) ? file.repos.filter(isLinkedRepo) : [];
}

/** The registry's modification time in ms, or 0 when there is none. */
export function registryStamp(paths: DaemonPaths): number {
  try {
    return statSync(paths.repos).mtimeMs;
  } catch {
    return 0;
  }
}

function write(paths: DaemonPaths, repos: LinkedRepo[]): void {
  ensureDaemonDir(paths);
  const sorted = [...repos].sort((a, b) => a.root.localeCompare(b.root));
  writeJsonAtomic(paths.repos, { version: 1, repos: sorted });
}

/**
 * Links `root` to a space, replacing any earlier link of the same root.
 * Returns the previous entry, if there was one.
 */
export function linkRepo(
  paths: DaemonPaths,
  entry: Omit<LinkedRepo, "created_at">,
  now: Date = new Date(),
): LinkedRepo | undefined {
  const repos = readRegistry(paths);
  const previous = repos.find((r) => r.root === entry.root);
  const created_at =
    previous && previous.space_id === entry.space_id
      ? previous.created_at
      : now.toISOString();
  write(paths, [
    ...repos.filter((r) => r.root !== entry.root),
    { ...entry, created_at },
  ]);
  return previous;
}

/** Removes `root` from the registry; returns the entry it removed. */
export function unlinkRepo(
  paths: DaemonPaths,
  root: string,
): LinkedRepo | undefined {
  const repos = readRegistry(paths);
  const previous = repos.find((r) => r.root === root);
  if (previous) {
    write(
      paths,
      repos.filter((r) => r.root !== root),
    );
  }
  return previous;
}

export function findLinkedRepo(
  paths: DaemonPaths,
  root: string,
): LinkedRepo | undefined {
  return readRegistry(paths).find((r) => r.root === root);
}
