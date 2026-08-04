import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import {
  getProjectScope,
  resolveClaudeProjectFolder,
  normalizeFilePath,
  isCanonicalProjectScope,
  type ProjectScope,
} from "../lib/project-context.js";
import { classifyAgentConfigFile } from "memax-sdk";
import type { AgentConfigClass, Scope } from "memax-sdk";

/**
 * Agent config discovery + write-path resolution — the single registry of
 * which on-disk files each agent owns. Detection is existence-based: every
 * candidate location is listed here and the sync engine filters to files
 * that actually exist.
 *
 * Two kinds of agents live in this registry:
 * - Coding agents (Claude Code, Cursor, Codex, …) — configs are project
 *   conventions and rules files, mostly project-scoped.
 * - Personal agents (OpenClaw, Hermes) — configs are identity (SOUL.md)
 *   and accumulated memory. These are irreplaceable, so coverage errs
 *   toward inclusion — but NEVER settings files (.env, *.json, *.yaml
 *   at the agent root): they routinely hold tokens and secrets.
 */

/** Files larger than this are skipped by sync — configs are stored inline
 * in Postgres, not object storage. */
export const MAX_AGENT_CONFIG_BYTES = 512 * 1024;

export interface AgentConfigLocation {
  agent: string; // "claude-code", "cursor", "hermes", etc.
  label: string; // display label (e.g. "~/.claude/CLAUDE.md")
  path: string; // absolute path on disk
  filePath: string; // relative path for storage (e.g. "CLAUDE.md")
  scope: Scope; // "global" | "project:<repo-url>" | "profile:<name>"
  configClass: AgentConfigClass; // identity | memory | rules | settings
}

export interface ResolveAgentConfigWritePathOptions {
  cwd?: string;
  home?: string;
  currentProjectScope?: ProjectScope;
  findClaudeProjectDir?: (scope: Scope) => string | null;
}

/** Profile names come from cloud-supplied scope strings — restrict to a
 * conservative charset so they can never traverse out of the profiles dir. */
const SAFE_PROFILE_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

function profileNameFromScope(scope: string): string | null {
  if (!scope.startsWith("profile:")) return null;
  const name = scope.slice("profile:".length);
  if (!SAFE_PROFILE_NAME.test(name) || name.includes("..")) return null;
  return name;
}

/** Cloud-supplied file paths must never escape the agent's root directory. */
function hasPathTraversal(filePath: string): boolean {
  return filePath.split("/").some((segment) => segment === "..");
}

export function findClaudeProjectDir(scope: Scope): string | null {
  const home = homedir();
  const claudeProjectsDir = join(home, ".claude", "projects");
  if (!existsSync(claudeProjectsDir)) return null;
  try {
    for (const project of readdirSync(claudeProjectsDir)) {
      const repoUrl = resolveClaudeProjectFolder(project);
      if (repoUrl && scope === `project:${repoUrl}`) {
        return join(claudeProjectsDir, project);
      }
    }
  } catch {
    // Permission denied — skip
  }
  return null;
}

export function resolveAgentConfigWritePath(
  agent: string,
  filePath: string,
  scope: Scope,
  options: ResolveAgentConfigWritePathOptions = {},
): string | null {
  const cwd = options.cwd ?? process.cwd();
  const home = options.home ?? homedir();
  const currentProjectScope =
    options.currentProjectScope ?? getProjectScope(cwd);
  const normalizedFilePath = normalizeFilePath(filePath);
  if (hasPathTraversal(normalizedFilePath)) return null;

  if (scope === "global") {
    switch (agent) {
      case "claude-code":
        return join(home, ".claude", normalizedFilePath);
      case "codex":
        return join(home, ".codex", normalizedFilePath);
      case "gemini":
        return join(home, ".gemini", normalizedFilePath);
      case "openclaw":
        return join(home, ".openclaw", normalizedFilePath);
      case "opencode":
        return join(home, ".opencode", normalizedFilePath);
      case "hermes":
        return join(home, ".hermes", normalizedFilePath);
      default:
        return null;
    }
  }

  if (scope.startsWith("profile:")) {
    const profile = profileNameFromScope(scope);
    if (!profile) return null;
    switch (agent) {
      case "hermes":
        return join(home, ".hermes", "profiles", profile, normalizedFilePath);
      default:
        return null;
    }
  }

  if (!scope.startsWith("project:") || scope !== currentProjectScope) {
    return null;
  }

  switch (agent) {
    case "claude-code":
      if (
        normalizedFilePath === "CLAUDE.md" ||
        normalizedFilePath === "MEMORY.md"
      ) {
        return join(cwd, ".claude", normalizedFilePath);
      }
      if (normalizedFilePath.startsWith(".claude/")) {
        return join(cwd, normalizedFilePath);
      }
      if (normalizedFilePath.startsWith("memory/")) {
        const projectDir = options.findClaudeProjectDir?.(scope);
        if (projectDir) {
          return join(projectDir, normalizedFilePath);
        }
        const mangledCwd = cwd.replace(/\//g, "-");
        return join(
          home,
          ".claude",
          "projects",
          mangledCwd,
          normalizedFilePath,
        );
      }
      return null;
    case "cursor":
      if (
        normalizedFilePath === ".cursorrules" ||
        normalizedFilePath.startsWith(".cursor/")
      ) {
        return join(cwd, normalizedFilePath);
      }
      return null;
    case "codex":
      if (normalizedFilePath === "instructions.md") {
        return join(cwd, ".codex", "instructions.md");
      }
      if (normalizedFilePath.startsWith(".codex/")) {
        return join(cwd, normalizedFilePath);
      }
      return null;
    case "gemini":
      if (normalizedFilePath === "GEMINI.md") {
        return join(cwd, "GEMINI.md");
      }
      return null;
    case "copilot":
      if (normalizedFilePath === "copilot-instructions.md") {
        return join(cwd, ".github", "copilot-instructions.md");
      }
      if (normalizedFilePath.startsWith(".github/")) {
        return join(cwd, normalizedFilePath);
      }
      return null;
    case "windsurf":
      if (
        normalizedFilePath === ".windsurfrules" ||
        normalizedFilePath.startsWith(".windsurf/")
      ) {
        return join(cwd, normalizedFilePath);
      }
      return null;
    case "opencode":
      if (normalizedFilePath.startsWith(".opencode/")) {
        return join(cwd, normalizedFilePath);
      }
      return join(cwd, ".opencode", normalizedFilePath);
    case "generic":
      if (
        normalizedFilePath === "AGENTS.md" ||
        normalizedFilePath === "CLAUDE.md" ||
        normalizedFilePath === "GEMINI.md"
      ) {
        return join(cwd, normalizedFilePath);
      }
      return null;
    default:
      return null;
  }
}

/** OpenClaw/Hermes bootstrap markdown files — identity + memory surfaces.
 * Settings (openclaw.json, config.yaml, .env) are deliberately absent. */
const OPENCLAW_BOOTSTRAP_FILES = [
  "SOUL.md",
  "IDENTITY.md",
  "USER.md",
  "AGENTS.md",
  "TOOLS.md",
  "MEMORY.md",
];
const HERMES_BOOTSTRAP_FILES = ["SOUL.md", "MEMORY.md", "AGENTS.md"];

export function discoverAgentConfigs(): AgentConfigLocation[] {
  const home = homedir();
  const cwd = process.cwd();
  const projectScope = getProjectScope(cwd);
  const canonicalProjectScope = isCanonicalProjectScope(projectScope)
    ? projectScope
    : null;
  const locations: AgentConfigLocation[] = [];

  const add = (
    agent: string,
    label: string,
    path: string,
    filePath: string,
    scope: Scope = "global",
  ) => {
    const normalized = normalizeFilePath(filePath);
    locations.push({
      agent,
      label,
      path,
      filePath: normalized,
      scope,
      configClass: classifyAgentConfigFile(agent, normalized),
    });
  };

  /** Scan a directory for markdown files and register each one. */
  const addMarkdownDir = (
    agent: string,
    labelPrefix: string,
    dir: string,
    filePathPrefix: string,
    scope: Scope = "global",
    extraExtensions: string[] = [],
  ) => {
    if (!existsSync(dir)) return;
    try {
      for (const file of readdirSync(dir)) {
        const keep =
          file.endsWith(".md") || extraExtensions.some((e) => file.endsWith(e));
        if (!keep) continue;
        add(
          agent,
          `${labelPrefix}/${file}`,
          join(dir, file),
          `${filePathPrefix}/${file}`,
          scope,
        );
      }
    } catch {
      /* Permission denied — skip */
    }
  };

  // Claude Code — global
  add(
    "claude-code",
    "~/.claude/CLAUDE.md",
    join(home, ".claude", "CLAUDE.md"),
    "CLAUDE.md",
  );
  add(
    "claude-code",
    "~/.claude/MEMORY.md",
    join(home, ".claude", "MEMORY.md"),
    "MEMORY.md",
  );

  // Claude Code — per-project memories: ~/.claude/projects/*/memory/*.md
  // The folder name is the absolute project path with "/" replaced by "-"
  // (e.g., "-workspaces-memax"). We resolve it to a git repo URL so the
  // same project's memories match across machines regardless of clone path.
  const claudeProjectsDir = join(home, ".claude", "projects");
  if (existsSync(claudeProjectsDir)) {
    try {
      for (const project of readdirSync(claudeProjectsDir)) {
        const memoryDir = join(claudeProjectsDir, project, "memory");
        if (!existsSync(memoryDir)) continue;

        // Try to resolve mangled folder → git repo → canonical scope
        const repoUrl = resolveClaudeProjectFolder(project);
        const memoryScope: Scope | undefined = repoUrl
          ? `project:${repoUrl}`
          : undefined;

        try {
          for (const file of readdirSync(memoryDir)) {
            if (!file.endsWith(".md")) continue;
            if (memoryScope) {
              // Canonical: filePath is just "memory/<file>", scope identifies the project
              add(
                "claude-code",
                `~/.claude/projects/${project}/memory/${file}`,
                join(memoryDir, file),
                `memory/${file}`,
                memoryScope,
              );
            } else {
              // Fallback: can't resolve project → keep legacy format with folder name
              add(
                "claude-code",
                `~/.claude/projects/${project}/memory/${file}`,
                join(memoryDir, file),
                `projects/${project}/memory/${file}`,
              );
            }
          }
        } catch {
          // Permission denied — skip
        }
      }
    } catch {
      // Permission denied — skip
    }
  }

  // --- Project-scoped configs (only when inside a git repo) ---
  if (canonicalProjectScope) {
    // Claude Code — project-level
    add(
      "claude-code",
      "./.claude/CLAUDE.md",
      join(cwd, ".claude", "CLAUDE.md"),
      "CLAUDE.md",
      canonicalProjectScope,
    );

    // Cursor (project-level)
    add(
      "cursor",
      "./.cursorrules",
      join(cwd, ".cursorrules"),
      ".cursorrules",
      canonicalProjectScope,
    );
    const cursorRulesDir = join(cwd, ".cursor", "rules");
    if (existsSync(cursorRulesDir)) {
      try {
        for (const file of readdirSync(cursorRulesDir)) {
          if (file.endsWith(".mdc")) {
            add(
              "cursor",
              `./.cursor/rules/${file}`,
              join(cursorRulesDir, file),
              `.cursor/rules/${file}`,
              canonicalProjectScope,
            );
          }
        }
      } catch {
        /* skip */
      }
    }

    // Codex (project-level)
    add(
      "codex",
      "./.codex/instructions.md",
      join(cwd, ".codex", "instructions.md"),
      "instructions.md",
      canonicalProjectScope,
    );
  }

  add(
    "codex",
    "~/.codex/AGENTS.md",
    join(home, ".codex", "AGENTS.md"),
    "AGENTS.md",
  );

  // Gemini CLI — global
  add(
    "gemini",
    "~/.gemini/GEMINI.md",
    join(home, ".gemini", "GEMINI.md"),
    "GEMINI.md",
  );

  if (canonicalProjectScope) {
    // Gemini CLI — project-level
    add(
      "gemini",
      "./GEMINI.md",
      join(cwd, "GEMINI.md"),
      "GEMINI.md",
      canonicalProjectScope,
    );

    // GitHub Copilot
    add(
      "copilot",
      "./.github/copilot-instructions.md",
      join(cwd, ".github", "copilot-instructions.md"),
      "copilot-instructions.md",
      canonicalProjectScope,
    );

    // Windsurf
    add(
      "windsurf",
      "./.windsurfrules",
      join(cwd, ".windsurfrules"),
      ".windsurfrules",
      canonicalProjectScope,
    );
    const windsurfRulesDir = join(cwd, ".windsurf", "rules");
    if (existsSync(windsurfRulesDir)) {
      try {
        for (const file of readdirSync(windsurfRulesDir)) {
          if (file.endsWith(".md")) {
            add(
              "windsurf",
              `./.windsurf/rules/${file}`,
              join(windsurfRulesDir, file),
              `.windsurf/rules/${file}`,
              canonicalProjectScope,
            );
          }
        }
      } catch {
        /* skip */
      }
    }
  }

  // OpenClaw — personal agent. Identity + memory markdown at the state root
  // and inside the workspace, plus skills. The agent's accumulated identity
  // is irreplaceable, so coverage is broad — but settings JSON stays out.
  const openclawDir = join(home, ".openclaw");
  for (const file of OPENCLAW_BOOTSTRAP_FILES) {
    add("openclaw", `~/.openclaw/${file}`, join(openclawDir, file), file);
    add(
      "openclaw",
      `~/.openclaw/workspace/${file}`,
      join(openclawDir, "workspace", file),
      `workspace/${file}`,
    );
  }
  // Legacy memory dir (kept for existing users — includes .json memory state)
  addMarkdownDir(
    "openclaw",
    "~/.openclaw/memory",
    join(openclawDir, "memory"),
    "memory",
    "global",
    [".json"],
  );
  addMarkdownDir(
    "openclaw",
    "~/.openclaw/workspace/memory",
    join(openclawDir, "workspace", "memory"),
    "workspace/memory",
  );
  // Skills — one SKILL.md per skill directory
  const openclawSkillsDir = join(openclawDir, "skills");
  if (existsSync(openclawSkillsDir)) {
    try {
      for (const skill of readdirSync(openclawSkillsDir)) {
        const skillFile = join(openclawSkillsDir, skill, "SKILL.md");
        if (existsSync(skillFile)) {
          add(
            "openclaw",
            `~/.openclaw/skills/${skill}/SKILL.md`,
            skillFile,
            `skills/${skill}/SKILL.md`,
          );
        }
      }
    } catch {
      /* skip */
    }
  }

  // Hermes — personal agent with named profiles. The default (root) profile
  // syncs as global scope; each ~/.hermes/profiles/<name>/ syncs under
  // profile:<name> so every device restores the same profile set.
  // config.yaml and .env are settings/secrets — never synced.
  const hermesDir = join(home, ".hermes");
  for (const file of HERMES_BOOTSTRAP_FILES) {
    add("hermes", `~/.hermes/${file}`, join(hermesDir, file), file);
  }
  addMarkdownDir(
    "hermes",
    "~/.hermes/memory",
    join(hermesDir, "memory"),
    "memory",
  );
  const hermesProfilesDir = join(hermesDir, "profiles");
  if (existsSync(hermesProfilesDir)) {
    try {
      for (const profile of readdirSync(hermesProfilesDir)) {
        if (!SAFE_PROFILE_NAME.test(profile)) continue;
        const profileDir = join(hermesProfilesDir, profile);
        const scope: Scope = `profile:${profile}`;
        for (const file of HERMES_BOOTSTRAP_FILES) {
          add(
            "hermes",
            `~/.hermes/profiles/${profile}/${file}`,
            join(profileDir, file),
            file,
            scope,
          );
        }
        addMarkdownDir(
          "hermes",
          `~/.hermes/profiles/${profile}/memory`,
          join(profileDir, "memory"),
          "memory",
          scope,
        );
      }
    } catch {
      /* skip */
    }
  }

  if (canonicalProjectScope) {
    // OpenCode (project-level)
    const opencodePath = join(cwd, ".opencode");
    if (existsSync(opencodePath)) {
      try {
        for (const file of readdirSync(opencodePath)) {
          if (file.endsWith(".md")) {
            add(
              "opencode",
              `./.opencode/${file}`,
              join(opencodePath, file),
              file,
              canonicalProjectScope,
            );
          }
        }
      } catch {
        /* skip */
      }
    }

    // Generic project-level agent files
    add(
      "generic",
      "./AGENTS.md",
      join(cwd, "AGENTS.md"),
      "AGENTS.md",
      canonicalProjectScope,
    );
    add(
      "generic",
      "./CLAUDE.md",
      join(cwd, "CLAUDE.md"),
      "CLAUDE.md",
      canonicalProjectScope,
    );
  }

  return locations;
}

export function formatAgentName(id: string): string {
  const names: Record<string, string> = {
    "claude-code": "Claude Code",
    cursor: "Cursor",
    codex: "Codex",
    gemini: "Gemini CLI",
    copilot: "GitHub Copilot",
    windsurf: "Windsurf",
    openclaw: "OpenClaw",
    hermes: "Hermes",
    opencode: "OpenCode",
    generic: "Generic",
  };
  return names[id] ?? id;
}
