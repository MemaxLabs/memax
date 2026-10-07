// Splitting an agent file into statements (plan 25 §7.3 step 4), the same
// way every time: list items and paragraphs become statements, each with
// the headings above it as its context and its first line as its source;
// table rows become statements too. Code blocks, frontmatter, HTML
// comments, `@file` imports, Memax's own managed block, and whole files
// Memax compiled are skipped. Hidden characters are stripped exactly as
// the compiler strips them (the copied sanitize.ts), and counted.
import { cleanLine } from "../daemon/compiler/sanitize.js";
import {
  MANAGED_END,
  MANAGED_START,
} from "../daemon/compiler/managed-block.js";
import {
  countHidden,
  type HiddenCounts,
  addHidden,
  emptyHidden,
} from "./hidden.js";
import type { Location } from "./files.js";
import type { V2 } from "memax-sdk";

/** The header line of a file Memax compiled (packages/compiler document.ts HEADER_LINE). */
export const MEMAX_HEADER = /^<!-- Compiled by Memax from \S+ at .* -->$/;

/** The longest statement the record takes (ledger.MaxStatementRunes). */
export const MAX_STATEMENT = 2000;
/** A paragraph longer than this is split into its sentences. */
const LONG_PARAGRAPH = 500;
/** The most statements read from one file; the rest stay on the machine. */
export const MAX_STATEMENTS_PER_FILE = 300;

export interface Statement {
  /** The first line it came from, 1-based. */
  line: number;
  /** The last line it came from. */
  endLine: number;
  text: string;
  /** The headings above it, outermost first. */
  heading: string[];
  section: V2.Section;
  kind: V2.MemoryKind;
  hidden: HiddenCounts;
  /** The raw lines, for the secret scan and the branch check; never sent. */
  raw: string[];
}

export interface SplitResult {
  statements: Statement[];
  /** Where the file applies, from its frontmatter (globs, applyTo, paths). */
  paths: string[];
  /** Memax compiled this file (its header, with no managed block). */
  compiled: boolean;
  /** Statements past MAX_STATEMENTS_PER_FILE, or longer than MAX_STATEMENT. */
  overLimit: number[];
  tooLong: number[];
  skipped: {
    code: number;
    managed: number;
    comments: number;
    imports: number;
    boilerplate: number;
  };
  hidden: HiddenCounts;
}

const ITEM = /^(\s*)(?:[-*+]|\d{1,9}[.)])\s+(.*)$/;
const HEADING = /^\s{0,3}(#{1,6})\s+(.*?)(?:\s+#+)?\s*$/;
const FENCE = /^\s{0,3}(`{3,}|~{3,})/;
const TABLE_RULE = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;
const BOILERPLATE = [
  /^this file provides guidance to\b/i,
  /^this (file|document) (provides|gives|contains) (guidance|instructions|context)\b/i,
];

/** Globs a file names in its frontmatter: `globs:`, `applyTo:`, or a `paths:` list. */
function frontmatterPaths(lines: string[]): string[] {
  const paths: string[] = [];
  const unquote = (s: string) => s.trim().replace(/^["']|["']$/g, "");
  let inList = false;
  for (const line of lines) {
    const field = /^(globs|applyTo|paths):\s*(.*)$/.exec(line);
    if (field) {
      inList = field[2].trim() === "";
      const v = field[2].trim().replace(/^\[|\]$/g, "");
      paths.push(...v.split(",").map(unquote));
    } else if (inList && /^\s+-\s+/.test(line)) {
      paths.push(unquote(line.replace(/^\s+-\s+/, "")));
    } else {
      inList = false;
    }
  }
  return [...new Set(paths.filter(Boolean))].sort();
}

/** Where a statement belongs in the Brief, from the headings above it. */
export function sectionFor(
  heading: string[],
  location: Location,
): { section: V2.Section; kind: V2.MemoryKind } {
  for (let i = heading.length - 1; i >= 0; i--) {
    const h = heading[i].toLowerCase();
    if (/\bdecisions?\b|\badrs?\b|\bdecided\b/.test(h))
      return { section: "decisions", kind: "decision" };
    if (
      /\bopen questions?\b|\bquestions\b|\btodo\b|\bto do\b|\bunresolved\b|\btbd\b/.test(
        h,
      )
    )
      return { section: "open_question", kind: "fact" };
    if (
      /\bpreferences?\b|\bprefer\b|\babout me\b|\bpersonal\b|\btone\b/.test(h)
    )
      return { section: "preferences", kind: "fact" };
  }
  return {
    section: location === "home" ? "preferences" : "conventions",
    kind: "fact",
  };
}

/** Splits one file into statements. */
export function splitMarkdown(
  content: string,
  location: Location,
): SplitResult {
  const out: SplitResult = {
    statements: [],
    paths: [],
    compiled: false,
    overLimit: [],
    tooLong: [],
    skipped: { code: 0, managed: 0, comments: 0, imports: 0, boilerplate: 0 },
    hidden: emptyHidden(),
  };
  const lines = content.replace(/^\u{FEFF}/u, "").split(/\r\n|\r|\n/);
  let i = 0;
  if (lines[0]?.trimEnd() === "---") {
    const close = lines.findIndex((l, j) => j > 0 && l.trimEnd() === "---");
    if (close > 0 && close < 80) {
      out.paths = frontmatterPaths(lines.slice(1, close));
      i = close + 1;
    }
  }
  const hasBlock = lines.some((l) => l.trim() === MANAGED_START);
  if (!hasBlock && lines.some((l) => MEMAX_HEADER.test(l.trim()))) {
    out.compiled = true;
    return out;
  }

  const headings: { level: number; text: string }[] = [];
  let fence: string | null = null;
  let inManaged = false;
  let inComment = false;
  let tableCols = 0;
  // The statement being gathered: an item (with its indent) or a paragraph.
  let cur: {
    kind: "item" | "para";
    indent: number;
    start: number;
    end: number;
    raw: string[];
  } | null = null;
  // List items ending in ":" lead into their children ("Testing:" ▸ "- Run …").
  const leads: {
    indent: number;
    text: string;
    used: boolean;
    start: number;
    raw: string[];
  }[] = [];

  const headingPath = () => headings.map((h) => h.text);
  const emit = (start: number, end: number, raw: string[], prefix = "") => {
    const joined = raw.map((r) => r.trim()).join(" ");
    const hidden = countHidden(joined);
    let text = cleanLine(joined).text;
    if (prefix) text = `${prefix}: ${text}`;
    addHidden(out.hidden, hidden);
    if (!/\p{L}/u.test(text) || text.length < 3) return;
    if (BOILERPLATE.some((b) => b.test(text))) {
      out.skipped.boilerplate++;
      return;
    }
    const pieces =
      text.length > LONG_PARAGRAPH && !prefix ? sentences(text) : [text];
    const { section, kind } = sectionFor(headingPath(), location);
    for (const p of pieces) {
      if (p.length > MAX_STATEMENT) {
        out.tooLong.push(start);
        continue;
      }
      if (out.statements.length >= MAX_STATEMENTS_PER_FILE) {
        out.overLimit.push(start);
        continue;
      }
      out.statements.push({
        line: start,
        endLine: end,
        text: p,
        heading: headingPath(),
        section,
        kind,
        hidden,
        raw,
      });
    }
  };
  const flushLeads = (indent: number) => {
    // A lead with no children is a statement of its own.
    while (leads.length > 0 && leads[leads.length - 1].indent >= indent) {
      const l = leads.pop()!;
      if (!l.used) emit(l.start, l.start, l.raw);
    }
  };
  const flush = () => {
    if (!cur) return;
    const c = cur;
    cur = null;
    if (c.kind === "item") {
      const text = c.raw.join(" ").trim();
      if (text.endsWith(":") && c.raw.length === 1) {
        leads.push({
          indent: c.indent,
          text: cleanLine(text.slice(0, -1)).text,
          used: false,
          start: c.start,
          raw: c.raw,
        });
        return;
      }
      const lead = [...leads].reverse().find((l) => l.indent < c.indent);
      if (lead) lead.used = true;
      emit(c.start, c.end, c.raw, lead?.text ?? "");
      return;
    }
    // A short paragraph ending in ":" introduces the list after it
    // ("Read these first:"), and each item carries it.
    const text = c.raw.join(" ").trim();
    if (text.endsWith(":") && c.raw.length === 1 && text.length <= 120) {
      leads.push({
        indent: -1,
        text: cleanLine(text.slice(0, -1)).text,
        used: false,
        start: c.start,
        raw: c.raw,
      });
      return;
    }
    emit(c.start, c.end, c.raw);
  };

  for (; i < lines.length; i++) {
    const n = i + 1;
    // A blockquote's lines are read without their markers (code fences in
    // a quote included).
    const raw = lines[i];
    const quoted = /^\s{0,3}>/.test(raw);
    const line = quoted ? raw.trimStart().replace(/^(>\s?)+/, "") : raw;
    const t = line.trim();
    if (fence !== null) {
      const m = FENCE.exec(line);
      if (
        m &&
        m[1][0] === fence[0] &&
        m[1].length >= fence.length &&
        line.trim() === m[1]
      )
        fence = null;
      continue;
    }
    if (inManaged) {
      out.skipped.managed++;
      if (t === MANAGED_END) inManaged = false;
      continue;
    }
    if (inComment) {
      if (t.includes("-->")) inComment = false;
      continue;
    }
    const f = FENCE.exec(line);
    if (f) {
      flush();
      fence = f[1];
      out.skipped.code++;
      continue;
    }
    if (t === MANAGED_START) {
      flush();
      inManaged = true;
      out.skipped.managed++;
      continue;
    }
    if (t.startsWith("<!--")) {
      flush();
      out.skipped.comments++;
      if (!t.includes("-->")) inComment = true;
      continue;
    }
    if (t === "") {
      flush();
      tableCols = 0;
      continue;
    }
    if (/^@\S+$/.test(t)) {
      flush();
      out.skipped.imports++;
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      flush();
      flushLeads(-1);
      const level = h[1].length;
      while (
        headings.length > 0 &&
        headings[headings.length - 1].level >= level
      )
        headings.pop();
      headings.push({ level, text: cleanLine(h[2]).text });
      continue;
    }
    // Setext headings: a paragraph line underlined with === or ---.
    if (/^\s{0,3}(=+|-+)\s*$/.test(line)) {
      if (cur?.kind === "para" && cur.raw.length === 1) {
        const level = line.trim()[0] === "=" ? 1 : 2;
        const text = cleanLine(cur.raw[0]).text;
        cur = null;
        while (
          headings.length > 0 &&
          headings[headings.length - 1].level >= level
        )
          headings.pop();
        headings.push({ level, text });
      } else {
        flush();
      }
      continue;
    }
    if (t.startsWith("|")) {
      flush();
      if (TABLE_RULE.test(t)) {
        tableCols = t.split("|").length;
        continue;
      }
      if (tableCols > 0) {
        const cells = t
          .replace(/^\||\|$/g, "")
          .split("|")
          .map((c) => c.trim())
          .filter(Boolean);
        if (cells.length > 0) emit(n, n, [cells.join(" — ")]);
      }
      // A header row (before the rule) names columns; it isn't a statement.
      continue;
    }
    const body = line;
    const item = ITEM.exec(body);
    if (item) {
      flush();
      const indent = item[1].replace(/\t/g, "    ").length;
      flushLeads(indent);
      let text = item[2];
      text = text.replace(/^\[[ xX]\]\s+/, "");
      cur = { kind: "item", indent, start: n, end: n, raw: [text] };
      continue;
    }
    if (cur?.kind === "item" && /^\s+\S/.test(body)) {
      cur.raw.push(body.trim());
      cur.end = n;
      continue;
    }
    if (cur?.kind === "para") {
      cur.raw.push(body.trim());
      cur.end = n;
      continue;
    }
    flush();
    if (/^\S/.test(body)) flushLeads(-1);
    cur = { kind: "para", indent: 0, start: n, end: n, raw: [body.trim()] };
  }
  flush();
  flushLeads(-1);
  return out;
}

/** A long paragraph's sentences. */
function sentences(text: string): string[] {
  return text
    .split(/(?<=[.!?])\s+(?=[A-Z0-9`"'(\[])/u)
    .map((s) => s.trim())
    .filter((s) => /\p{L}/u.test(s) && s.length >= 3);
}
