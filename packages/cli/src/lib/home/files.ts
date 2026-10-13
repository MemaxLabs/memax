// Which home files `memax link --home` offers on this machine (personal-
// layer design §3.2, §6.1), resolved here, in the person's shell, where
// CLAUDE_CONFIG_DIR and CODEX_HOME are set: the daemon runs without them.
//
// - Claude Code: `$CLAUDE_CONFIG_DIR/rules/memax-personal.md` (else
//   ~/.claude), a file Memax owns. User rules load every session.
// - Codex: one block in `$CODEX_HOME/AGENTS.override.md` when that file
//   exists and isn't empty (Codex reads it instead), else in AGENTS.md.
// - Gemini: one block in `~/.gemini/AGENTS.md` (`$GEMINI_CLI_HOME/.gemini`),
//   which Antigravity reads, and Gemini CLI once its context.fileName lists
//   AGENTS.md; offered only when one of them reads it here, and only when
//   asked for (UNCONFIRMED_HOME_KINDS).
// - Copilot: one block in `$COPILOT_HOME/copilot-instructions.md` (else
//   ~/.copilot), which Copilot CLI loads every session; beside Claude
//   Code's file, see copilotBesideClaude.
// - Windsurf: one block in ~/.codeium/windsurf/memories/global_rules.md,
//   whose 6,000 characters the person's rules share (the daemon writes
//   nothing past them); only when asked for (UNCONFIRMED_HOME_KINDS).
// - The canonical copy `~/.memax/personal/AGENTS.md`, Memax's, which
//   OpenCode reads once memax connect opencode names it in its
//   instructions (lib/connect/opencode.ts).
//
// An agent offers a file only when its folder is here. Each file is
// refused when it resolves outside home, through a symlink, or inside a
// git work tree (a dotfiles repository would commit it).
import { lstatSync, readFileSync, realpathSync, statSync } from "node:fs";
import { basename, dirname, isAbsolute, join, relative } from "node:path";
import type { V2 } from "memax-sdk";
import { agent } from "../agents/agents.js";
import { readerOf } from "../agents/loads.js";
import type { AgentId } from "../agents/types.js";
import { blockState } from "../daemon/compiler/drift.js";
import { removeManagedBlock } from "../daemon/compiler/managed-block.js";
import {
  HOME_CANONICAL,
  homeBlockFile,
  homeFileRefusal,
  tilde,
} from "../daemon/home-place.js";
import { geminiDir, geminiReaders } from "../connect/gemini.js";
import type { HomeFile } from "../daemon/home-link.js";

/** An agent's name, as the registry has it. */
const named = (id: AgentId) => agent(id).name;

export interface HomeEnv {
  /** The home folder, a real path. */
  home: string;
  env: Record<string, string | undefined>;
  platform?: NodeJS.Platform;
}

export interface HomeFilePlan {
  kind: V2.TargetKind;
  /** Who reads it: "Claude Code", "Codex", or "Memax" for the canonical. */
  agent: string;
  /** The agents that load it here (the target's read_by); none for the canonical. */
  readBy: string[];
  /** Relative to home: what the server and home.json record. */
  path: string;
  abs: string;
  /** Why Memax won't write it here, or null. */
  refusal: string | null;
  /** Whether the person owns the file and Memax keeps one block in it. */
  block: boolean;
  /** What is there now. */
  now: "absent" | "memax" | "theirs";
  /** The person's own lines in a block file (outside any Memax block). */
  lines: number;
  /** A file the person owns already has a <!-- memax:start --> block (V1's). */
  hasBlock: boolean;
  /** What to know about writing it on this machine (copilotBesideClaude). */
  note?: string;
}

/** The folder an environment variable names, else `~/<fallback>`. */
function folder(e: HomeEnv, name: string, fallback: string): string {
  const v = e.env[name]?.trim();
  if (!v) return join(e.home, fallback);
  const p = v.startsWith("~/") ? join(e.home, v.slice(2)) : v;
  return isAbsolute(p) ? p : join(e.home, p);
}

/** `p` with its parent's symlinks resolved, its own name kept (a link stays visible). */
function realParent(p: string): string {
  try {
    return join(realpathSync(dirname(p)), basename(p));
  } catch {
    return p;
  }
}

function isDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

function nonEmptyFile(p: string): boolean {
  try {
    return statSync(p).isFile() && statSync(p).size > 0;
  } catch {
    return false;
  }
}

function plan(
  e: HomeEnv,
  kind: V2.TargetKind,
  agent: string,
  abs: string,
  readBy: string[],
): HomeFilePlan {
  const rel = relative(e.home, abs);
  const outside = rel === "" || rel.startsWith("..") || isAbsolute(rel);
  const path = outside ? abs : rel.split("\\").join("/");
  const block = !outside && homeBlockFile(kind, path);
  const refusal = outside
    ? `${abs} is outside your home folder, so Memax won't write it`
    : homeFileRefusal(e.home, kind, path);
  const out: HomeFilePlan = {
    kind,
    agent,
    readBy,
    path,
    abs,
    refusal,
    block,
    now: "absent",
    lines: 0,
    hasBlock: false,
  };
  let content: string | null = null;
  try {
    if (lstatSync(abs).isFile()) content = readFileSync(abs, "utf8");
  } catch {
    // absent
  }
  if (content === null) return out;
  if (block) {
    out.now = "theirs";
    const state = blockState(content);
    out.hasBlock = state === "present";
    const mine = state === "present" ? removeManagedBlock(content) : content;
    out.lines =
      mine === "" ? 0 : mine.replace(/\r?\n$/, "").split(/\r?\n/).length;
  } else {
    out.now = content.includes("Compiled by Memax") ? "memax" : "theirs";
  }
  return out;
}

/**
 * Files an agent reads instead of the one linked here since (design §6.5):
 * Codex reads a non-empty AGENTS.override.md in place of AGENTS.md, and
 * AGENTS.md again once the override is gone or empty. Memax never moves a
 * block on its own; memax connect codex moves it.
 */
export function movedFiles(
  home: string,
  files: Array<{ kind: V2.TargetKind; path: string }>,
): string[] {
  const out: string[] = [];
  for (const f of files) {
    if (f.kind !== "codex_user") continue;
    const dir = join(home, dirname(f.path));
    const override = join(dir, "AGENTS.override.md");
    const reads = nonEmptyFile(override) ? override : join(dir, "AGENTS.md");
    if (reads !== join(home, f.path)) {
      out.push(
        `Codex reads ${tilde(home, reads)} now; run memax connect codex to move Memax's block there.`,
      );
    }
  }
  return out;
}

/** What the consent and memax connect say beside Claude Code's and Copilot's files. */
export const COPILOT_BESIDE_CLAUDE =
  "VS Code's Local agent reads ~/.claude and ~/.copilot both, so it loads your Personal space twice; its Copilot agent, which VS Code recommends, and Copilot CLI read it once.";

/**
 * The one rule for Copilot's file beside Claude Code's (adapter load
 * matrix, user level): VS Code's Local harness reads ~/.claude/rules and
 * ~/.claude/CLAUDE.md, and ~/.copilot/copilot-instructions.md, so where
 * both agents' files are written it loads the Personal space twice. Memax
 * writes Copilot's anyway and says so. Copilot CLI and VS Code's Copilot
 * harness, which VS Code's docs say to start with, read only ~/.copilot;
 * leaving the file out would give them no personal layer on every machine
 * with Claude Code, to spare the Local harness a second copy of at most
 * 6 KiB. Marks both files with the note when both are planned here.
 */
export function copilotBesideClaude(files: HomeFilePlan[]): HomeFilePlan[] {
  const both =
    files.some((f) => f.kind === "claude_user" && !f.refusal) &&
    files.some((f) => f.kind === "copilot_user" && !f.refusal);
  if (!both) return files;
  return files.map((f) =>
    f.kind === "claude_user" || f.kind === "copilot_user"
      ? { ...f, note: COPILOT_BESIDE_CLAUDE }
      : f,
  );
}

/**
 * Home kinds no real tool has confirmed loading yet, so a home link offers
 * them only when someone asks for that agent (memax connect gemini), never
 * in memax link --home's default consent. Gemini's file: Antigravity's docs
 * and Gemini CLI's source say they read ~/.gemini/AGENTS.md, but neither
 * has been run against a block in it. Windsurf's: its docs say it reads
 * its global rules, never run here.
 */
export const UNCONFIRMED_HOME_KINDS: ReadonlySet<V2.TargetKind> = new Set([
  "gemini_user",
  "windsurf_user",
]);

/**
 * The home files this machine offers, in the consent's order; an
 * unconfirmed kind only when `ask` names it.
 */
export function planHomeFiles(
  e: HomeEnv,
  ask: readonly V2.TargetKind[] = [],
): HomeFilePlan[] {
  const out: HomeFilePlan[] = [];
  const claude = realParent(folder(e, "CLAUDE_CONFIG_DIR", ".claude"));
  if (isDir(claude)) {
    out.push(
      plan(
        e,
        "claude_user",
        named("claude-code"),
        join(claude, "rules", "memax-personal.md"),
        ["claude-code"],
      ),
    );
  }
  const codex = realParent(folder(e, "CODEX_HOME", ".codex"));
  if (isDir(codex)) {
    const override = join(codex, "AGENTS.override.md");
    out.push(
      plan(
        e,
        "codex_user",
        named("codex"),
        nonEmptyFile(override) ? override : join(codex, "AGENTS.md"),
        ["codex"],
      ),
    );
  }
  const gemini = realParent(geminiDir(e));
  const readers =
    isDir(gemini) && ask.includes("gemini_user") ? geminiReaders(e) : [];
  if (readers.length > 0) {
    out.push(
      plan(
        e,
        "gemini_user",
        readers
          .map((r) =>
            r === "antigravity"
              ? readerOf("antigravity")!.name
              : named("gemini-cli"),
          )
          .join(" and "),
        join(gemini, "AGENTS.md"),
        readers,
      ),
    );
  }
  const copilot = realParent(folder(e, "COPILOT_HOME", ".copilot"));
  if (isDir(copilot)) {
    out.push(
      plan(
        e,
        "copilot_user",
        named("copilot"),
        join(copilot, "copilot-instructions.md"),
        ["copilot"],
      ),
    );
  }
  const windsurf = realParent(join(e.home, ".codeium", "windsurf"));
  if (isDir(windsurf) && ask.includes("windsurf_user")) {
    out.push(
      plan(
        e,
        "windsurf_user",
        named("windsurf"),
        join(windsurf, "memories", "global_rules.md"),
        ["windsurf"],
      ),
    );
  }
  if (out.length > 0 || ask.includes("agents_md")) {
    out.push(plan(e, "agents_md", "Memax", join(e.home, HOME_CANONICAL), []));
  }
  return copilotBesideClaude(out);
}

/** A linked file as home.json records it, with who reads it. */
export function homeFile(
  targetId: string,
  f: Pick<HomeFilePlan, "kind" | "path" | "abs" | "readBy">,
): HomeFile {
  return {
    target_id: targetId,
    kind: f.kind,
    path: f.path,
    abs: f.abs,
    ...(f.readBy.length > 0 ? { read_by: f.readBy } : {}),
  };
}

/** What linking does to a file, as the consent says it. */
export function describe(f: HomeFilePlan): string {
  if (!f.block) {
    if (f.now === "absent") return "a new file, Memax's";
    if (f.now === "memax") return "Memax's, already here";
    return "already here and not Memax's: Memax won't write over it";
  }
  if (f.now === "absent") return "a new file, with one Memax block";
  const yours =
    f.lines === 0
      ? ""
      : f.lines === 1
        ? "; your 1 line stays as it is"
        : `; your ${f.lines} lines stay as they are`;
  return `one Memax block${f.hasBlock ? ", in place of the one there" : ""}${yours}`;
}

/** The consent (design §6.1): every file, what happens to it, who can read it. */
export function consentLines(home: string, files: HomeFilePlan[]): string[] {
  const ok = files.filter((f) => !f.refusal);
  const width = Math.max(...ok.map((f) => tilde(home, f.abs).length)) + 3;
  const notes = [...new Set(ok.flatMap((f) => (f.note ? [f.note] : [])))];
  return [
    "",
    "  Write your Personal space into your agents' own files on this machine?",
    "",
    ...ok.map((f) => `    ${tilde(home, f.abs).padEnd(width)}${describe(f)}`),
    ...notes.flatMap((n) => ["", ...wrap(n, 74).map((l) => `  ${l}`)]),
    "",
    "  Every agent on this machine reads these in every project. Anyone who uses",
    "  this account on this computer can read them too. Hand edits come back as",
  ];
}

/** The consent's last line, which the answer follows. */
export const CONSENT_QUESTION =
  "  proposals, and memax unlink --home takes them off. [Y/n] ";

/** `text` in lines of at most `width` characters, broken at spaces. */
function wrap(text: string, width: number): string[] {
  const lines: string[] = [];
  let line = "";
  for (const word of text.split(" ")) {
    if (line && line.length + 1 + word.length > width) {
      lines.push(line);
      line = word;
    } else line = line ? `${line} ${word}` : word;
  }
  if (line) lines.push(line);
  return lines;
}
