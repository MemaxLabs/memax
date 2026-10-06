// Small JSON files under ~/.memax/daemon, written atomically.
import { randomBytes } from "node:crypto";
import { readFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";

/** The parsed file, or `fallback` when it is missing or unreadable. */
export function readJson<T>(path: string, fallback: T): T {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return fallback;
  }
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

/** Writes through a temp file and a rename, so a reader never sees half. */
export function writeJsonAtomic(path: string, value: unknown): void {
  const tmp = `${path}.${randomBytes(4).toString("hex")}.tmp`;
  try {
    writeFileSync(tmp, JSON.stringify(value, null, 2) + "\n", {
      mode: 0o600,
    });
    renameSync(tmp, path);
  } catch (err) {
    try {
      unlinkSync(tmp);
    } catch {
      // never created
    }
    throw err;
  }
}
