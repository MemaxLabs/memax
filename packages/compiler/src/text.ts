/** Small formatting helpers. None of them depend on the locale or time zone. */

const ISO_TIME =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?(?:Z|[+-]\d{2}:\d{2})$/;

/** Milliseconds since the epoch for an ISO 8601 time with a zone, or null. */
export function parseTime(value: unknown): number | null {
  if (typeof value !== "string" || !ISO_TIME.test(value)) return null;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : null;
}

/** `2026-10-05 14:31 UTC`. */
export function formatStamp(ms: number): string {
  return `${new Date(ms).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

/** `31.2 KiB`, or `32 KiB` when whole. */
export function kib(bytes: number): string {
  const value = (bytes / 1024).toFixed(1);
  return `${value.endsWith(".0") ? value.slice(0, -2) : value} KiB`;
}

/** `12,000`. */
export function thousands(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/** `1 fact`, `3 facts`. */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** A POSIX path relative to the repository root: no `..`, no empty segments. */
export function isRepoPath(path: string): boolean {
  if (path === "" || path.length > 300 || !/^[A-Za-z0-9._\-/]+$/.test(path))
    return false;
  return path
    .split("/")
    .every((seg) => seg !== "" && seg !== "." && seg !== "..");
}

/** The last segment of a path. */
export function basename(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** The path from the directory holding `from` to the file `to`. */
export function relativePath(from: string, to: string): string {
  const fromDir = from.split("/").slice(0, -1);
  const toParts = to.split("/");
  let common = 0;
  while (
    common < fromDir.length &&
    common < toParts.length - 1 &&
    fromDir[common] === toParts[common]
  ) {
    common += 1;
  }
  const up = fromDir.slice(common).map(() => "..");
  return [...up, ...toParts.slice(common)].join("/");
}

/** Compare strings by UTF-16 code units, the same everywhere. */
export function byCodeUnit(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}
