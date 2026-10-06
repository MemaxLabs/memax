// The drift hashes the daemon compares, computed exactly as the compiler
// (packages/compiler/src/parse.ts `driftHash`) and the server
// (`ledger.ManifestSHA256`) compute them. test/daemon/compiler-parity.test.ts
// holds them to the compiler's own functions.
import { createHash } from "node:crypto";
import { extractManagedBlock, findManagedBlock } from "./managed-block.js";

export function sha256Hex(text: string): string {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

/** Line endings and a BOM don't count as edits. */
function normalize(content: string): string {
  return content.replace(/^\u{FEFF}/u, "").replace(/\r\n?/g, "\n");
}

/**
 * The hash drift checks compare: the managed block's, when the file has
 * one, otherwise the whole content's.
 */
export function driftHash(content: string): string {
  const normalized = normalize(content);
  let block: string | null = null;
  try {
    block = extractManagedBlock(normalized);
  } catch {
    // Broken markers: the block can't be found, so the whole file counts.
  }
  return sha256Hex(block ?? normalized);
}

/** Whether a file holds a managed block: none, one, or broken markers. */
export function blockState(content: string): "none" | "present" | "broken" {
  try {
    return findManagedBlock(content) ? "present" : "none";
  } catch {
    return "broken";
  }
}

/**
 * The lines inside a compiled file's managed block, markers excluded, or
 * null when it has none.
 */
export function managedInner(content: string): string[] | null {
  let block: string | null;
  try {
    block = extractManagedBlock(content);
  } catch {
    return null;
  }
  if (block === null) return null;
  return block.slice(0, -1).split("\n").slice(1, -1);
}

/**
 * A run's drift hash over its files (path → drift hash): the file's own
 * hash for one file, otherwise the sha256 of `<path> NUL <hash> LF` lines
 * sorted by path. Paths are ASCII (RepoPath), so code-unit order is byte
 * order.
 */
export function manifestSha256(entries: Record<string, string>): string {
  const keys = Object.keys(entries);
  if (keys.length === 1) return entries[keys[0]];
  keys.sort();
  const sum = createHash("sha256");
  for (const k of keys) sum.update(`${k}\u0000${entries[k]}\n`, "utf8");
  return sum.digest("hex");
}
