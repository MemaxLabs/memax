/**
 * Parse-back: from a hand-edited file to proposals.
 *
 * Memax never writes over a file whose hash isn't the last delivered hash.
 * When a person edits a compiled file, {@link parseBack} compares it with the
 * last compile and says what the edit means:
 *
 * - a cited line whose words changed → `edit` that memory;
 * - a line with no matching cite → `new` proposal;
 * - a cited line that's gone → `remove`: a proposal to forget or exclude,
 *   never an automatic forget;
 * - lines that only moved → nothing.
 *
 * Lines are matched by their `[M-…]` cites, not by position. Line endings,
 * trailing whitespace, list markers (`-`, `*`, `+`, `1.`), indented
 * continuation lines and Memax's own markers (`(Being verified)`) don't
 * count as edits. Hidden characters are stripped before anything is
 * compared or proposed.
 */
import { HEADER_LINE } from "./document.js";
import { sha256Hex } from "./hash.js";
import {
  extractManagedBlock,
  MANAGED_END,
  MANAGED_START,
} from "./managed-block.js";
import { cleanLine } from "./sanitize.js";
import type {
  Change,
  ChangeSet,
  CompiledFile,
  DriftInfo,
  ParsedFile,
  ParsedLine,
} from "./types.js";

const ITEM = /^\s*(?:[-*+]|\d{1,9}[.)])\s+(.*)$/;
const HEADING = /^(#{1,6})\s+(.*?)(?:\s+#+)?\s*$/;
const CITES = /\s*\[(M-\d+(?:\s*,\s*M-\d+)*)\]$/;
const MARKER = /^\((Being verified|In conflict)\)\s+/;
const SCOPE_HEADING = /^In (`[^`]+`(?:, `[^`]+`)*)$/;
const FENCE = /^\s*(```|~~~)/;

function splitLines(content: string): string[] {
  const lines = content.replace(/^\u{FEFF}/u, "").split(/\r\n|\r|\n/);
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** Globs named by `globs:`, `applyTo:` or a `paths:` list. */
function frontmatterPaths(lines: string[]): string[] {
  const paths: string[] = [];
  const unquote = (s: string) => s.trim().replace(/^["']|["']$/g, "");
  let inPaths = false;
  for (const line of lines) {
    const field = /^(globs|applyTo|paths):\s*(.*)$/.exec(line);
    if (field) {
      inPaths = field[1] === "paths" && field[2] === "";
      paths.push(...unquote(field[2]).split(",").map(unquote));
    } else if (inPaths && /^\s+-\s+/.test(line)) {
      paths.push(unquote(line.replace(/^\s+-\s+/, "")));
    } else {
      inPaths = false;
    }
  }
  return [...new Set(paths.filter(Boolean))].sort();
}

/** Parses a compiled (or hand-edited) markdown file into classified lines. */
export function parseFile(content: string): ParsedFile {
  const raw = splitLines(content);
  const lines: ParsedLine[] = [];
  let hidden = 0;
  let header: string | null = null;
  let frontmatter: string[] | null = null;
  let filePaths: string[] = [];
  let start = 0;

  if (raw[0]?.trimEnd() === "---") {
    const close = raw.findIndex((l, i) => i > 0 && l.trimEnd() === "---");
    if (close > 0) {
      frontmatter = raw.slice(1, close).map((l) => l.trimEnd());
      filePaths = frontmatterPaths(frontmatter);
      for (let i = 0; i <= close; i++) {
        hidden += cleanLine(raw[i]).removed;
        lines.push(line(i + 1, "frontmatter", raw[i].trim(), null, []));
      }
      start = close + 1;
    }
  }

  const clean = (s: string) => cleanLine(s).text;
  const markers: number[] = [];
  const ends: number[] = [];
  let section: string | null = null;
  let scope: string[] | null = null;
  let inFence = false;
  /** The raw words of the last item, while indented lines continue it. */
  let itemRaw: string | null = null;

  for (let i = start; i < raw.length; i++) {
    const n = i + 1;
    const text = raw[i].trimEnd();
    hidden += cleanLine(text).removed;

    // An indented line right under an item continues it.
    if (
      itemRaw !== null &&
      /^\s+\S/.test(text) &&
      !ITEM.test(text) &&
      !FENCE.test(text)
    ) {
      itemRaw = `${itemRaw} ${text.trim()}`;
      const prev = lines[lines.length - 1];
      lines[lines.length - 1] = textLine(
        prev.line,
        "item",
        clean(itemRaw),
        prev.section,
        prev.paths,
      );
      continue;
    }
    itemRaw = null;

    if (FENCE.test(text)) {
      inFence = !inFence;
      lines.push(line(n, "fence", text.trim(), section, scope ?? filePaths));
      continue;
    }
    if (inFence) {
      lines.push(line(n, "text", clean(text), section, scope ?? filePaths));
      continue;
    }
    if (text.trim() === "") {
      lines.push(line(n, "blank", "", section, scope ?? filePaths));
      continue;
    }
    if (text.trim() === MANAGED_START || text.trim() === MANAGED_END) {
      (text.trim() === MANAGED_START ? markers : ends).push(n);
      lines.push(line(n, "marker", text.trim(), section, []));
      continue;
    }
    if (header === null && HEADER_LINE.test(text.trim())) {
      header = text.trim();
      lines.push(line(n, "header", header, section, []));
      continue;
    }
    if (/^\s*<!--.*-->$/.test(text)) {
      lines.push(line(n, "comment", text.trim(), section, scope ?? filePaths));
      continue;
    }
    const heading = HEADING.exec(text);
    if (heading) {
      const title = clean(heading[2]);
      if (heading[1].length <= 2) {
        section = heading[1].length === 2 ? title : null;
        scope = null;
      } else if (heading[1].length === 3) {
        const m = SCOPE_HEADING.exec(title);
        scope = m ? m[1].split(", ").map((p) => p.slice(1, -1)) : null;
      }
      lines.push(line(n, "heading", title, section, scope ?? filePaths));
      continue;
    }
    if (/^@\S+$/.test(text)) {
      lines.push(line(n, "import", text, section, []));
      continue;
    }
    const item = ITEM.exec(text);
    if (item) {
      itemRaw = item[1];
      lines.push(
        textLine(n, "item", clean(itemRaw), section, scope ?? filePaths),
      );
      continue;
    }
    lines.push(textLine(n, "text", clean(text), section, scope ?? filePaths));
  }

  const managed =
    markers.length === 1 && ends.length === 1 && markers[0] < ends[0]
      ? { start: markers[0] + 1, end: ends[0] - 1 }
      : null;
  return { lines, header, frontmatter, managed, hidden_characters: hidden };
}

function line(
  n: number,
  kind: ParsedLine["kind"],
  text: string,
  section: string | null,
  paths: string[],
): ParsedLine {
  return { line: n, kind, text, refs: [], marker: null, section, paths };
}

/** An item or text line: split off the trailing cites and, for items, the marker. */
function textLine(
  n: number,
  kind: "item" | "text",
  text: string,
  section: string | null,
  paths: string[],
): ParsedLine {
  const parsed = line(n, kind, text, section, paths);
  const cites = CITES.exec(text);
  if (cites) {
    parsed.refs = cites[1].split(",").map((r) => r.trim());
    parsed.text = text.slice(0, cites.index).trim();
  }
  if (kind === "item") {
    const marker = MARKER.exec(parsed.text);
    if (marker) {
      parsed.marker = marker[1];
      parsed.text = parsed.text.slice(marker[0].length);
    }
  }
  return parsed;
}

// ---------------------------------------------------------------------------
// Change sets
// ---------------------------------------------------------------------------

const isContent = (l: ParsedLine) =>
  (l.kind === "item" || l.kind === "text") && l.text !== "";
const isLayout = (l: ParsedLine) =>
  l.kind === "heading" ||
  l.kind === "import" ||
  l.kind === "comment" ||
  l.kind === "fence";

/** Lines as compared for "changed": no BOM, no trailing whitespace, no trailing blank lines. */
function comparable(
  content: string,
  range: { start: number; end: number } | null,
): string[] {
  const lines = splitLines(content).map((l) => l.trimEnd());
  const region = range ? lines.slice(range.start - 1, range.end) : lines;
  while (region.length > 0 && region[region.length - 1] === "") region.pop();
  return region;
}

/**
 * Compares the last compiled file with what's on disk now and returns the
 * proposals the edit implies, plus drift metadata. Pass the compiled text or
 * the {@link CompiledFile} it came from.
 */
export function parseBack(
  last: string | CompiledFile,
  current: string,
): ChangeSet {
  const lastContent = typeof last === "string" ? last : last.content;
  const before = parseFile(lastContent);
  const after = parseFile(current);

  let oldLines = before.lines;
  let newLines = after.lines;
  let managedBlock: DriftInfo["managed_block"] = null;
  if (before.managed) {
    if (!after.managed) {
      const removed = comparable(current, null);
      return {
        changes: [],
        drift: {
          changed: true,
          header_edited: false,
          frontmatter_edited: false,
          layout_edited: false,
          managed_block: "removed",
          hidden_characters: removed.reduce(
            (n, l) => n + cleanLine(l).removed,
            0,
          ),
        },
      };
    }
    const within = (r: { start: number; end: number }) => (l: ParsedLine) =>
      l.line >= r.start && l.line <= r.end;
    oldLines = oldLines.filter(within(before.managed));
    newLines = newLines.filter(within(after.managed));
  }

  const oldRegion = comparable(lastContent, before.managed);
  const newRegion = comparable(current, after.managed);
  const changed = oldRegion.join("\n") !== newRegion.join("\n");
  if (before.managed) managedBlock = changed ? "edited" : "intact";

  const changes = diff(oldLines, newLines);
  const layoutLeft = multiset(oldLines.filter(isLayout).map((l) => l.text));
  const layoutChanged =
    newLines.filter(isLayout).some((l) => !take(layoutLeft, l.text)) ||
    layoutLeft.size > 0 ||
    changes.layout;

  return {
    changes: changes.list,
    drift: {
      changed,
      header_edited: before.header !== after.header,
      frontmatter_edited:
        JSON.stringify(before.frontmatter) !==
        JSON.stringify(after.frontmatter),
      layout_edited: layoutChanged,
      managed_block: managedBlock,
      hidden_characters: newRegion.reduce(
        (n, l) => n + cleanLine(l).removed,
        0,
      ),
    },
  };
}

function diff(
  oldLines: ParsedLine[],
  newLines: ParsedLine[],
): { list: Change[]; layout: boolean } {
  const oldContent = oldLines.filter(isContent);
  const newContent = newLines.filter(isContent);
  const key = (l: ParsedLine) => l.refs.join(",");

  // Cited lines pair up by their cites, in order.
  const queues = new Map<string, ParsedLine[]>();
  for (const l of newContent.filter((l) => l.refs.length > 0)) {
    queues.set(key(l), [...(queues.get(key(l)) ?? []), l]);
  }
  const paired = new Set<ParsedLine>();
  const edits: Change[] = [];
  const gone: ParsedLine[] = [];
  for (const o of oldContent.filter((l) => l.refs.length > 0)) {
    const n = queues.get(key(o))?.shift();
    if (!n) {
      gone.push(o);
      continue;
    }
    paired.add(n);
    if (n.text !== o.text) {
      edits.push({
        kind: "edit",
        ref: o.refs[0],
        refs: o.refs,
        old_text: o.text,
        new_text: n.text,
        old_line: o.line,
        new_line: n.line,
      });
    }
  }

  // Uncited lines compare as a multiset, so moving them changes nothing.
  const oldUncited = multiset(
    oldContent.filter((l) => l.refs.length === 0).map((l) => l.text),
  );
  const added = newContent.filter((l) => {
    if (paired.has(l)) return false;
    if (l.refs.length > 0) return true;
    return !take(oldUncited, l.text);
  });

  // A cited line whose words survive without the cite wasn't removed.
  const removes: Change[] = [];
  for (const o of gone) {
    const same = added.findIndex(
      (l) => l.refs.length === 0 && l.text === o.text,
    );
    if (same >= 0) {
      added.splice(same, 1);
      continue;
    }
    removes.push({
      kind: "remove",
      ref: o.refs[0],
      refs: o.refs,
      old_text: o.text,
      old_line: o.line,
    });
  }

  const news: Change[] = added.map((l) => ({
    kind: "new",
    text: l.text,
    line: l.line,
    section: l.section,
    paths: l.paths,
    cites: l.refs,
  }));
  const at = (c: Change) =>
    c.kind === "edit" ? c.new_line : c.kind === "new" ? c.line : c.old_line;
  return {
    list: [
      ...[...edits, ...news].sort((a, b) => at(a) - at(b)),
      ...removes.sort((a, b) => at(a) - at(b)),
    ],
    layout: oldUncited.size > 0,
  };
}

function multiset(values: string[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const v of values) counts.set(v, (counts.get(v) ?? 0) + 1);
  return counts;
}

/** Takes one `value` out of the multiset; false if there was none. */
function take(counts: Map<string, number>, value: string): boolean {
  const n = counts.get(value) ?? 0;
  if (n === 0) return false;
  if (n === 1) counts.delete(value);
  else counts.set(value, n - 1);
  return true;
}

// ---------------------------------------------------------------------------
// Drift checks
// ---------------------------------------------------------------------------

const normalize = (content: string) =>
  content.replace(/^\u{FEFF}/u, "").replace(/\r\n?/g, "\n");

/**
 * The hash drift checks compare: the managed block's, when the file has one,
 * otherwise the whole content's. Line endings and a BOM don't count.
 */
export function driftHash(content: string): string {
  const normalized = normalize(content);
  let block: string | null = null;
  try {
    block = extractManagedBlock(normalized);
  } catch {
    // Broken markers: the managed region can't be found, so hash it all.
  }
  return sha256Hex(block ?? normalized);
}

/**
 * Whether a file changed since Memax last delivered it. Pass the delivered
 * file's `drift_sha256` (for files Memax owns, that's also `sha256`).
 */
export function isDrifted(
  lastDeliveredSha: string,
  currentContent: string,
): boolean {
  if (sha256Hex(normalize(currentContent)) === lastDeliveredSha) return false;
  return driftHash(currentContent) !== lastDeliveredSha;
}
