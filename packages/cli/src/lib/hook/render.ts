// The `<memax-context>` block: what changed since the agent's last session
// in this repository, never the compiled file itself (an agent that loads
// AGENTS.md and also reads it from a hook holds two copies). Short by
// design, and held under the agent's budget: forgets first, then gates,
// then the changed lines, then compiles not on disk yet. The changed
// lines are cut first when the block runs long (they are in the files).
//
// On a machine whose home is linked, a Personal section follows the
// repository's, the same way (personal-layer design §7.1): what changed
// in the agent's own home file. Outside a repository it is the block. The
// budget is shared, the repository's lines first: Personal's are cut first.
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
  /**
   * Cloud: new and changed lines the server left out (it sends at most 60
   * a file); they are counted with the lines cut for the budget.
   */
  more?: number;
}

export interface BlockForget {
  ref: string;
  with: string[];
  at: string;
  /** Cloud: the files here that still say it. */
  held?: string[];
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

/** A memory marked stale, and why in words (no statement). */
export interface BlockVerifying {
  ref: string;
  why: string;
}

/** What this machine's check found on the branch checked out here. */
export interface BlockBranch {
  name: string;
  unpushed?: boolean;
  /** When the daemon checked it (ISO). */
  at: string;
  hints: BlockVerifying[];
}

/** One space's news: the repository's, or the Personal space's here. */
export interface BlockSection {
  space: string;
  files: BlockFile[];
  forgotten: BlockForget[];
  gates: BlockGate[];
  behind: BlockBehind[];
  /** Memories marked stale since the last session here. */
  verifying?: BlockVerifying[];
  /** This branch's hints not told before. */
  branch?: BlockBranch;
}

export interface BlockInput {
  /** The repository's section; absent outside a repository. */
  repo?: BlockSection;
  /** The Personal space's, on a machine whose home is linked. */
  personal?: BlockSection;
  maxTokens: number;
  /**
   * `cloud` is a cloud agent's start (`memax hook cloud-start`): what
   * changed since the compile each committed file holds (`was`, the file;
   * `compile`, the space's latest), as the server says. Only the opening
   * sentence, each file's first line, a hand edit's line, a forget still
   * in the files and the count of lines left out read differently;
   * `local`, the default, is the session-start hook's block.
   */
  mode?: "local" | "cloud";
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
/** "2026-10-13 14:31 UTC". */
const minute = (iso: string) => `${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`;

function quote(s: string, max = 200): string {
  const t = s.length > max ? `${s.slice(0, max - 1)}…` : s;
  return `"${t.replace(/"/g, "'")}"`;
}

function attr(s: string): string {
  return s.replace(/[^A-Za-z0-9._-]/g, "");
}

function hasNews(b: BlockSection): boolean {
  return (
    b.forgotten.length > 0 ||
    b.gates.length > 0 ||
    b.behind.length > 0 ||
    (b.verifying?.length ?? 0) > 0 ||
    (b.branch?.hints.length ?? 0) > 0 ||
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

const REPO_INTRO =
  "Memax: what changed in this repository's compiled context since your last session here. The files themselves are already loaded.";
const PERSONAL_ALONE =
  "Memax: what changed in your Personal space's own file on this machine since your last session. It is already loaded.";

/** The fixed parts of a section: everything but its changed lines. */
function frame(
  b: BlockSection,
  intro: string,
): { head: string[]; tail: string[] } {
  const head: string[] = [intro];
  const tail: string[] = [];
  if (b.forgotten.length > 0) {
    head.push(
      "Forgotten by a person (don't use or repeat what these said, and drop any copy you keep):",
    );
    for (const f of b.forgotten) {
      const w = f.with.length > 0 ? `, with ${f.with.join(", ")}` : "";
      const held =
        f.held && f.held.length > 0
          ? `, still in ${f.held.join(" and ")} here`
          : "";
      head.push(`- ${f.ref} (${day(f.at)}${w})${held}`);
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
  if (b.verifying && b.verifying.length > 0) {
    head.push(
      "Being verified (something these rely on changed; check the code before relying on them, and tell the person):",
    );
    for (const v of b.verifying) head.push(`- ${v.ref}: ${v.why}`);
  }
  if (b.branch && b.branch.hints.length > 0) {
    const pushed = b.branch.unpushed ? ", not pushed yet" : "";
    head.push(
      `On this branch (${b.branch.name}${pushed}), as this machine checked it at ${minute(b.branch.at)} (not the record until it merges):`,
    );
    for (const h of b.branch.hints) head.push(`- ${h.ref}: ${h.why}`);
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
  return { head, tail };
}

/** One piece of a file's changes: a plain line, or a titled list. */
type Piece = { line: string } | { title: string; bullets: string[] };

/** The block, or "" when nothing changed. */
export function renderBlock(b: BlockInput): string {
  const sections: Array<{
    head: string[];
    tail: string[];
    pieces: Piece[];
  }> = [];
  const cloud = b.mode === "cloud";
  const repo = b.repo && hasNews(b.repo) ? b.repo : undefined;
  const personal = b.personal && hasNews(b.personal) ? b.personal : undefined;
  if (!repo && !personal) return "";
  if (repo)
    sections.push({
      ...frame(repo, cloud ? cloudIntro(repo) : REPO_INTRO),
      pieces: repo.files.flatMap(cloud ? cloudPieces : filePieces),
    });
  if (personal) {
    const intro = repo
      ? `Your Personal space (${personal.space}): what changed in your own file on this machine since your last session. It is already loaded too.`
      : PERSONAL_ALONE;
    sections.push({
      ...frame(personal, intro),
      pieces: personal.files.flatMap(filePieces),
    });
  }
  const open = `<memax-context space="${attr((repo ?? personal)!.space)}">`;
  const close = "</memax-context>";
  const fits = (s: string) =>
    estimateTokens(s) <= b.maxTokens && s.length <= MAX_CHARS;
  const total = sections.reduce(
    (n, sec) =>
      n +
      sec.pieces.reduce(
        (m, p) => m + ("bullets" in p ? p.bullets.length : 0),
        0,
      ),
    0,
  );
  // Lines the server left out (cloud): they are news the files don't hold.
  const leftOut = cloud
    ? (repo?.files ?? []).reduce((n, f) => n + (f.more ?? 0), 0)
    : 0;
  // The first `keep` changed lines, in order (the repository's first); a
  // list left empty goes.
  const lines = (keep: number): string[] => {
    const out: string[] = [open];
    let left = keep;
    sections.forEach((sec, i) => {
      out.push(...sec.head);
      for (const p of sec.pieces) {
        if ("line" in p) {
          out.push(p.line);
          continue;
        }
        const shown = p.bullets.slice(0, Math.max(0, left));
        left -= shown.length;
        if (shown.length > 0) out.push(p.title, ...shown);
      }
      if (i === sections.length - 1 && (keep < total || leftOut > 0)) {
        const more = total - keep + leftOut;
        out.push(
          cloud
            ? `…and ${more} more new or changed line${more === 1 ? "" : "s"} in the space's latest compile, not shown here.`
            : `…and ${more} more new or changed line${more === 1 ? "" : "s"}, already in the files above.`,
        );
      }
      out.push(...sec.tail);
    });
    return [...out, close];
  };
  let keep = total;
  let text = lines(keep).join("\n");
  while (!fits(text) && keep > 0) {
    keep = Math.max(0, keep - Math.max(1, Math.ceil(keep / 4)));
    text = lines(keep).join("\n");
  }
  if (!fits(text)) {
    // The notices alone run long: keep what fits, line by line.
    const out: string[] = [];
    for (const l of lines(0).slice(0, -1)) {
      if (!fits([...out, l, close].join("\n"))) break;
      out.push(l);
    }
    text = [...out, close].join("\n");
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

/** A cloud start's opening sentence, with the compiles the files hold. */
function cloudIntro(b: BlockSection): string {
  const held = [...new Set(b.files.map((f) => f.was ?? f.compile))];
  const at = held.length > 0 ? ` (${held.join(", ")})` : "";
  return `Memax: what changed in this repository's compiled context since the compile it holds${at}. The files themselves are already loaded.`;
}

/**
 * A file's lines in a cloud start: the compile the file holds (`was`)
 * against the space's latest (`compile`). A hand edit is said, and the
 * delta still follows: it is the record's, whatever the file says.
 */
function cloudPieces(f: BlockFile): Piece[] {
  const here = f.was ?? f.compile;
  if (f.added.length + f.changed.length + f.gone.length === 0 && !f.handEdit)
    return [];
  const out: Piece[] = [];
  if (here !== f.compile)
    out.push({
      line: `${f.path} here is ${here}; the space has compiled ${f.compile} since.`,
    });
  if (f.handEdit)
    out.push({
      line: `${f.path} here differs from what ${here} wrote (edited by hand), so its Memax lines may not be the record's.`,
    });
  if (f.added.length > 0)
    out.push({ title: "New:", bullets: f.added.map((l) => bullet(l.text)) });
  if (f.changed.length > 0)
    out.push({
      title: "Changed:",
      bullets: f.changed.map((l) => bullet(l.text)),
    });
  if (f.gone.length > 0)
    out.push({
      line: `No longer in ${f.path}: ${f.gone.join(", ")} (superseded or excluded; don't rely on ${f.gone.length === 1 ? "it" : "them"}).`,
    });
  return out;
}
