// Reads a compiled file the way the session-start hook needs it: which
// compile (C-) its header names, and its cited lines (`- … [M-0219]`),
// each with a short hash so the next session can tell what changed
// without keeping the words. In a file the person owns, only Memax's
// managed block counts. Node built-ins only: this is on the hook's
// critical path.
import { closeSync, openSync, readSync } from "node:fs";
import { cleanLine } from "../daemon/compiler/sanitize.js";
import { shortHash } from "./hash.js";

/** The compiler's header (packages/compiler/src/document.ts headerText). */
const HEADER =
  /<!-- Compiled by Memax from (\S+) at [^>]*?\((C-\d{1,18})\)[^>]*-->/;
/** Trailing cites, as the compiler's parse-back reads them. */
const CITES = /\s*\[(M-\d+(?:\s*,\s*M-\d+)*)\]$/;
const START = "<!-- memax:start -->";
const END = "<!-- memax:end -->";
/** AGENTS.md is held to 32 KiB; a person's own file may be bigger. */
const MAX_BYTES = 256 * 1024;

export interface CitedLine {
  /** The cites, joined (`M-0431,M-0174`), with `#n` for a repeat. */
  key: string;
  refs: string[];
  /** The line as it reads, cleaned of hidden characters. */
  text: string;
  hash: string;
}

export interface CompiledFile {
  /** Repository-relative. */
  path: string;
  /** The compile its header names. */
  compile: string;
  /** The space slug its header names. */
  space: string;
  lines: CitedLine[];
}

function readHead(abs: string): string | null {
  let fd: number;
  try {
    fd = openSync(abs, "r");
  } catch {
    return null;
  }
  try {
    const buf = Buffer.allocUnsafe(MAX_BYTES);
    let n = 0;
    while (n < MAX_BYTES) {
      const got = readSync(fd, buf, n, MAX_BYTES - n, null);
      if (got === 0) break;
      n += got;
    }
    return buf.subarray(0, n).toString("utf8");
  } catch {
    return null;
  } finally {
    closeSync(fd);
  }
}

export function lineHash(text: string): string {
  return shortHash(text);
}

/** Parses compiled content; null when it carries no Memax header. */
export function parseCompiled(
  path: string,
  content: string,
): CompiledFile | null {
  let lines = content.split(/\r\n|\r|\n/);
  const start = lines.findIndex((l) => l.trim() === START);
  const end = lines.findIndex((l) => l.trim() === END);
  if (start >= 0 && end > start) lines = lines.slice(start + 1, end);
  let header: RegExpExecArray | null = null;
  const out: CitedLine[] = [];
  const seen = new Map<string, number>();
  for (const raw of lines) {
    const line = raw.trim();
    if (!header) {
      const h = HEADER.exec(line);
      if (h) {
        header = h;
        continue;
      }
    }
    if (line === "" || line.startsWith("#") || line.startsWith("<!--"))
      continue;
    const cites = CITES.exec(line);
    if (!cites) continue;
    const refs = cites[1].split(",").map((r) => r.trim());
    const base = refs.join(",");
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    const text = cleanLine(line).text;
    out.push({
      key: n === 1 ? base : `${base}#${n}`,
      refs,
      text,
      hash: lineHash(text),
    });
  }
  if (!header) return null;
  return { path, compile: header[2], space: header[1], lines: out };
}

/** Reads and parses one compiled file; null when it is missing or not Memax's. */
export function readCompiled(abs: string, path: string): CompiledFile | null {
  const content = readHead(abs);
  return content === null ? null : parseCompiled(path, content);
}
