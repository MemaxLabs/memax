// The files agents already keep (plan 25 §7.3 step 1), in the repository
// and on this machine. Each is read as plain Markdown notes; nothing here
// parses an agent's private format (the fragility guard). Each kind says
// whose words a file holds, which sets the trust its statements arrive at:
//
//   repository  a file in the repository (CLAUDE.md, AGENTS.md, Cursor,
//               Copilot, Gemini, Windsurf/Devin and Claude rules): anyone
//               with push access can change it, so it is no one person's
//               word. Lines that are only on another branch than the
//               default one arrive external (GitInject; see git.ts).
//   person      a file the person writes for themselves: ~/.claude/CLAUDE.md
//               (Claude Code adds to it only when asked to, with #),
//               ~/.codex/AGENTS.md, and CLAUDE.local.md.
//   agent_own_work  notes an agent wrote on its own: Claude Code's auto
//               memory, Codex's memories, and Gemini's GEMINI.md (its
//               save_memory tool appends to the same file, so Memax can't
//               tell the person's lines from Gemini's and takes the lower).
//
// Project files go to the project space; machine-local memory to the
// person's Personal space. Claude Code's auto memory is kept per project:
// only this repository's is read.
import { readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import type { V2 } from "memax-sdk";

export type FileKind =
  | "agents_md"
  | "claude_md"
  | "claude_local"
  | "claude_rule"
  | "cursor_rule"
  | "cursorrules"
  | "copilot_instructions"
  | "copilot_scoped"
  | "gemini_md"
  | "windsurf_rule"
  | "devin_rule"
  | "claude_user"
  | "claude_memory"
  | "codex_user"
  | "codex_memory"
  | "gemini_user"
  | "gemini_memory";

export type Location = "repository" | "home";

export interface FoundFile {
  kind: FileKind;
  agent: V2.AgentKind;
  location: Location;
  trust: V2.Trust;
  /** Absolute path on disk. */
  path: string;
  /** What people see (and the server stores): "CLAUDE.md", "~/.codex/memories/x.md". */
  label: string;
  /** For repository files, the path relative to the root. */
  repoPath?: string;
  bytes: number;
}

export interface FileScan {
  files: FoundFile[];
  /** Files too large to read as notes. */
  tooLarge: FoundFile[];
  /** Claude Code memory kept for other projects: counted, not read. */
  otherProjects: number;
}

/** The largest file read as notes; bigger ones are listed and skipped. */
export const MAX_FILE_BYTES = 256 * 1024;
/** The most files of one kind read (a memories folder, a rules folder). */
export const MAX_FILES_PER_DIR = 100;

function fileSize(p: string): number | null {
  try {
    const s = statSync(p);
    return s.isFile() ? s.size : null;
  } catch {
    return null;
  }
}

/** Markdown files under dir, sorted, at most depth levels down. */
function markdownIn(dir: string, exts: string[], depth: number): string[] {
  const out: string[] = [];
  const walk = (d: string, level: number) => {
    let names: string[];
    try {
      names = readdirSync(d).sort();
    } catch {
      return;
    }
    for (const n of names) {
      if (out.length >= MAX_FILES_PER_DIR) return;
      if (n.startsWith(".")) continue;
      const p = join(d, n);
      let st;
      try {
        st = statSync(p);
      } catch {
        continue;
      }
      if (st.isDirectory()) {
        if (level < depth) walk(p, level + 1);
      } else if (exts.some((e) => n.endsWith(e))) {
        out.push(p);
      }
    }
  };
  walk(dir, 1);
  return out;
}

/**
 * Claude Code keeps a project's auto memory under ~/.claude/projects,
 * in a folder named after the project's path with every character that
 * isn't a letter or a digit turned into "-".
 */
export function claudeProjectFolder(root: string): string {
  return root.replace(/[^A-Za-z0-9]/g, "-");
}

export interface FindOptions {
  home: string;
  root: string | null;
  /** Leave out machine-local memory (--no-personal). */
  personal: boolean;
}

/** Every agent file in the repository and on this machine. */
export function findFiles(o: FindOptions): FileScan {
  const scan: FileScan = { files: [], tooLarge: [], otherProjects: 0 };
  const add = (f: Omit<FoundFile, "bytes">) => {
    const bytes = fileSize(f.path);
    if (bytes === null) return;
    if (scan.files.some((x) => x.path === f.path)) return;
    const found = { ...f, bytes };
    if (bytes > MAX_FILE_BYTES) scan.tooLarge.push(found);
    else scan.files.push(found);
  };
  const repo = (
    rel: string,
    kind: FileKind,
    agent: V2.AgentKind,
    location: Location = "repository",
    trust: V2.Trust = "repository",
  ) => {
    if (!o.root) return;
    const repoPath = rel.split(sep).join("/");
    add({
      kind,
      agent,
      location,
      trust,
      path: join(o.root, rel),
      label: repoPath,
      repoPath,
    });
  };
  const repoDir = (
    dir: string,
    exts: string[],
    kind: FileKind,
    agent: V2.AgentKind,
  ) => {
    if (!o.root) return;
    for (const p of markdownIn(join(o.root, dir), exts, 3)) {
      repo(relative(o.root, p), kind, agent);
    }
  };
  const home = (
    rel: string,
    kind: FileKind,
    agent: V2.AgentKind,
    trust: V2.Trust,
  ) => {
    add({
      kind,
      agent,
      location: "home",
      trust,
      path: join(o.home, rel),
      label: "~/" + rel.split(sep).join("/"),
    });
  };

  // The repository.
  repo("AGENTS.md", "agents_md", "codex");
  repo("CLAUDE.md", "claude_md", "claude-code");
  repo(join(".claude", "CLAUDE.md"), "claude_md", "claude-code");
  repoDir(join(".claude", "rules"), [".md"], "claude_rule", "claude-code");
  repoDir(join(".cursor", "rules"), [".mdc", ".md"], "cursor_rule", "cursor");
  repo(".cursorrules", "cursorrules", "cursor");
  repo(
    join(".github", "copilot-instructions.md"),
    "copilot_instructions",
    "copilot",
  );
  repoDir(
    join(".github", "instructions"),
    [".instructions.md"],
    "copilot_scoped",
    "copilot",
  );
  repo("GEMINI.md", "gemini_md", "gemini-cli");
  repoDir(join(".windsurf", "rules"), [".md"], "windsurf_rule", "windsurf");
  repo(".windsurfrules", "windsurf_rule", "windsurf");
  repoDir(join(".devin", "rules"), [".md"], "devin_rule", "windsurf");
  if (!o.personal) return scan;

  // CLAUDE.local.md sits in the repository, kept out of git: the person's
  // own notes for this project, so it goes to Personal.
  if (o.root) {
    add({
      kind: "claude_local",
      agent: "claude-code",
      location: "home",
      trust: "person",
      path: join(o.root, "CLAUDE.local.md"),
      label: "CLAUDE.local.md",
    });
  }

  // This machine.
  home(join(".claude", "CLAUDE.md"), "claude_user", "claude-code", "person");
  if (o.root) {
    const projects = join(o.home, ".claude", "projects");
    const mine = claudeProjectFolder(o.root);
    let folders: string[] = [];
    try {
      folders = readdirSync(projects);
    } catch {
      // no auto memory on this machine
    }
    for (const f of folders) {
      const memory = join(projects, f, "memory");
      const notes = markdownIn(memory, [".md"], 1);
      if (notes.length === 0) continue;
      if (f !== mine) {
        scan.otherProjects++;
        continue;
      }
      for (const p of notes) {
        add({
          kind: "claude_memory",
          agent: "claude-code",
          location: "home",
          trust: "agent_own_work",
          path: p,
          label: `~/.claude/projects/…/memory/${relative(memory, p).split(sep).join("/")}`,
        });
      }
    }
  }
  home(join(".codex", "AGENTS.md"), "codex_user", "codex", "person");
  for (const p of markdownIn(join(o.home, ".codex", "memories"), [".md"], 3)) {
    home(relative(o.home, p), "codex_memory", "codex", "agent_own_work");
  }
  home(
    join(".gemini", "GEMINI.md"),
    "gemini_user",
    "gemini-cli",
    "agent_own_work",
  );
  home(
    join(".gemini", "MEMORY.md"),
    "gemini_memory",
    "gemini-cli",
    "agent_own_work",
  );
  for (const p of markdownIn(join(o.home, ".gemini", "memory"), [".md"], 2)) {
    home(relative(o.home, p), "gemini_memory", "gemini-cli", "agent_own_work");
  }
  return scan;
}
