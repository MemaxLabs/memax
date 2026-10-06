/**
 * One managed block inside a file the person owns.
 *
 * When someone keeps their own CLAUDE.md (or GEMINI.md), Memax doesn't take
 * the file over. It manages exactly one block between two marker lines and
 * leaves every byte outside it alone, line endings included. Upserting is
 * idempotent. The markers are the ones the V1 CLI already writes, so a V2
 * compile replaces a V1 instruction block in place.
 */

export const MANAGED_START = "<!-- memax:start -->";
export const MANAGED_END = "<!-- memax:end -->";

/** The file's markers don't form exactly one start/end pair. */
export class ManagedBlockError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ManagedBlockError";
  }
}

interface RawLine {
  text: string;
  /** The line's own terminator: `\n`, `\r\n`, `\r`, or `""` at the end of the file. */
  eol: string;
}

function splitKeep(content: string): RawLine[] {
  const lines: RawLine[] = [];
  const re = /([^\r\n]*)(\r\n|\r|\n|$)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(content)) !== null) {
    if (m[0] === "") break;
    lines.push({ text: m[1], eol: m[2] });
  }
  return lines;
}

/** The marker lines' 0-based indexes, or null when the file has no block. */
function locate(lines: RawLine[]): { start: number; end: number } | null {
  const starts: number[] = [];
  const ends: number[] = [];
  lines.forEach((l, i) => {
    if (l.text.trim() === MANAGED_START) starts.push(i);
    if (l.text.trim() === MANAGED_END) ends.push(i);
  });
  if (starts.length === 0 && ends.length === 0) return null;
  if (starts.length !== 1 || ends.length !== 1 || ends[0] < starts[0]) {
    throw new ManagedBlockError(
      `the file needs exactly one ${MANAGED_START} line followed by one ${MANAGED_END} line; fix or remove the markers by hand`,
    );
  }
  return { start: starts[0], end: ends[0] };
}

/**
 * The block's 1-based line numbers (marker lines included), or null if the
 * file has none. Throws {@link ManagedBlockError} for broken markers.
 */
export function findManagedBlock(
  content: string,
): { start: number; end: number } | null {
  const at = locate(splitKeep(content));
  return at ? { start: at.start + 1, end: at.end + 1 } : null;
}

/** The block's text with LF line endings, markers included, or null. */
export function extractManagedBlock(content: string): string | null {
  const lines = splitKeep(content);
  const at = locate(lines);
  if (!at) return null;
  return `${lines
    .slice(at.start, at.end + 1)
    .map((l) => l.text.trimEnd())
    .join("\n")}\n`;
}

/**
 * Writes `inner` (lines, without markers) as the managed block: in place if
 * the file has one, otherwise appended after a blank line.
 */
export function upsertManagedBlock(content: string, inner: string[]): string {
  const lines = splitKeep(content);
  const at = locate(lines);
  const eol = lines.find((l) => l.eol !== "")?.eol ?? "\n";
  const block = [MANAGED_START, ...inner, MANAGED_END];

  if (at) {
    const before = lines
      .slice(0, at.start)
      .map((l) => l.text + l.eol)
      .join("");
    const after = lines
      .slice(at.end + 1)
      .map((l) => l.text + l.eol)
      .join("");
    return before + block.join(eol) + lines[at.end].eol + after;
  }
  if (content === "") return block.join(eol) + eol;
  let out = content;
  if (!/(\r\n|\r|\n)$/.test(out)) out += eol;
  if (!/(\r\n|\r|\n)(\r\n|\r|\n)$/.test(out)) out += eol;
  return out + block.join(eol) + eol;
}

/** Removes the managed block, leaving everything else exactly as it was. */
export function removeManagedBlock(content: string): string {
  const lines = splitKeep(content);
  const at = locate(lines);
  if (!at) return content;
  return [...lines.slice(0, at.start), ...lines.slice(at.end + 1)]
    .map((l) => l.text + l.eol)
    .join("");
}
