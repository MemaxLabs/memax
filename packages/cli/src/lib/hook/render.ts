// The `<memax-context>` block: what changed since the agent's last session
// in this repository, never the compiled file itself (an agent that loads
// AGENTS.md and also reads it from a hook holds two copies). Short by
// design, and held under the agent's budget: forgets first, then gates,
// then the changed lines, then compiles not on disk yet. The changed
// lines are cut first when the block runs long (they are in the files).
import type { CitedLine } from "./compiled.js";

export interface BlockFile {
  path: string;
  compile: string;
  /** The compile the last session saw, when it changed. */
  was?: string;
  added: CitedLine[];
  changed: CitedLine[];
  /** Refs whose lines left the file (not forgotten). */
  gone: string[];
  /** A hand edit waits in Review: its lines aren't the record's. */
  handEdit: boolean;
  /** The last session didn't have this file: it loads in full. */
  fresh?: boolean;
}

export interface BlockForget {
  ref: string;
  with: string[];
  at: string;
}

export interface BlockGate {
  ref: string;
  question: string;
  agent?: string;
  expires_at: string;
}

export interface BlockBehind {
  path: string;
  here: string;
  latest: string;
  sync_state: string;
}

export interface BlockInput {
  space: string;
  files: BlockFile[];
  forgotten: BlockForget[];
  gates: BlockGate[];
  behind: BlockBehind[];
  maxTokens: number;
}

/** Claude Code saves plain output over 10,000 characters to a file instead. */
export const MAX_CHARS = 9_000;

/**
 * A conservative token count: about 3.5 ASCII characters a token, and a
 * token for every other character (CJK runs close to one each).
 */
export function estimateTokens(s: string): number {
  let ascii = 0;
  let other = 0;
  for (const ch of s) {
    if (ch.charCodeAt(0) < 128) ascii++;
    else other++;
  }
  return Math.ceil(ascii / 3.5) + other;
}

const day = (iso: string) => iso.slice(0, 10);

function quote(s: string, max = 200): string {
  const t = s.length > max ? `${s.slice(0, max - 1)}…` : s;
  return `"${t.replace(/"/g, "'")}"`;
}

function attr(s: string): string {
  return s.replace(/[^A-Za-z0-9._-]/g, "");
}

function hasNews(b: BlockInput): boolean {
  return (
    b.forgotten.length > 0 ||
    b.gates.length > 0 ||
    b.behind.length > 0 ||
    b.files.some(
      (f) =>
        f.added.length > 0 ||
        f.changed.length > 0 ||
        f.gone.length > 0 ||
        f.handEdit ||
        f.fresh,
    )
  );
}

/** The fixed parts: everything but the changed lines. */
function frame(b: BlockInput): { head: string[]; tail: string[] } {
  const head: string[] = [
    `<memax-context space="${attr(b.space)}">`,
    "Memax: what changed in this repository's compiled context since your last session here. The files themselves are already loaded.",
  ];
  const tail: string[] = [];
  if (b.forgotten.length > 0) {
    head.push(
      "Forgotten by a person (don't use or repeat what these said, and drop any copy you keep):",
    );
    for (const f of b.forgotten) {
      const w = f.with.length > 0 ? `, with ${f.with.join(", ")}` : "";
      head.push(`- ${f.ref} (${day(f.at)}${w})`);
    }
  }
  if (b.gates.length > 0) {
    head.push(
      "Waiting for a person to decide (don't decide these yourself; the answer arrives as a kept decision):",
    );
    for (const g of b.gates) {
      const by = g.agent ? `asked by ${g.agent}, ` : "";
      head.push(
        `- ${g.ref} ${quote(g.question)} (${by}open until ${day(g.expires_at)})`,
      );
    }
  }
  for (const x of b.behind) {
    const why =
      x.sync_state === "drifted"
        ? "a local edit to it waits in Review"
        : x.sync_state === "held"
          ? "a pulled edit's proposals wait in Review"
          : "it reaches this machine when the daemon delivers it";
    tail.push(
      `${x.path} here is ${x.here}; the space has compiled ${x.latest} since (${why}). Use memax_recall for anything newer.`,
    );
  }
  tail.push("</memax-context>");
  return { head, tail };
}

/** One piece of a file's changes: a plain line, or a titled list. */
type Piece = { line: string } | { title: string; bullets: string[] };

/** The block, or "" when nothing changed. */
export function renderBlock(b: BlockInput): string {
  if (!hasNews(b)) return "";
  const { head, tail } = frame(b);
  const fits = (s: string) =>
    estimateTokens(s) <= b.maxTokens && s.length <= MAX_CHARS;
  const pieces = b.files.flatMap(filePieces);
  const total = pieces.reduce(
    (n, p) => n + ("bullets" in p ? p.bullets.length : 0),
    0,
  );
  // The first `keep` changed lines, in order; a list left empty goes.
  const body = (keep: number): string[] => {
    const out: string[] = [];
    let left = keep;
    for (const p of pieces) {
      if ("line" in p) {
        out.push(p.line);
        continue;
      }
      const shown = p.bullets.slice(0, Math.max(0, left));
      left -= shown.length;
      if (shown.length > 0) out.push(p.title, ...shown);
    }
    if (keep < total) {
      const more = total - keep;
      out.push(
        `…and ${more} more new or changed line${more === 1 ? "" : "s"}, already in the files above.`,
      );
    }
    return out;
  };
  let keep = total;
  let text = [...head, ...body(keep), ...tail].join("\n");
  while (!fits(text) && keep > 0) {
    keep = Math.max(0, keep - Math.max(1, Math.ceil(keep / 4)));
    text = [...head, ...body(keep), ...tail].join("\n");
  }
  if (!fits(text)) {
    // The notices alone run long: keep what fits, line by line.
    const out: string[] = [];
    for (const l of [...head, ...body(0), ...tail.slice(0, -1)]) {
      if (!fits([...out, l, "</memax-context>"].join("\n"))) break;
      out.push(l);
    }
    text = [...out, "</memax-context>"].join("\n");
  }
  return `${text}\n`;
}

function filePieces(f: BlockFile): Piece[] {
  if (f.handEdit)
    return [
      {
        line: `${f.path} has a local edit that waits in Review; its changed lines aren't shown until a person settles it.`,
      },
    ];
  if (f.fresh)
    return [
      {
        line: `${f.path} (${f.compile}) is new here since your last session; it's loaded in full.`,
      },
    ];
  if (f.added.length + f.changed.length + f.gone.length === 0) return [];
  const out: Piece[] = [
    {
      line: `${f.path} is now ${f.compile} (your last session saw ${f.was ?? "an earlier compile"}).`,
    },
  ];
  if (f.added.length > 0)
    out.push({ title: "New:", bullets: f.added.map((l) => bullet(l.text)) });
  if (f.changed.length > 0)
    out.push({
      title: "Changed:",
      bullets: f.changed.map((l) => bullet(l.text)),
    });
  if (f.gone.length > 0)
    out.push({
      line: `No longer in ${f.path}: ${f.gone.join(", ")} (superseded or excluded; don't rely on them).`,
    });
  return out;
}

function bullet(text: string): string {
  return text.startsWith("- ") ? text : `- ${text}`;
}
