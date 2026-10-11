// The two tidy-ups a home link offers once its first files are on disk
// (personal-layer design §6.3), each needing a yes, never under --yes,
// and each leaving a copy in ~/.memax/backup/ first:
//
// - Lines that now load twice. memax init brought the person's own lines
//   from ~/.claude/CLAUDE.md and ~/.codex/AGENTS.md into Personal; kept
//   and compiled, the agent reads each twice on this machine. A line goes
//   only when it is one whole statement and exactly a line Memax compiled
//   into that agent's home file here (so a memory kept for one repository,
//   which stays out of home files, never matches).
// - V1's instruction block. V1's memax setup --instructions left a
//   <!-- memax:start --> block of guidance in ~/.claude/CLAUDE.md and
//   ~/.gemini/GEMINI.md (Codex's is replaced in place by V2's block). It
//   goes only when it holds exactly V1's text: that guidance is in the
//   Memax plugin's skill now.
import { mkdirSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { MEMAX_INSTRUCTION_BLOCK } from "../../commands/setup-instructions.js";
import { extractManagedBlock } from "../daemon/compiler/managed-block.js";
import { commitWrite, readDisk } from "../daemon/fs-atomic.js";
import type { HomeLink } from "../daemon/home-link.js";
import { agentFolder, tilde } from "../daemon/home-place.js";
import { splitMarkdown } from "../init/split.js";

export interface Tidy {
  kind: "twice" | "v1";
  abs: string;
  /** `~/.claude/CLAUDE.md` */
  label: string;
  /** The lines that go (twice), or 0 for V1's block. */
  lines: number;
  /** The file as it was read, and as it would be. */
  before: string;
  after: string;
}

function readText(abs: string): string | null {
  try {
    return readFileSync(abs, "utf8");
  } catch {
    return null;
  }
}

/** A file's lines, each with its own line break ("" at the end of the file). */
function rawLines(content: string): string[] {
  return content.match(/[^\r\n]*(?:\r\n|\r|\n)|[^\r\n]+$/g) ?? [];
}

const CITED = /^\s*[-*+]\s+(.*?)\s+\[M-\d+(?:,\s*M-\d+)*\]\s*$/;

/** The statements a compiled file carries, as cited lines (no qualifier). */
function compiledStatements(content: string): Set<string> {
  const out = new Set<string>();
  for (const line of content.split(/\r\n|\r|\n/)) {
    const m = CITED.exec(line);
    if (m && !m[1].startsWith("(")) out.add(m[1]);
  }
  return out;
}

/** The person's lines in `content` that are exactly one of `compiled`. */
function twiceIn(
  home: string,
  abs: string,
  content: string,
  compiled: Set<string>,
): Tidy | null {
  const drop = new Set(
    splitMarkdown(content, "home")
      .statements.filter((s) => s.line === s.endLine && compiled.has(s.text))
      .map((s) => s.line),
  );
  if (drop.size === 0) return null;
  const after = rawLines(content)
    .filter((_, i) => !drop.has(i + 1))
    .join("");
  return {
    kind: "twice",
    abs,
    label: tilde(home, abs),
    lines: drop.size,
    before: content,
    after,
  };
}

/** Lines that load twice now, in this machine's linked agents' files. */
export function findTwice(link: HomeLink): Tidy[] {
  const out: Tidy[] = [];
  for (const f of link.files) {
    if (
      f.kind === "claude_user" &&
      f.path.endsWith("/rules/memax-personal.md")
    ) {
      // Claude Code reads the person's CLAUDE.md beside Memax's rules file.
      const rules = readText(f.abs);
      const folder = agentFolder(f.kind, f.path);
      const own = folder ? join(link.home, folder, "CLAUDE.md") : null;
      const content = own ? readText(own) : null;
      if (!rules || !own || content === null) continue;
      const t = twiceIn(link.home, own, content, compiledStatements(rules));
      if (t) out.push(t);
    } else if (
      f.kind === "codex_user" ||
      f.kind === "gemini_user" ||
      f.kind === "copilot_user"
    ) {
      // Codex's, Gemini's and Copilot's file: the person's lines around
      // Memax's block.
      // Antigravity reads ~/.gemini/GEMINI.md beside it too.
      const content = readText(f.abs);
      let block: string | null = null;
      try {
        block = content === null ? null : extractManagedBlock(content);
      } catch {
        block = null;
      }
      if (content === null || !block) continue;
      const statements = compiledStatements(block);
      const t = twiceIn(link.home, f.abs, content, statements);
      if (t) out.push(t);
      if (f.kind === "gemini_user") {
        const beside = join(dirname(f.abs), "GEMINI.md");
        const theirs = readText(beside);
        const g =
          theirs === null
            ? null
            : twiceIn(link.home, beside, theirs, statements);
        if (g) out.push(g);
      }
    }
  }
  return out;
}

const V1_BLOCK = extractManagedBlock(MEMAX_INSTRUCTION_BLOCK);

/** V1's instruction block, where it is exactly V1's (Claude Code's, Gemini's). */
export function findV1(
  home: string,
  env: Record<string, string | undefined>,
): Tidy[] {
  const claude = env.CLAUDE_CONFIG_DIR?.trim() || join(home, ".claude");
  const out: Tidy[] = [];
  for (const abs of [
    join(claude, "CLAUDE.md"),
    join(home, ".gemini", "GEMINI.md"),
  ]) {
    const content = readText(abs);
    if (content === null) continue;
    let block: string | null = null;
    try {
      block = extractManagedBlock(content);
    } catch {
      continue;
    }
    if (block === null || block !== V1_BLOCK) continue;
    // As V1's own remove did: the block, and the blank line it left.
    const rest = content
      .replace(/\r?\n?<!-- memax:start -->[\s\S]*?<!-- memax:end -->\r?\n?/, "")
      .trimEnd();
    out.push({
      kind: "v1",
      abs,
      label: tilde(home, abs),
      lines: 0,
      before: content,
      after: rest === "" ? "" : rest + "\n",
    });
  }
  return out;
}

/** The question a tidy-up asks; Enter keeps the file as it is. */
export function tidyQuestion(t: Tidy, space: string): string {
  if (t.kind === "v1") {
    return `  ${t.label} still has Memax V1's instructions; the Memax plugin's skill carries them now. Take them out? A copy goes to ~/.memax/backup/. [y/N] `;
  }
  const one = t.lines === 1;
  return `  ${t.lines} ${one ? "line" : "lines"} in ${t.label} ${one ? "is" : "are"} kept in ${space} now and ${one ? "loads" : "load"} twice. Take ${one ? "it" : "them"} out? A copy goes to ~/.memax/backup/. [y/N] `;
}

/**
 * Copies the file to ~/.memax/backup/ (0600, the folder 0700), then writes
 * the tidied file, but only if it is still exactly what was read.
 */
export async function applyTidy(
  t: Tidy,
  home: string,
  now = new Date(),
): Promise<"done" | "changed"> {
  const disk = await readDisk(t.abs);
  if (disk.kind !== "file" || disk.content !== t.before) return "changed";
  const dir = join(home, ".memax", "backup");
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const stamp = now.toISOString().replace(/[:.]/g, "-");
  const name = `${basename(dirname(t.abs)).replace(/^\./, "")}-${basename(t.abs)}.${stamp}`;
  const backup = join(dir, name);
  await commitWrite(backup, t.before, { kind: "absent" }, { mode: 0o600 });
  const r = await commitWrite(
    t.abs,
    t.after,
    { kind: "content", content: t.before },
    { mode: disk.mode },
  );
  return r === "written" ? "done" : "changed";
}
