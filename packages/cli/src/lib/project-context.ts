import { execSync } from "node:child_process";
import {
  existsSync,
  readFileSync,
  realpathSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import type { Scope } from "memax-sdk";

// =============================================================================
// Git URL normalization — canonical project identity for cross-machine sync
// =============================================================================

/**
 * Normalize a git remote URL to a canonical form for use as a project identifier.
 *
 * All of these produce the same result: `github.com/acme/project`
 *   - https://github.com/acme/project.git
 *   - git@github.com:acme/project.git
 *   - ssh://git@github.com/acme/project
 */
export function normalizeRepoUrl(url: string): string {
  let s = url.trim();
  // Strip protocol (https://, ssh://, git://)
  s = s.replace(/^(https?:\/\/|ssh:\/\/|git:\/\/)/, "");
  // Strip user@ prefix (e.g., git@)
  s = s.replace(/^[^@]+@/, "");
  // SSH colon syntax → slash (github.com:org/repo → github.com/org/repo)
  // Skip if followed by digits (port number like git.corp.com:8443/repo)
  s = s.replace(/^([^/:]+):(?![0-9])/, "$1/");
  // Remove .git suffix
  s = s.replace(/\.git$/, "");
  // Trailing slashes
  s = s.replace(/\/+$/, "");
  // Lowercase + forward slashes
  s = s.toLowerCase().replace(/\\/g, "/");
  return s;
}

export interface MemaxYmlConfig {
  hub?: string;
  project_id?: string;
  /** The V2 space the repository compiles from (`memax link`). */
  space?: string;
}

export type ProjectScope = Scope | "project";

export interface ProjectScopeResolution {
  scope: ProjectScope;
  source: "memax_yml" | "git_remote" | "fallback";
  warning?: string;
}

export function isCanonicalProjectScope(scope: ProjectScope): scope is Scope {
  return scope !== "project";
}

function normalizeProjectID(projectID: string): string {
  let s = projectID.trim();
  s = s.replace(/^project:/, "");
  s = normalizeRepoUrl(s);
  if (!s || /\s/.test(s)) {
    throw new Error("invalid project_id");
  }
  return s;
}

function findNearestMemaxYml(startDir: string): string | null {
  let dir = startDir;
  const root = "/";
  while (true) {
    const ymlPath = join(dir, ".memax.yml");
    if (existsSync(ymlPath)) {
      return ymlPath;
    }
    const parent = join(dir, "..");
    if (parent === dir || dir === root) break;
    dir = parent;
  }
  return null;
}

export function findNearestMemaxYmlPath(dir?: string): string | null {
  return findNearestMemaxYml(dir ?? process.cwd());
}

export function readMemaxYmlConfig(dir?: string): MemaxYmlConfig | undefined {
  const cwd = dir ?? process.cwd();
  const ymlPath = findNearestMemaxYml(cwd);
  if (!ymlPath) {
    return undefined;
  }

  try {
    const content = readFileSync(ymlPath, "utf-8");
    const hubMatch = content.match(/^hub:\s*(.+)$/m);
    const projectMatch = content.match(/^project_id:\s*(.+)$/m);
    const spaceMatch = content.match(SPACE_LINE);

    const cfg: MemaxYmlConfig = {};
    if (hubMatch) {
      cfg.hub = hubMatch[1].trim();
    }
    if (projectMatch) {
      cfg.project_id = normalizeProjectID(projectMatch[1].trim());
    }
    if (spaceMatch) {
      cfg.space = unquote(spaceMatch[1].trim());
    }
    if (!cfg.hub && !cfg.project_id && !cfg.space) {
      return undefined;
    }
    return cfg;
  } catch {
    return undefined;
  }
}

function getGitOriginProjectID(cwd: string): string | undefined {
  try {
    const repo = execSync("git remote get-url origin", {
      cwd,
      encoding: "utf-8",
      timeout: 2000,
      stdio: ["pipe", "pipe", "pipe"],
    }).trim();
    if (repo) {
      return normalizeRepoUrl(repo);
    }
  } catch {}
  return undefined;
}

/**
 * Get a canonical project scope string for the given directory.
 *
 * Returns:
 *   "project:<project-id>"           — .memax.yml project_id or git remote available
 *   "project"                        — no canonical cross-device project identity
 */
export function getProjectScope(dir?: string): ProjectScope {
  return resolveProjectScope(dir).scope;
}

export function resolveProjectScope(dir?: string): ProjectScopeResolution {
  const cwd = dir ?? process.cwd();
  const memaxCfg = readMemaxYmlConfig(cwd);
  const gitProjectID = getGitOriginProjectID(cwd);

  if (memaxCfg?.project_id) {
    const warning =
      gitProjectID && gitProjectID !== memaxCfg.project_id
        ? `.memax.yml project_id (${memaxCfg.project_id}) overrides git origin (${gitProjectID})`
        : undefined;
    return {
      scope: `project:${memaxCfg.project_id}`,
      source: "memax_yml",
      warning,
    };
  }

  if (gitProjectID) {
    return {
      scope: `project:${gitProjectID}`,
      source: "git_remote",
    };
  }

  return {
    scope: "project",
    source: "fallback",
  };
}

export function resolveProjectRootPath(dir?: string): string | null {
  const cwd = dir ?? process.cwd();
  const ymlPath = findNearestMemaxYml(cwd);
  if (ymlPath) {
    return dirname(ymlPath);
  }

  try {
    const root = execSync("git rev-parse --show-toplevel", {
      cwd,
      encoding: "utf-8",
      timeout: 2000,
      stdio: ["pipe", "pipe", "pipe"],
    }).trim();
    return root || null;
  } catch {}

  return null;
}

/**
 * Resolve a Claude Code mangled project folder name to a normalized repo URL.
 *
 * Claude Code names project folders by replacing "/" with "-" in the absolute path:
 *   "-workspaces-project"         → /workspaces/project
 *   "-Users-ziyang-code-memax"    → /Users/ziyang/code/memax
 *
 * Returns the normalized repo URL if the path exists and is a git repo, null otherwise.
 */
export function resolveClaudeProjectFolder(mangledName: string): string | null {
  // Convert mangled name back to absolute path
  const absolutePath = mangledName.replace(/-/g, "/");
  if (!existsSync(absolutePath)) return null;
  const resolution = resolveProjectScope(absolutePath);
  if (resolution.scope === "project") {
    return null;
  }
  return resolution.scope.replace(/^project:/, "");
}

export function resolveClaudeProjectPath(mangledName: string): string | null {
  const absolutePath = mangledName.replace(/-/g, "/");
  if (!existsSync(absolutePath)) return null;
  return absolutePath;
}

/**
 * Normalize a file path for cross-platform consistency.
 *   - Forward slashes only
 *   - No leading "./"
 *   - No double slashes
 */
export function normalizeFilePath(p: string): string {
  return p.replace(/\\/g, "/").replace(/^\.\//, "").replace(/\/+/g, "/");
}

/** Detect git project context (repo URL, project name, branch). */
export function detectProjectContext(dir?: string): Record<string, string> {
  const cwd = dir ?? process.cwd();
  const ctx: Record<string, string> = {};
  const gitOpts = {
    cwd,
    encoding: "utf-8" as const,
    timeout: 2000,
    stdio: ["pipe", "pipe", "pipe"] as ["pipe", "pipe", "pipe"],
  };
  try {
    const repo = execSync("git remote get-url origin", gitOpts).trim();
    if (repo) ctx.repo = repo;
  } catch {}
  try {
    const project = execSync("git rev-parse --show-toplevel", gitOpts).trim();
    if (project) ctx.project = project.split("/").pop()!;
  } catch {}
  try {
    const branch = execSync("git branch --show-current", gitOpts).trim();
    if (branch) ctx.branch = branch;
  } catch {}
  return ctx;
}

/** Walk up from cwd looking for .memax.yml with a hub field. */
export function readMemaxYmlHub(): string | undefined {
  return readMemaxYmlConfig()?.hub;
}

// =============================================================================
// V2: the space a repository is linked to (`space:` in .memax.yml)
// =============================================================================

const SPACE_LINE = /^space:\s*(.+?)\s*$/m;
const SPACE_VALUE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

function unquote(v: string): string {
  const m = v.match(/^(["'])(.*)\1$/);
  return m ? m[2] : v;
}

/** The repository's root: git's top level, as a real path; null outside git. */
export function gitRoot(dir?: string): string | null {
  try {
    const root = execSync("git rev-parse --show-toplevel", {
      cwd: dir ?? process.cwd(),
      encoding: "utf-8",
      timeout: 2000,
      stdio: ["pipe", "pipe", "pipe"],
    }).trim();
    return root ? realpathSync(root) : null;
  } catch {
    return null;
  }
}

/**
 * Sets `space:` in `<root>/.memax.yml`, keeping every other line. Returns
 * whether the file changed.
 */
export function writeMemaxYmlSpace(root: string, space: string): boolean {
  if (!SPACE_VALUE.test(space)) {
    throw new Error(
      `can't write ${JSON.stringify(space)} as a space in .memax.yml`,
    );
  }
  const path = join(root, ".memax.yml");
  const before = existsSync(path) ? readFileSync(path, "utf-8") : "";
  const line = `space: ${space}`;
  let after: string;
  if (SPACE_LINE.test(before)) {
    after = before.replace(SPACE_LINE, line);
  } else if (before === "") {
    after = `# The Memax space this repository compiles from (memax link).\n${line}\n`;
  } else {
    after = before + (before.endsWith("\n") ? "" : "\n") + line + "\n";
  }
  if (after === before) return false;
  writeFileSync(path, after);
  return true;
}

/**
 * Takes `space:` out of `<root>/.memax.yml`, and the file with it when
 * nothing but comments would be left. Returns whether anything changed.
 */
export function removeMemaxYmlSpace(root: string): boolean {
  const path = join(root, ".memax.yml");
  if (!existsSync(path)) return false;
  const before = readFileSync(path, "utf-8");
  if (!SPACE_LINE.test(before)) return false;
  const after = before.replace(/^space:.*(\r?\n|$)/m, "");
  const meaningful = after
    .split(/\r?\n/)
    .some((l) => l.trim() !== "" && !l.trim().startsWith("#"));
  if (meaningful) writeFileSync(path, after);
  else unlinkSync(path);
  return true;
}
