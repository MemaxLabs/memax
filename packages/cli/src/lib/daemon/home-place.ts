// Where a home link writes (personal-layer design §6.1–6.2): exactly the
// files home.json recorded, each inside the home folder, never through a
// symlink, never inside a git work tree (a dotfiles repository would
// commit the Personal space with it), and never into an agent's folder
// that isn't there: Memax doesn't create ~/.codex. New files are 0600 and
// the folders Memax creates 0700.
import { lstatSync, realpathSync } from "node:fs";
import { lstat, mkdir } from "node:fs/promises";
import { dirname, join, relative, sep } from "node:path";
import type { V2 } from "memax-sdk";
import { HOME_CANONICAL, WINDSURF_GLOBAL_RULES } from "../agents/agents.js";
import type { HomeLink } from "./home-link.js";
import type { Place, PlaceTarget } from "./place.js";
import { UnsafePathError, validRepoPath } from "./safe-path.js";

/**
 * The personal canonical copy, as the server names it (HomeCanonicalPath),
 * and Windsurf's one global rules file (its WindsurfGlobalRules), from the
 * agents registry.
 */
export { HOME_CANONICAL, WINDSURF_GLOBAL_RULES };

/** The server's checkHomePath: a home file of a kind, in a dot folder. */
export function validHomePath(kind: V2.TargetKind, path: string): boolean {
  if (!validRepoPath(path)) return false;
  const slash = path.indexOf("/");
  const folder = slash < 0 ? path : path.slice(0, slash);
  if (!folder.startsWith(".") || slash < 0) return false;
  switch (kind) {
    case "agents_md":
      return path === HOME_CANONICAL;
    case "claude_user":
      return (
        folder !== ".memax" &&
        (path.endsWith("/rules/memax-personal.md") ||
          path.endsWith("/CLAUDE.md"))
      );
    case "codex_user":
      return (
        folder !== ".memax" &&
        (path.endsWith("/AGENTS.md") || path.endsWith("/AGENTS.override.md"))
      );
    case "gemini_user":
      return (
        folder !== ".memax" &&
        (path === ".gemini/AGENTS.md" || path.endsWith("/.gemini/AGENTS.md"))
      );
    case "copilot_user":
      return (
        folder !== ".memax" &&
        folder !== ".github" &&
        path.endsWith("/copilot-instructions.md")
      );
    case "windsurf_user":
      return path === WINDSURF_GLOBAL_RULES;
  }
  return false;
}

/**
 * Whether a home file is the person's, with one Memax block in it (the
 * server's homeOwned): Codex's and Gemini's AGENTS.md, Copilot's
 * copilot-instructions.md, Windsurf's global rules, and the
 * ~/.claude/CLAUDE.md fallback. Claude Code's rules file and the canonical
 * copy are Memax's own.
 */
export function homeBlockFile(kind: V2.TargetKind, path: string): boolean {
  return (
    kind === "codex_user" ||
    kind === "gemini_user" ||
    kind === "copilot_user" ||
    kind === "windsurf_user" ||
    (kind === "claude_user" && path.endsWith("/CLAUDE.md"))
  );
}

const AGENT: Partial<Record<V2.TargetKind, string>> = {
  claude_user: "Claude Code",
  codex_user: "Codex",
  gemini_user: "Gemini",
  copilot_user: "Copilot",
  windsurf_user: "Windsurf",
};

/**
 * The folder a home file lives in that Memax never creates (the agent's
 * own: CLAUDE_CONFIG_DIR, CODEX_HOME, ~/.gemini), relative to home; null for the
 * canonical copy, whose folders are Memax's.
 */
export function agentFolder(kind: V2.TargetKind, path: string): string | null {
  for (const suffix of [
    "/memories/global_rules.md",
    "/copilot-instructions.md",
    "/rules/memax-personal.md",
    "/CLAUDE.md",
    "/AGENTS.override.md",
    "/AGENTS.md",
  ]) {
    if (kind !== "agents_md" && path.endsWith(suffix))
      return path.slice(0, -suffix.length);
  }
  return null;
}

/** `~/…` for a path under `home`. */
export function tilde(home: string, abs: string): string {
  if (abs === home) return "~";
  return abs.startsWith(home + sep) ? `~/${relative(home, abs)}` : abs;
}

function lstatOrNull(p: string) {
  try {
    return lstatSync(p);
  } catch {
    return null;
  }
}

function realOrNull(p: string): string | null {
  try {
    return realpathSync(p);
  } catch {
    return null;
  }
}

/** The git work tree `abs` is inside (the folder holding `.git`), or null. */
export function gitWorkTree(abs: string): string | null {
  let dir = abs;
  for (;;) {
    if (lstatOrNull(join(dir, ".git"))) return dir;
    const up = dirname(dir);
    if (up === dir) return null;
    dir = up;
  }
}

/**
 * Why Memax won't write the home file at `path` (relative to `home`, a
 * real path) on this machine, or null. The same checks at link time and
 * before every write, since dotfiles and agents move.
 */
export function homeFileRefusal(
  home: string,
  kind: V2.TargetKind,
  path: string,
): string | null {
  const abs = join(home, path);
  if (!abs.startsWith(home + sep) || !validRepoPath(path)) {
    return `${abs} is outside your home folder`;
  }
  if (!validHomePath(kind, path)) {
    const folder = path.split("/")[0];
    return folder.startsWith(".")
      ? `${tilde(home, abs)} isn't a file Memax writes for ${AGENT[kind] ?? kind}`
      : `~/${folder} isn't a dot folder of your home folder; Memax writes home files only in folders like ~/.claude`;
  }
  const agent = agentFolder(kind, path);
  if (agent !== null) {
    const st = lstatOrNull(join(home, agent));
    if (!st || (!st.isDirectory() && !st.isSymbolicLink())) {
      return `${AGENT[kind] ?? kind} isn't set up here any more: ${tilde(home, join(home, agent))} is gone`;
    }
  }
  // Every folder on the way, and the file: none may be a link.
  let dir = home;
  let deepest = home;
  for (const s of path.split("/")) {
    dir = join(dir, s);
    const st = lstatOrNull(dir);
    if (!st) break;
    if (st.isSymbolicLink()) {
      // A dotfiles manager's link says more than "a symlink".
      const real = realOrNull(dir);
      const tree = real ? gitWorkTree(real) : null;
      if (tree) return dotfiles(home, dir, tree);
      return `${tilde(home, dir)} is a symlink; Memax doesn't write through links`;
    }
    deepest = dir;
  }
  const tree = gitWorkTree(deepest);
  if (tree) {
    const shown = agent !== null ? join(home, agent) : dirname(abs);
    return dotfiles(home, shown, tree);
  }
  return null;
}

function dotfiles(home: string, what: string, tree: string): string {
  return `${tilde(home, what)} is in ${tilde(home, tree)}, a git repository. Your Personal space would be committed with it`;
}

/** The home link's place: only its files, only as they were approved. */
export function homePlace(link: HomeLink): Place {
  const home = link.home;
  const fileOf = (id: string) => link.files.find((f) => f.target_id === id);
  return {
    kind: "home",
    root: home,
    refusal(t: PlaceTarget, file: string): string | null {
      if (t.root !== "home") return "isn't a home file";
      const f = fileOf(t.id);
      if (!f) return "isn't a file you approved on this machine";
      if (t.path !== f.path || file !== f.path) {
        return `isn't ${f.path}, the file you approved for it on this machine`;
      }
      return validHomePath(t.kind, file) ? null : "isn't a home file's path";
    },
    async resolve(rel: string): Promise<string> {
      if (!validRepoPath(rel)) {
        throw new UnsafePathError(rel, "isn't a path inside your home folder");
      }
      let dir = home;
      const segments = rel.split("/");
      for (const s of segments.slice(0, -1)) {
        dir = join(dir, s);
        let st;
        try {
          st = await lstat(dir);
        } catch {
          break; // created by ensureParents
        }
        if (st.isSymbolicLink() || !st.isDirectory()) {
          throw new UnsafePathError(
            rel,
            `${tilde(home, dir)} is a symlink or not a folder; Memax doesn't write through links`,
          );
        }
      }
      return join(home, ...segments);
    },
    async ensureParents(rel: string): Promise<void> {
      const segments = rel.split("/").slice(0, -1);
      const t = link.files.find((f) => f.path === rel);
      const agent = t ? agentFolder(t.kind, t.path) : null;
      let dir = home;
      let at = "";
      for (const s of segments) {
        dir = join(dir, s);
        at = at ? `${at}/${s}` : s;
        // An agent's own folder is never created; anything below it, and
        // the canonical copy's folders, are Memax's, readable by you only.
        if (agent === null || at.length > agent.length) {
          try {
            await mkdir(dir, { mode: 0o700 });
          } catch (err) {
            if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
          }
        }
        let st;
        try {
          st = await lstat(dir);
        } catch {
          throw new UnsafePathError(rel, `${tilde(home, dir)} isn't here`);
        }
        if (st.isSymbolicLink() || !st.isDirectory()) {
          throw new UnsafePathError(
            rel,
            `${tilde(home, dir)} is a symlink or not a folder`,
          );
        }
      }
    },
    async unwritable(t: PlaceTarget, rel: string): Promise<string | null> {
      return homeFileRefusal(home, t.kind, rel);
    },
    newFileMode: 0o600,
    removesOwnedWhenOff: true,
  };
}
