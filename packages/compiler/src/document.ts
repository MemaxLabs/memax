/**
 * Turning a selection into a file: fit it to the budget, then render.
 *
 * A file is a head (frontmatter, header, title), sections of `- line [M-id]`
 * items with optional `### In <globs>` subsections, and a tail (Live
 * context). Every part's size is additive, so the fill can be exact: it
 * walks the entries round-robin by rank (every section's best line, then
 * every section's second-best, and so on) and keeps each one that still
 * fits. Sections share the budget, and the most-read facts in each go first.
 */
import { byteLength } from "./hash.js";
import type { Model } from "./model.js";
import type { Entry, Selection } from "./select.js";
import { byCodeUnit } from "./text.js";

export type Style = "markdown" | "plain";

export interface Layout {
  style: Style;
  /** Lines before the sections. */
  head: string[];
  /** Lines after the sections; a blank line goes in between. */
  tail: string[];
}

export interface Limit {
  bytes: number;
  /** A character cap as well, for tools that count characters. */
  chars: number | null;
}

export interface Document {
  content: string;
  included: Entry[];
  dropped: Entry[];
}

interface Size {
  bytes: number;
  chars: number;
}

/** The size of lines once joined with `\n`, each counted with its newline. */
function measure(lines: string[]): Size {
  let bytes = 0;
  let chars = 0;
  for (const line of lines) {
    bytes += byteLength(line) + 1;
    chars += line.length + 1;
  }
  return { bytes, chars };
}

/** `- (Being verified) Ask memax answers with the Haiku tier. [M-0187]` */
export function itemLine(entry: Entry): string {
  const marker = entry.marker ? `(${entry.marker}) ` : "";
  return `- ${marker}${entry.text} [${entry.refs.join(", ")}]`;
}

function sectionLines(style: Style, heading: string): string[] {
  return ["", style === "markdown" ? `## ${heading}` : heading];
}

/** The subsection for path-scoped lines inside a shared file. */
export function scopeHeading(style: Style, paths: string[]): string {
  return style === "markdown"
    ? `### In ${paths.map((p) => `\`${p}\``).join(", ")}`
    : `In ${paths.join(", ")}`;
}

/** Fits a selection to a limit and renders it. */
export function compose(
  selection: Selection,
  layout: Layout,
  limit: Limit,
): Document {
  const { style, head, tail } = layout;
  const used = measure(head);
  if (tail.length > 0) add(used, measure(["", ...tail]));

  const openSections = new Set<number>();
  const openGroups = new Set<string>();
  const included: Entry[] = [];
  const dropped: Entry[] = [];

  const queue = [...selection.entries].sort(
    (a, b) => a.rank - b.rank || a.section - b.section || a.order - b.order,
  );
  for (const entry of queue) {
    const groupId = `${entry.section}\n${entry.group}`;
    const cost = measure([itemLine(entry)]);
    if (!openSections.has(entry.section)) {
      add(
        cost,
        measure(sectionLines(style, selection.sections[entry.section].heading)),
      );
    }
    if (entry.group !== "" && !openGroups.has(groupId)) {
      add(cost, measure(["", scopeHeading(style, entry.paths)]));
    }
    const fits =
      used.bytes + cost.bytes <= limit.bytes &&
      (limit.chars === null || used.chars + cost.chars <= limit.chars);
    if (!fits) {
      dropped.push(entry);
      continue;
    }
    add(used, cost);
    openSections.add(entry.section);
    openGroups.add(groupId);
    included.push(entry);
  }

  const content = render(selection, included, layout);
  if (byteLength(content) !== used.bytes) {
    throw new Error(
      "compiler bug: the rendered size doesn't match the budget's count",
    );
  }
  return { content, included, dropped };
}

function add(total: Size, more: Size): void {
  total.bytes += more.bytes;
  total.chars += more.chars;
}

function render(
  selection: Selection,
  included: Entry[],
  layout: Layout,
): string {
  const lines = [...layout.head];
  selection.sections.forEach((section, index) => {
    const entries = included
      .filter((e) => e.section === index)
      .sort((a, b) => a.order - b.order);
    if (entries.length === 0) return;
    lines.push(...sectionLines(layout.style, section.heading));
    lines.push(...entries.filter((e) => e.group === "").map(itemLine));
    const groups = [...new Set(entries.map((e) => e.group))]
      .filter((g) => g !== "")
      .sort(byCodeUnit);
    for (const group of groups) {
      const members = entries.filter((e) => e.group === group);
      lines.push(
        "",
        scopeHeading(layout.style, members[0].paths),
        ...members.map(itemLine),
      );
    }
  });
  if (layout.tail.length > 0) lines.push("", ...layout.tail);
  return `${lines.join("\n")}\n`;
}

// ---------------------------------------------------------------------------
// Shared lines
// ---------------------------------------------------------------------------

/** The one quiet header line every compiled file starts with (after any frontmatter). */
export function headerComment(model: Model): string {
  return `<!-- ${headerText(model)}; edits here come back as proposals. -->`;
}

/** The same header for copy-out text, which can't come back. */
export function headerPlain(model: Model): string {
  return `${headerText(model)}.`;
}

function headerText(model: Model): string {
  return `Compiled by Memax from ${model.space.slug} at ${model.stamp} (${model.compileId}). Edit it at ${model.space.url}`;
}

/** Recognises a header written by {@link headerComment}. */
export const HEADER_LINE = /^<!-- Compiled by Memax from \S+ at .* -->$/;
