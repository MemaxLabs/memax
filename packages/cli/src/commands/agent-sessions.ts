import { Command } from "commander";
import chalk from "chalk";
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { basename, dirname, join, relative } from "node:path";
import { homedir } from "node:os";
import { getClient } from "../lib/client.js";
import {
  getProjectScope,
  resolveClaudeProjectPath,
  resolveClaudeProjectFolder,
  resolveProjectRootPath,
  resolveProjectScope,
  normalizeFilePath,
  isCanonicalProjectScope,
  type ProjectScope,
} from "../lib/project-context.js";
import { getOrCreateDeviceID, loadConfig } from "../lib/config.js";
import { ask, confirmDefault } from "../lib/prompt.js";
import { moveFileToTrash } from "../lib/trash.js";
import type {
  AgentSession,
  SessionSyncPlanAction,
  UploadIntent,
  Scope,
} from "memax-sdk";

interface AgentSessionLocation {
  agent: string;
  path: string;
  filePath: string;
  scope: Scope;
  sessionType: string;
  projectRoot?: string;
  contentHash?: string;
}

interface AgentSessionPlacement {
  kind: "present" | "restorable" | "different_project" | "unresolved";
  path?: string;
  reason: string;
}

const PROJECT_ROOT_PLACEHOLDER = "__MEMAX_PROJECT_ROOT__";

interface SessionProjectContext {
  scope: Scope;
  projectRoot?: string;
}

const PORTABLE_SESSION_AGENTS = new Set(["claude-code", "codex", "gemini"]);

function isScopeValue(value: string): value is Scope {
  return value === "global" || value.startsWith("project:");
}

function isWithinRoot(candidate: string, root: string): boolean {
  return candidate === root || candidate.startsWith(`${root}/`);
}

function replaceProjectRootPrefix(
  value: string,
  fromRoot: string,
  toRoot: string,
): string {
  if (!isWithinRoot(value, fromRoot)) return value;
  if (value === fromRoot) return toRoot;
  return `${toRoot}${value.slice(fromRoot.length)}`;
}

function transformStructuredCwdFields(
  value: unknown,
  map: (cwd: string) => string,
): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => transformStructuredCwdFields(item, map));
  }
  if (!value || typeof value !== "object") {
    return value;
  }
  const next: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value)) {
    if (key === "cwd" && typeof child === "string") {
      next[key] = map(child);
      continue;
    }
    next[key] = transformStructuredCwdFields(child, map);
  }
  return next;
}

function transformJsonLinesByCwd(
  content: Buffer,
  map: (cwd: string) => string,
): Buffer {
  const lines = content.toString("utf-8").split("\n");
  const transformed = lines.map((line) => {
    if (!line.trim()) return line;
    try {
      const parsed = JSON.parse(line);
      return JSON.stringify(transformStructuredCwdFields(parsed, map));
    } catch {
      return line;
    }
  });
  return Buffer.from(transformed.join("\n"), "utf-8");
}

function transformJsonObjectByCwd(
  content: Buffer,
  map: (cwd: string) => string,
): Buffer {
  try {
    const parsed = JSON.parse(content.toString("utf-8"));
    return Buffer.from(
      `${JSON.stringify(transformStructuredCwdFields(parsed, map), null, 2)}\n`,
      "utf-8",
    );
  } catch {
    return content;
  }
}

function canonicalizeSessionContent(
  agent: string,
  content: Buffer,
  projectRoot?: string,
): Buffer {
  if (!projectRoot) return content;
  const map = (cwd: string) =>
    replaceProjectRootPrefix(cwd, projectRoot, PROJECT_ROOT_PLACEHOLDER);
  switch (agent) {
    case "claude-code":
    case "codex":
      return transformJsonLinesByCwd(content, map);
    case "gemini":
      return transformJsonObjectByCwd(content, map);
    default:
      return content;
  }
}

function renderPortableSessionContent(
  agent: string,
  content: Buffer,
  sourceProjectRoot: string | undefined,
  targetProjectRoot: string | undefined,
): Buffer {
  if (!sourceProjectRoot || !targetProjectRoot) return content;
  if (sourceProjectRoot === targetProjectRoot) return content;
  const map = (cwd: string) =>
    replaceProjectRootPrefix(cwd, sourceProjectRoot, targetProjectRoot);
  switch (agent) {
    case "claude-code":
    case "codex":
      return transformJsonLinesByCwd(content, map);
    case "gemini":
      return transformJsonObjectByCwd(content, map);
    default:
      return content;
  }
}

export function hashPortableSessionContent(
  agent: string,
  content: Buffer,
  projectRoot?: string,
): string {
  const canonical = canonicalizeSessionContent(agent, content, projectRoot);
  return createHash("sha256").update(canonical).digest("hex");
}

export function computeSessionSyncHash(
  agent: string,
  scope: Scope,
  content: Buffer,
  currentProjectRootPath: string,
): string {
  return hashPortableSessionContent(
    agent,
    content,
    scope.startsWith("project:") ? currentProjectRootPath : undefined,
  );
}

export function isLegacyGlobalSessionShadowed(
  action: SessionSyncPlanAction,
  projectScopedKeys: Set<string>,
): boolean {
  if (action.scope !== "global") return false;
  if (
    action.reason !== "cloud_only" &&
    action.reason !== "deleted_everywhere"
  ) {
    return false;
  }
  if (!PORTABLE_SESSION_AGENTS.has(action.agent)) return false;
  if (
    action.agent === "codex" &&
    normalizeFilePath(action.file_path) === "history.jsonl"
  ) {
    return false;
  }
  return projectScopedKeys.has(
    `${action.agent}|${normalizeFilePath(action.file_path)}`,
  );
}

interface ShadowedGlobalSessionPair {
  global: AgentSession;
  project: AgentSession;
}

export function findShadowedGlobalSessions(
  sessions: AgentSession[],
): ShadowedGlobalSessionPair[] {
  const projectScopedByKey = new Map<string, AgentSession[]>();
  for (const session of sessions) {
    if (!PORTABLE_SESSION_AGENTS.has(session.agent)) continue;
    if (!session.scope.startsWith("project:")) continue;
    const normalized = normalizeFilePath(session.file_path);
    if (session.agent === "codex" && normalized === "history.jsonl") continue;
    const key = `${session.agent}|${normalized}`;
    const group = projectScopedByKey.get(key) ?? [];
    group.push(session);
    projectScopedByKey.set(key, group);
  }

  const pairs: ShadowedGlobalSessionPair[] = [];
  for (const session of sessions) {
    if (!PORTABLE_SESSION_AGENTS.has(session.agent)) continue;
    if (session.scope !== "global") continue;
    const normalized = normalizeFilePath(session.file_path);
    if (session.agent === "codex" && normalized === "history.jsonl") continue;
    const siblings = projectScopedByKey.get(`${session.agent}|${normalized}`);
    if (!siblings) continue;
    for (const sibling of siblings) {
      pairs.push({ global: session, project: sibling });
    }
  }
  return pairs;
}

function computeDownloadedPortableHash(agent: string, content: Buffer): string {
  const projectRoot = readStructuredSessionRootFromContent(agent, content);
  return hashPortableSessionContent(agent, content, projectRoot ?? undefined);
}

function readStructuredSessionRootFromContent(
  agent: string,
  content: Buffer,
): string | null {
  try {
    const raw = content.toString("utf-8");
    switch (agent) {
      case "claude-code":
      case "codex": {
        for (const line of raw.split("\n")) {
          if (!line.trim()) continue;
          try {
            const parsed = JSON.parse(line);
            const cwd =
              typeof parsed?.cwd === "string"
                ? parsed.cwd
                : typeof parsed?.payload?.cwd === "string"
                  ? parsed.payload.cwd
                  : "";
            if (cwd) {
              return resolveProjectRootPath(cwd) ?? cwd;
            }
          } catch {}
        }
        return null;
      }
      case "gemini": {
        const parsed = JSON.parse(raw);
        const cwd = typeof parsed?.cwd === "string" ? parsed.cwd : "";
        if (cwd) {
          return resolveProjectRootPath(cwd) ?? cwd;
        }
        return null;
      }
      default:
        return null;
    }
  } catch {
    return null;
  }
}

function readStructuredSessionRoot(agent: string, path: string): string | null {
  try {
    return readStructuredSessionRootFromContent(agent, readFileSync(path));
  } catch {
    return null;
  }
}

export function computeSessionProjectContext(
  agent: string,
  path: string,
  fallbackProjectRoot?: string | null,
): SessionProjectContext {
  const projectRoot =
    fallbackProjectRoot ?? readStructuredSessionRoot(agent, path) ?? undefined;
  if (!projectRoot) {
    return { scope: "global" };
  }
  const resolvedRoot = resolveProjectRootPath(projectRoot) ?? projectRoot;
  const scope = getProjectScope(resolvedRoot);
  if (scope === "project" || !scope.startsWith("project:")) {
    return { scope: "global" };
  }
  return { scope, projectRoot: resolvedRoot };
}

function encodeClaudeProjectDir(projectRoot: string): string {
  return projectRoot.replace(/\//g, "-");
}

function ensureGeminiProjectDir(
  home: string,
  currentProjectRootPath: string,
  scope: Scope,
): string {
  const existing = findGeminiProjectDir(home, scope);
  if (existing) return existing;
  const dirName = `${basename(currentProjectRootPath)}-${createHash("sha256").update(scope).digest("hex").slice(0, 8)}`;
  const projectDir = join(home, ".gemini", "tmp", dirName);
  mkdirSync(projectDir, { recursive: true });
  return projectDir;
}

export function materializeAgentSessionContent(
  agent: string,
  content: Buffer,
  options: {
    scope: Scope;
    currentProjectRootPath: string;
    writePath: string;
  },
): Buffer {
  let next = content;
  if (options.scope.startsWith("project:")) {
    const sourceProjectRoot = readStructuredSessionRootFromContent(
      agent,
      content,
    );
    next = renderPortableSessionContent(
      agent,
      next,
      sourceProjectRoot ?? undefined,
      options.currentProjectRootPath,
    );
  }

  if (agent === "gemini" && options.scope.startsWith("project:")) {
    const projectDir = dirname(dirname(options.writePath));
    mkdirSync(projectDir, { recursive: true });
    writeFileSync(
      join(projectDir, ".project_root"),
      `${options.currentProjectRootPath}\n`,
    );
  }

  return next;
}

export async function syncAgentSessionsCommand(
  options: {
    push?: boolean;
    pull?: boolean;
  } = {},
): Promise<void> {
  console.log(chalk.bold("\n  Memax Session Sync\n"));

  const cwd = process.cwd();
  const home = homedir();
  const deviceID = getOrCreateDeviceID();
  const projectScopeResolution = resolveProjectScope(cwd);
  const currentProjectScope = projectScopeResolution.scope;
  const currentProjectRootPath = resolveProjectRootPath(cwd) ?? cwd;
  const locations = discoverAgentSessions();
  const localSessions = locations
    .filter((loc) => existsSync(loc.path))
    .map((loc) => {
      const content = readFileSync(loc.path);
      return {
        loc,
        content,
        hash: hashPortableSessionContent(loc.agent, content, loc.projectRoot),
        size: statSync(loc.path).size,
      };
    });

  const manifest = localSessions.map((session) => ({
    agent: session.loc.agent,
    file_path: session.loc.filePath,
    scope: session.loc.scope,
    content_hash: session.hash,
    local_path: session.loc.path,
  }));

  let actions: SessionSyncPlanAction[];
  try {
    const plan = await getClient().agentSessions.sync({
      device_id: deviceID,
      sessions: manifest,
    });
    actions = plan.actions;
  } catch (err) {
    console.error(chalk.red(`  Sync failed: ${(err as Error).message}\n`));
    return;
  }

  // Note: diverged sessions are NOT rewritten to push/pull — they use
  // resolveDivergence() which atomically snapshots the losing branch.
  // --push and --pull set the resolution direction for diverged sessions.

  actions = actions.filter((action) => {
    if (!action.scope.startsWith("project:")) return true;
    if (action.scope === currentProjectScope) return true;
    if (action.action === "pull" && action.reason === "cloud_only")
      return false;
    return true;
  });

  const projectScopedKeys = new Set(
    actions
      .filter((action) => action.scope.startsWith("project:"))
      .map(
        (action) => `${action.agent}|${normalizeFilePath(action.file_path)}`,
      ),
  );
  actions = actions.filter(
    (action) => !isLegacyGlobalSessionShadowed(action, projectScopedKeys),
  );

  const localByKey = new Map<string, (typeof localSessions)[number]>();
  for (const session of localSessions) {
    localByKey.set(
      `${session.loc.agent}|${session.loc.filePath}|${session.loc.scope}`,
      session,
    );
  }
  const locationByKey = new Map<string, AgentSessionLocation>();
  for (const location of locations) {
    locationByKey.set(
      `${location.agent}|${location.filePath}|${location.scope}`,
      location,
    );
  }

  const resolveWritePath = (
    agent: string,
    filePath: string,
    scope: Scope,
  ): string | null => {
    const existing = locationByKey.get(`${agent}|${filePath}|${scope}`);
    if (existing) return existing.path;
    return resolveAgentSessionWritePath(agent, filePath, scope, {
      cwd,
      home,
      currentProjectScope,
      currentProjectRootPath,
    });
  };

  let pushed = 0;
  let pulled = 0;
  let deletedLocal = 0;
  let reconciled = 0;
  let unchanged = 0;
  let skipped = 0;
  let errors = 0;
  const ackSessions: {
    agent: string;
    file_path: string;
    scope: Scope;
    content_hash?: string;
    version: number;
    local_path?: string;
    deleted?: boolean;
  }[] = [];

  const byAgent = new Map<string, SessionSyncPlanAction[]>();
  for (const action of actions) {
    const group = byAgent.get(action.agent) ?? [];
    group.push(action);
    byAgent.set(action.agent, group);
  }

  for (const [agent, agentActions] of byAgent) {
    console.log(chalk.white(`  ${formatAgentName(agent)}`));

    for (const action of agentActions) {
      const key = `${action.agent}|${action.file_path}|${action.scope}`;

      if (action.action === "unchanged") {
        const local = localByKey.get(key);
        console.log(
          chalk.gray(`    = ${action.file_path}`),
          chalk.gray("unchanged"),
        );
        if (local && action.version) {
          ackSessions.push({
            agent: action.agent,
            file_path: action.file_path,
            scope: action.scope,
            content_hash: local.hash,
            version: action.version,
            local_path: local.loc.path,
          });
        }
        unchanged++;
        continue;
      }

      if (action.action === "push") {
        const local = localByKey.get(key);
        if (!local) {
          console.log(
            chalk.red(`    ✗ ${action.file_path}`),
            chalk.gray("local file not found for push"),
          );
          errors++;
          continue;
        }
        try {
          const fileRef = await uploadLocalFile(local.loc.path, local.content);
          await getClient().agentSessions.upsert({
            agent: action.agent,
            file_path: action.file_path,
            scope: action.scope,
            session_type: local.loc.sessionType,
            content_hash: local.hash,
            device_id: deviceID,
            local_path: local.loc.path,
            file_ref: fileRef,
          });
          console.log(
            chalk.green(`    ↑ ${action.file_path}`),
            chalk.gray(
              action.reason === "local_only"
                ? "pushing (new)"
                : "pushing (local newer)",
            ),
          );
          pushed++;
        } catch (err) {
          if (err instanceof SessionOversizeError) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
              ),
            );
            skipped++;
          } else {
            console.log(
              chalk.red(`    ✗ ${action.file_path}`),
              chalk.gray((err as Error).message),
            );
            errors++;
          }
        }
        continue;
      }

      if (action.action === "pull") {
        if (!action.session_id) {
          console.log(
            chalk.red(`    ✗ ${action.file_path}`),
            chalk.gray("missing session ID from server"),
          );
          errors++;
          continue;
        }
        const writePath = resolveWritePath(
          action.agent,
          action.file_path,
          action.scope,
        );
        if (!writePath) {
          console.log(
            chalk.yellow(`    ? ${action.file_path}`),
            chalk.gray(
              action.scope !== "global" && action.scope !== currentProjectScope
                ? "different project — skipped"
                : "no safe restore path on this machine",
            ),
          );
          skipped++;
          continue;
        }
        try {
          const isNewLocally =
            action.reason === "cloud_only" && !existsSync(writePath);
          if (isNewLocally && !options.pull) {
            console.log(chalk.cyan(`    New file: ${action.file_path}`));
            console.log(chalk.gray(`    → ${writePath}`));
            const accept = await confirmDefault(`    Download? [Y/n] `);
            if (!accept) {
              console.log(
                chalk.gray(`    - ${action.file_path}`),
                chalk.gray("skipped"),
              );
              skipped++;
              continue;
            }
          }

          const session = await getClient().agentSessions.get(
            action.session_id,
          );
          const bytes = await downloadAgentSession(action.session_id);
          mkdirSync(dirname(writePath), { recursive: true });
          const materialized = materializeAgentSessionContent(
            action.agent,
            bytes,
            {
              scope: action.scope,
              currentProjectRootPath,
              writePath,
            },
          );
          writeFileSync(writePath, materialized);
          if (action.version) {
            ackSessions.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              content_hash: computeSessionSyncHash(
                action.agent,
                action.scope,
                materialized,
                currentProjectRootPath,
              ),
              version: action.version,
              local_path: writePath,
            });
          }
          console.log(
            chalk.cyan(`    ↓ ${action.file_path}`),
            chalk.gray(isNewLocally ? "restored" : "pulling (cloud newer)"),
          );
          pulled++;
        } catch (err) {
          if (err instanceof SessionOversizeError) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
              ),
            );
            skipped++;
          } else {
            console.log(
              chalk.red(`    ✗ ${action.file_path}`),
              chalk.gray((err as Error).message),
            );
            errors++;
          }
        }
        continue;
      }

      if (action.action === "delete_local") {
        if (isLegacyGlobalSessionShadowed(action, projectScopedKeys)) {
          if (action.version) {
            ackSessions.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              version: action.version,
              deleted: true,
            });
          }
          console.log(
            chalk.gray(`    = ${action.file_path}`),
            chalk.gray("legacy global duplicate removed"),
          );
          reconciled++;
          continue;
        }
        if (!options.pull) {
          if (!process.stdin.isTTY || !process.stdout.isTTY) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                "cloud deleted this session artifact — skipped in non-interactive mode",
              ),
            );
            skipped++;
            continue;
          }
          const resolution = await promptSessionCloudDeletion(action.file_path);
          if (resolution === "skip") {
            console.log(
              chalk.gray(`    - ${action.file_path}`),
              chalk.gray("skipped"),
            );
            skipped++;
            continue;
          }
          if (resolution === "local") {
            const local = localByKey.get(key);
            if (!local) {
              console.log(
                chalk.yellow(`    - ${action.file_path}`),
                chalk.gray("local file missing — skipped"),
              );
              skipped++;
              continue;
            }
            try {
              const fileRef = await uploadLocalFile(
                local.loc.path,
                local.content,
              );
              await getClient().agentSessions.upsert({
                agent: action.agent,
                file_path: action.file_path,
                scope: action.scope,
                session_type: local.loc.sessionType,
                content_hash: local.hash,
                device_id: deviceID,
                local_path: local.loc.path,
                file_ref: fileRef,
              });
              console.log(
                chalk.green(`    ↑ ${action.file_path}`),
                chalk.gray("kept local and restored to cloud"),
              );
              pushed++;
            } catch (err) {
              if (err instanceof SessionOversizeError) {
                console.log(
                  chalk.yellow(`    - ${action.file_path}`),
                  chalk.gray(
                    `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
                  ),
                );
                skipped++;
              } else {
                console.log(
                  chalk.red(`    ✗ ${action.file_path}`),
                  chalk.gray((err as Error).message),
                );
                errors++;
              }
            }
            continue;
          }
        }
        try {
          const writePath = resolveWritePath(
            action.agent,
            action.file_path,
            action.scope,
          );
          if (writePath && existsSync(writePath)) {
            moveFileToTrash(writePath, "agent-sessions");
          }
          if (action.version) {
            ackSessions.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              version: action.version,
              local_path: writePath ?? undefined,
              deleted: true,
            });
          }
          console.log(
            chalk.yellow(`    - ${action.file_path}`),
            chalk.gray("deleted locally (moved to Memax trash)"),
          );
          deletedLocal++;
        } catch (err) {
          if (err instanceof SessionOversizeError) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
              ),
            );
            skipped++;
          } else {
            console.log(
              chalk.red(`    ✗ ${action.file_path}`),
              chalk.gray((err as Error).message),
            );
            errors++;
          }
        }
        continue;
      }

      if (action.action === "tombstone_diverged") {
        // Cloud session is deleted; local copy has content that has changed
        // since the last ack. There is no live cloud branch to snapshot —
        // resolution is either re-create the cloud session from local, or
        // accept the deletion and remove the local file.
        let resolution: "keep_local" | "keep_cloud" | "skip";
        if (options.push) {
          resolution = "keep_local";
        } else if (options.pull) {
          resolution = "keep_cloud";
        } else if (!process.stdin.isTTY || !process.stdout.isTTY) {
          resolution = "keep_local";
        } else {
          const answer = await promptSessionCloudDeletion(action.file_path);
          resolution =
            answer === "local"
              ? "keep_local"
              : answer === "delete"
                ? "keep_cloud"
                : "skip";
        }

        if (resolution === "skip") {
          console.log(
            chalk.gray(`    - ${action.file_path}`),
            chalk.gray("skipped"),
          );
          skipped++;
          continue;
        }

        if (resolution === "keep_local") {
          const local = localByKey.get(key);
          if (!local) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray("local file missing — skipped"),
            );
            skipped++;
            continue;
          }
          try {
            const fileRef = await uploadLocalFile(
              local.loc.path,
              local.content,
            );
            await getClient().agentSessions.upsert({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              session_type: local.loc.sessionType,
              content_hash: local.hash,
              device_id: deviceID,
              local_path: local.loc.path,
              file_ref: fileRef,
            });
            console.log(
              chalk.green(`    ↑ ${action.file_path}`),
              chalk.gray("kept local, re-created on cloud"),
            );
            pushed++;
          } catch (err) {
            if (err instanceof SessionOversizeError) {
              console.log(
                chalk.yellow(`    - ${action.file_path}`),
                chalk.gray(
                  `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
                ),
              );
              skipped++;
            } else {
              console.log(
                chalk.red(`    ✗ ${action.file_path}`),
                chalk.gray((err as Error).message),
              );
              errors++;
            }
          }
          continue;
        }

        try {
          const writePath = resolveWritePath(
            action.agent,
            action.file_path,
            action.scope,
          );
          if (writePath && existsSync(writePath)) {
            moveFileToTrash(writePath, "agent-sessions");
          }
          if (action.version) {
            ackSessions.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              version: action.version,
              local_path: writePath ?? undefined,
              deleted: true,
            });
          }
          console.log(
            chalk.yellow(`    - ${action.file_path}`),
            chalk.gray("accepted cloud deletion, removed local"),
          );
          deletedLocal++;
        } catch (err) {
          if (err instanceof SessionOversizeError) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
              ),
            );
            skipped++;
          } else {
            console.log(
              chalk.red(`    ✗ ${action.file_path}`),
              chalk.gray((err as Error).message),
            );
            errors++;
          }
        }
        continue;
      }

      if (action.action !== "diverged") {
        // Unknown action type — skip defensively so we never misroute.
        console.log(
          chalk.yellow(`    ? ${action.file_path}`),
          chalk.gray(`unknown action: ${action.action}`),
        );
        skipped++;
        continue;
      }

      // Live-session divergence: both branches have real content. Use
      // resolveDivergence() to atomically snapshot the loser.
      // --push → keep_local, --pull → keep_cloud,
      // non-interactive default → keep_local, interactive → prompt user.
      let divergeResolution: "keep_local" | "keep_cloud" | "skip";
      if (options.push) {
        divergeResolution = "keep_local";
      } else if (options.pull) {
        divergeResolution = "keep_cloud";
      } else if (!process.stdin.isTTY || !process.stdout.isTTY) {
        divergeResolution = "keep_local";
      } else {
        const answer = await promptSessionConflict(action.file_path);
        divergeResolution =
          answer === "local"
            ? "keep_local"
            : answer === "cloud"
              ? "keep_cloud"
              : "skip";
      }

      if (divergeResolution === "skip") {
        skipped++;
        continue;
      }

      if (!action.session_id || !action.cloud_hash) {
        // Defensive: true diverged actions always include session_id + cloud_hash.
        console.log(
          chalk.red(`    ✗ ${action.file_path}`),
          chalk.gray("missing cloud reference for divergence resolution"),
        );
        errors++;
        continue;
      }

      // Upload local content — needed for both keep_local (to apply) and
      // keep_cloud (to snapshot the local branch).
      const local = localByKey.get(key);
      if (!local) {
        skipped++;
        continue;
      }
      try {
        const fileRef = await uploadLocalFile(local.loc.path, local.content);
        const result = await getClient().agentSessions.resolveDivergence({
          agent: action.agent,
          file_path: action.file_path,
          scope: action.scope,
          device_id: deviceID,
          local_file_ref: fileRef,
          local_content_hash: local.hash,
          expected_cloud_version: action.cloud_version ?? action.version ?? 0,
          expected_cloud_hash: action.cloud_hash,
          resolution: divergeResolution,
        });

        if (result.winner === "local") {
          ackSessions.push({
            agent: action.agent,
            file_path: action.file_path,
            scope: action.scope,
            content_hash: local.hash,
            version: result.new_version,
            local_path: local.loc.path,
          });
          console.log(
            chalk.green(`    ↑ ${action.file_path}`),
            chalk.gray("kept local, cloud branch archived"),
          );
          console.log(
            chalk.gray(
              `      Recover: memax agents sessions snapshots --session ${action.session_id}`,
            ),
          );
          pushed++;
        } else {
          const writePath = resolveWritePath(
            action.agent,
            action.file_path,
            action.scope,
          );
          if (writePath) {
            const bytes = await downloadAgentSession(action.session_id);
            mkdirSync(dirname(writePath), { recursive: true });
            const materialized = materializeAgentSessionContent(
              action.agent,
              bytes,
              {
                scope: action.scope,
                currentProjectRootPath,
                writePath,
              },
            );
            writeFileSync(writePath, materialized);
            ackSessions.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              content_hash: computeSessionSyncHash(
                action.agent,
                action.scope,
                materialized,
                currentProjectRootPath,
              ),
              version: result.new_version,
              local_path: writePath,
            });
          }
          console.log(
            chalk.cyan(`    ↓ ${action.file_path}`),
            chalk.gray("kept cloud, local branch archived"),
          );
          console.log(
            chalk.gray(
              `      Recover: memax agents sessions snapshots --session ${action.session_id}`,
            ),
          );
          pulled++;
        }
      } catch (err) {
        if (err instanceof SessionOversizeError) {
          console.log(
            chalk.yellow(`    - ${action.file_path}`),
            chalk.gray(
              `${err.message}; skipping (run \`memax agents sessions delete\` to drop it)`,
            ),
          );
          skipped++;
        } else {
          console.log(
            chalk.red(`    ✗ ${action.file_path}`),
            chalk.gray((err as Error).message),
          );
          errors++;
        }
      }
    }
  }

  if (ackSessions.length > 0) {
    try {
      await getClient().agentSessions.ack({
        device_id: deviceID,
        sessions: ackSessions,
      });
    } catch (err) {
      console.log(
        chalk.yellow("\n  Warning: failed to persist session sync state"),
        chalk.gray((err as Error).message),
      );
    }
  }

  const parts: string[] = [];
  if (pushed > 0) parts.push(`${pushed} pushed`);
  if (pulled > 0) parts.push(`${pulled} restored`);
  if (deletedLocal > 0) parts.push(`${deletedLocal} deleted locally`);
  if (reconciled > 0) parts.push(`${reconciled} reconciled`);
  if (unchanged > 0) parts.push(`${unchanged} unchanged`);
  if (skipped > 0) parts.push(`${skipped} skipped`);
  if (errors > 0) parts.push(`${errors} errors`);
  if (parts.length === 0) {
    console.log(chalk.gray("  No session artifacts discovered.\n"));
    return;
  }
  console.log(chalk.bold(`\n  Done: ${parts.join(", ")}`));
  console.log(
    chalk.gray(
      "  Session sync preserves raw artifacts. Knowledge extraction remains a separate workflow.\n",
    ),
  );
}

export async function listAgentSessionsCommand(): Promise<void> {
  try {
    const result = await getClient().agentSessions.list();
    const sessions = result.sessions;
    if (sessions.length === 0) {
      console.log(chalk.yellow("  No synced session artifacts.\n"));
      return;
    }
    console.log();
    for (const session of sessions) {
      const scopeTag =
        session.scope === "global"
          ? chalk.dim("global")
          : chalk.dim(session.scope.replace(/^project:/, ""));
      console.log(
        `  ${chalk.cyan(formatAgentName(session.agent))}  ${session.file_path}  ${scopeTag}  ${chalk.dim(formatBytes(session.size_bytes))}`,
      );
    }
    console.log();
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch session artifacts: ${(err as Error).message}\n`,
      ),
    );
  }
}

export async function listDeletedAgentSessionsCommand(): Promise<void> {
  try {
    const result = await getClient().agentSessions.listDeleted();
    const sessions = result.sessions;
    if (sessions.length === 0) {
      console.log(chalk.gray("  No recoverable deleted session artifacts.\n"));
      return;
    }
    console.log(chalk.bold("\n  Recoverable Deleted Session Artifacts\n"));
    for (const [index, session] of sessions.entries()) {
      const scopeTag =
        session.scope === "global"
          ? chalk.dim("global")
          : chalk.dim(session.scope.replace(/^project:/, ""));
      console.log(
        `  ${chalk.bold(String(index + 1).padStart(2, " "))}. ${chalk.cyan(formatAgentName(session.agent))} ${session.file_path} ${scopeTag}`,
      );
      console.log(
        chalk.gray(
          `      deleted ${formatAge(session.deleted_at)} · recoverable until ${session.content_expires_at ? new Date(session.content_expires_at).toLocaleString() : "expired"}`,
        ),
      );
    }
    console.log();
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch deleted session artifacts: ${(err as Error).message}\n`,
      ),
    );
  }
}

export async function restoreDeletedAgentSessionsCommand(): Promise<void> {
  let deleted;
  try {
    deleted = await getClient().agentSessions.listDeleted();
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch deleted session artifacts: ${(err as Error).message}\n`,
      ),
    );
    return;
  }
  if (deleted.sessions.length === 0) {
    console.log(chalk.gray("  No recoverable deleted session artifacts.\n"));
    return;
  }

  console.log(chalk.bold("\n  Recover Deleted Session Artifacts\n"));
  deleted.sessions.forEach((session, index) => {
    const scopeTag =
      session.scope === "global"
        ? chalk.dim("global")
        : chalk.dim(session.scope.replace(/^project:/, ""));
    console.log(
      `  ${chalk.dim(`${index + 1}.`)} ${chalk.cyan(formatAgentName(session.agent))} ${session.file_path} ${scopeTag}`,
    );
  });
  console.log();

  const answer = await ask(
    "  Select session artifacts to restore (comma-separated numbers, or 'q' to quit): ",
  );
  if (!answer || answer.trim().toLowerCase() === "q") {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  const indexes = answer
    .split(",")
    .map((part) => Number.parseInt(part.trim(), 10))
    .filter(
      (idx) =>
        Number.isInteger(idx) && idx >= 1 && idx <= deleted.sessions.length,
    );
  if (indexes.length === 0) {
    console.log(chalk.gray("  No valid selection.\n"));
    return;
  }

  const cwd = process.cwd();
  const deviceID = getOrCreateDeviceID();
  const currentProjectScope = getProjectScope(cwd);
  const currentProjectRootPath = resolveProjectRootPath(cwd) ?? cwd;

  let restored = 0;
  for (const index of indexes) {
    const sessionInfo = deleted.sessions[index - 1];
    try {
      const writePath = resolveAgentSessionWritePath(
        sessionInfo.agent,
        sessionInfo.file_path,
        sessionInfo.scope,
        {
          cwd,
          home: homedir(),
          currentProjectScope,
        },
      );
      const session = await getClient().agentSessions.restore({
        agent: sessionInfo.agent,
        file_path: sessionInfo.file_path,
        scope: sessionInfo.scope,
        device_id: deviceID,
        local_path: writePath ?? undefined,
      });

      if (writePath && !existsSync(writePath)) {
        const bytes = await downloadAgentSession(session.id);
        mkdirSync(dirname(writePath), { recursive: true });
        const materialized = materializeAgentSessionContent(
          session.agent,
          bytes,
          {
            scope: session.scope,
            currentProjectRootPath,
            writePath,
          },
        );
        writeFileSync(writePath, materialized);
        await getClient().agentSessions.ack({
          device_id: deviceID,
          sessions: [
            {
              agent: session.agent,
              file_path: session.file_path,
              scope: session.scope,
              content_hash: computeSessionSyncHash(
                session.agent,
                session.scope,
                materialized,
                currentProjectRootPath,
              ),
              version: session.version,
              local_path: writePath,
            },
          ],
        });
        console.log(
          chalk.green(`    ✓ ${session.file_path}`),
          chalk.gray("restored to cloud and local machine"),
        );
      } else if (writePath && existsSync(writePath)) {
        console.log(
          chalk.yellow(`    - ${session.file_path}`),
          chalk.gray("restored to cloud; local file already exists"),
        );
      } else {
        console.log(
          chalk.yellow(`    - ${session.file_path}`),
          chalk.gray("restored to cloud; no safe local path on this machine"),
        );
      }
      restored++;
    } catch (err) {
      console.log(
        chalk.red(`    ✗ ${sessionInfo.file_path}`),
        chalk.gray((err as Error).message),
      );
    }
  }

  console.log(
    chalk.gray(
      `\n  ${restored} session artifact${restored === 1 ? "" : "s"} restored.\n`,
    ),
  );
}

export async function deleteAgentSessionsCommand(): Promise<void> {
  let sessions: AgentSession[];
  try {
    sessions = (await getClient().agentSessions.list()).sessions;
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch session artifacts: ${(err as Error).message}\n`,
      ),
    );
    return;
  }
  if (sessions.length === 0) {
    console.log(chalk.yellow("  No synced session artifacts to delete.\n"));
    return;
  }

  sessions.forEach((session, index) => {
    const scopeTag =
      session.scope === "global"
        ? chalk.dim("global")
        : chalk.dim(session.scope.replace(/^project:/, ""));
    console.log(
      `  ${chalk.dim(`${index + 1}.`)} ${chalk.cyan(formatAgentName(session.agent))} ${session.file_path} ${scopeTag}`,
    );
  });
  console.log();

  const answer = await ask(
    "  Select session artifacts to delete (comma-separated numbers, or 'q' to quit): ",
  );
  if (!answer || answer.trim().toLowerCase() === "q") {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }
  const indices = answer
    .split(",")
    .map((value) => Number.parseInt(value.trim(), 10))
    .filter(
      (value) =>
        Number.isFinite(value) && value >= 1 && value <= sessions.length,
    );
  if (indices.length === 0) {
    console.log(chalk.gray("  No valid selections.\n"));
    return;
  }

  const mode = (
    await ask(
      "  Delete from [l] this device only, [e] everywhere, or [s] skip? ",
    )
  )
    .trim()
    .toLowerCase();
  if (mode !== "l" && mode !== "e") {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  const deviceID = getOrCreateDeviceID();
  const cwd = process.cwd();
  const currentProjectScope = getProjectScope(cwd);
  for (const index of indices) {
    const session = sessions[index - 1];
    const localPath = resolveAgentSessionWritePath(
      session.agent,
      session.file_path,
      session.scope,
      { cwd, home: homedir(), currentProjectScope },
    );
    try {
      if (mode === "l") {
        await getClient().agentSessions.localDelete({
          device_id: deviceID,
          agent: session.agent,
          file_path: session.file_path,
          scope: session.scope,
          local_path: localPath ?? undefined,
        });
        if (localPath && existsSync(localPath)) {
          moveFileToTrash(localPath, "agent-sessions");
        }
      } else {
        await getClient().agentSessions.delete(session.id);
        if (localPath && existsSync(localPath)) {
          moveFileToTrash(localPath, "agent-sessions");
        }
        await getClient().agentSessions.ack({
          device_id: deviceID,
          sessions: [
            {
              agent: session.agent,
              file_path: session.file_path,
              scope: session.scope,
              version: session.version + 1,
              local_path: localPath ?? undefined,
              deleted: true,
            },
          ],
        });
      }
      console.log(
        chalk.green(`    ✓ ${session.file_path}`),
        chalk.gray(
          mode === "e" ? "deleted everywhere" : "removed from this device",
        ),
      );
    } catch (err) {
      console.log(
        chalk.red(`    ✗ ${session.file_path}`),
        chalk.gray((err as Error).message),
      );
    }
  }
  console.log();
}

export async function cleanupAgentSessionsCommand(options?: {
  yes?: boolean;
}): Promise<void> {
  let sessions: AgentSession[];
  try {
    sessions = (await getClient().agentSessions.list()).sessions;
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch session artifacts: ${(err as Error).message}\n`,
      ),
    );
    return;
  }

  const pairs = findShadowedGlobalSessions(sessions);
  if (pairs.length === 0) {
    console.log(chalk.gray("  No legacy global session duplicates found.\n"));
    return;
  }

  const safePairs: ShadowedGlobalSessionPair[] = [];
  const divergedPairs: ShadowedGlobalSessionPair[] = [];
  for (const pair of pairs) {
    if (pair.global.content_hash === pair.project.content_hash) {
      safePairs.push(pair);
      continue;
    }
    try {
      const [globalBytes, projectBytes] = await Promise.all([
        downloadAgentSession(pair.global.id),
        downloadAgentSession(pair.project.id),
      ]);
      if (
        computeDownloadedPortableHash(pair.global.agent, globalBytes) ===
        computeDownloadedPortableHash(pair.project.agent, projectBytes)
      ) {
        safePairs.push(pair);
      } else {
        divergedPairs.push(pair);
      }
    } catch {
      divergedPairs.push(pair);
    }
  }

  console.log(chalk.bold("\n  Session Duplicate Cleanup\n"));

  if (safePairs.length > 0) {
    console.log(chalk.white("  Safe To Remove"));
    for (const pair of safePairs) {
      console.log(
        `    ${chalk.cyan(formatAgentName(pair.global.agent))}  ${pair.global.file_path}`,
      );
      console.log(
        `      ${chalk.gray("delete legacy global copy; project-scoped copy remains")}`,
      );
    }
    console.log();
  }

  if (divergedPairs.length > 0) {
    console.log(chalk.yellow("  Needs Manual Review"));
    for (const pair of divergedPairs) {
      console.log(
        `    ${chalk.yellow(formatAgentName(pair.global.agent))}  ${pair.global.file_path}`,
      );
      console.log(
        `      ${chalk.gray(`global hash ${pair.global.content_hash.slice(0, 8)}… differs from project ${pair.project.scope.replace(/^project:/, "")} hash ${pair.project.content_hash.slice(0, 8)}…`)}`,
      );
    }
    console.log();
  }

  if (safePairs.length === 0) {
    console.log(
      chalk.gray(
        "  No identical legacy global duplicates can be removed safely.\n",
      ),
    );
    return;
  }

  if (!options?.yes) {
    const proceed = await confirmDefault(
      `  Delete ${safePairs.length} safe global duplicate${safePairs.length === 1 ? "" : "s"} from cloud? [Y/n] `,
    );
    if (!proceed) {
      console.log(chalk.gray("  Cancelled.\n"));
      return;
    }
  }

  let deleted = 0;
  let errors = 0;
  for (const pair of safePairs) {
    try {
      await getClient().agentSessions.delete(pair.global.id);
      console.log(
        chalk.green(`    ✓ ${pair.global.file_path}`),
        chalk.gray("deleted legacy global copy"),
      );
      deleted++;
    } catch (err) {
      console.log(
        chalk.red(`    ✗ ${pair.global.file_path}`),
        chalk.gray((err as Error).message),
      );
      errors++;
    }
  }

  const summary: string[] = [];
  if (deleted > 0) summary.push(`${deleted} deleted`);
  if (divergedPairs.length > 0)
    summary.push(`${divergedPairs.length} need review`);
  if (errors > 0) summary.push(`${errors} errors`);
  console.log(chalk.bold(`\n  Done: ${summary.join(", ")}\n`));
}

export async function doctorAgentSessionsCommand(): Promise<void> {
  const cwd = process.cwd();
  const project = resolveProjectScope(cwd);
  const deviceID = getOrCreateDeviceID();
  const locations = discoverAgentSessions();
  const localByKey = new Map<string, AgentSessionLocation>();
  for (const loc of locations) {
    if (!existsSync(loc.path)) continue;
    localByKey.set(`${loc.agent}|${loc.filePath}|${loc.scope}`, loc);
  }

  console.log(chalk.bold("\n  Memax Agent Session Doctor\n"));
  console.log(`  Device  ${chalk.bold(deviceID)}`);
  console.log(`  CWD     ${chalk.gray(cwd)}`);
  console.log(`  Scope   ${chalk.bold(project.scope)}`);
  if (project.warning) {
    console.log(`  Warning ${chalk.yellow(project.warning)}`);
  }
  console.log();

  console.log(chalk.white("  Local Discovery"));
  if (localByKey.size === 0) {
    console.log(
      `    ${chalk.gray("No supported local session artifacts discovered.")}`,
    );
  } else {
    for (const loc of localByKey.values()) {
      console.log(
        `    ${chalk.cyan(formatAgentName(loc.agent))}  ${loc.filePath}  ${chalk.gray(loc.scope)}  ${chalk.gray(loc.path)}`,
      );
    }
  }
  console.log();

  try {
    const cloud = await getClient().agentSessions.list();
    const placements = cloud.sessions.map((session) => ({
      session,
      placement: classifyAgentSessionPlacement(
        session.agent,
        session.file_path,
        session.scope,
        {
          cwd,
          home: homedir(),
          currentProjectScope: project.scope,
          localByKey,
        },
      ),
    }));

    printSessionPlacementSection(
      "  Restorable Here",
      chalk.cyan,
      placements.filter((item) => item.placement.kind === "restorable"),
    );
    printSessionPlacementSection(
      "  Different Project",
      chalk.yellow,
      placements.filter((item) => item.placement.kind === "different_project"),
    );
    printSessionPlacementSection(
      "  Unresolved",
      chalk.magenta,
      placements.filter((item) => item.placement.kind === "unresolved"),
    );
    console.log(
      chalk.gray(
        "  Session sync restores only when placement is safe. Ambiguous session stores are skipped.\n",
      ),
    );
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch cloud session artifacts: ${(err as Error).message}\n`,
      ),
    );
  }
}

export function registerAgentSessionCommands(agentsCmd: Command): void {
  const agentSessionsCmd = agentsCmd
    .command("sessions")
    .description("Manage synced agent session artifacts");

  agentSessionsCmd
    .command("sync")
    .description(
      "Sync agent session artifacts bidirectionally with Memax cloud",
    )
    .option("--push", "Force push local session artifacts to cloud (overwrite)")
    .option("--pull", "Force pull cloud session artifacts to local (overwrite)")
    .action(syncAgentSessionsCommand);

  agentSessionsCmd
    .command("list")
    .description("List synced agent session artifacts in the cloud")
    .action(listAgentSessionsCommand);

  agentSessionsCmd
    .command("deleted")
    .description("List recoverable deleted session artifacts retained in cloud")
    .action(listDeletedAgentSessionsCommand);

  agentSessionsCmd
    .command("restore")
    .description("Restore deleted session artifacts retained in cloud")
    .action(restoreDeletedAgentSessionsCommand);

  agentSessionsCmd
    .command("delete")
    .description("Interactively select and delete synced session artifacts")
    .action(deleteAgentSessionsCommand);

  agentSessionsCmd
    .command("cleanup")
    .description("Remove safe legacy global session duplicates from cloud")
    .option("-y, --yes", "Skip confirmation")
    .action(cleanupAgentSessionsCommand);

  agentSessionsCmd
    .command("doctor")
    .description(
      "Explain session sync identity, discovery, and safe restore behavior on this machine",
    )
    .action(doctorAgentSessionsCommand);
}

function discoverAgentSessions(): AgentSessionLocation[] {
  const home = homedir();
  const config = loadConfig();
  const locations: AgentSessionLocation[] = [];
  const add = (
    agent: string,
    path: string,
    filePath: string,
    scope: Scope,
    sessionType: string,
    projectRoot?: string,
  ) => {
    locations.push({
      agent,
      path,
      filePath: normalizeFilePath(filePath),
      scope,
      sessionType,
      projectRoot,
    });
  };

  const claudeProjectsDir = join(home, ".claude", "projects");
  if (existsSync(claudeProjectsDir)) {
    for (const project of safeListDir(claudeProjectsDir)) {
      const projectRoot = resolveClaudeProjectPath(project);
      const repoUrl = resolveClaudeProjectFolder(project);
      if (!repoUrl || !projectRoot) continue;
      const projectDir = join(claudeProjectsDir, project);
      for (const file of safeListDir(projectDir)) {
        if (!file.endsWith(".jsonl")) continue;
        add(
          "claude-code",
          join(projectDir, file),
          `sessions/${file}`,
          `project:${repoUrl}`,
          "transcript",
          projectRoot,
        );
      }
    }
  }

  const codexHistory = join(home, ".codex", "history.jsonl");
  add("codex", codexHistory, "history.jsonl", "global", "history");
  const codexSessionsRoot = join(home, ".codex", "sessions");
  if (existsSync(codexSessionsRoot)) {
    for (const file of walkFiles(codexSessionsRoot, (entry) =>
      entry.endsWith(".jsonl"),
    )) {
      const ctx = computeSessionProjectContext("codex", file);
      add(
        "codex",
        file,
        join("sessions", relative(codexSessionsRoot, file)),
        ctx.scope,
        "transcript",
        ctx.projectRoot,
      );
    }
  }

  const geminiTmpRoot = join(home, ".gemini", "tmp");
  if (existsSync(geminiTmpRoot)) {
    for (const projectDirName of safeListDir(geminiTmpRoot)) {
      const projectDir = join(geminiTmpRoot, projectDirName);
      const projectRootPath = readProjectRootMarker(
        join(projectDir, ".project_root"),
      );
      if (!projectRootPath) continue;
      const resolvedProjectRoot =
        resolveProjectRootPath(projectRootPath) ?? projectRootPath;
      const scope = getProjectScope(resolvedProjectRoot);
      if (scope === "project" || !scope.startsWith("project:")) continue;
      const canonicalScope = scope as Scope;
      const chatsDir = join(projectDir, "chats");
      if (!existsSync(chatsDir)) continue;
      for (const file of safeListDir(chatsDir)) {
        if (!file.endsWith(".json")) continue;
        add(
          "gemini",
          join(chatsDir, file),
          `chats/${file}`,
          canonicalScope,
          "session",
          resolvedProjectRoot,
        );
      }
    }
  }

  for (const root of config.agent_session_roots ?? []) {
    const normalizedScope = (root.scope || "").trim();
    if (!isScopeValue(normalizedScope)) continue;
    const rootPath = root.root_path ? resolveHome(root.root_path) : "";
    if (!rootPath || !existsSync(rootPath)) continue;
    const includeExtensions =
      root.include_extensions && root.include_extensions.length > 0
        ? new Set(root.include_extensions.map((value) => value.toLowerCase()))
        : new Set([".jsonl", ".json", ".md", ".txt"]);
    const sessionType = root.session_type?.trim() || "artifact";
    for (const file of walkFiles(rootPath, (entry) =>
      includeExtensions.has(extension(entry)),
    )) {
      add(
        root.agent,
        file,
        relative(rootPath, file),
        normalizedScope,
        sessionType,
      );
    }
  }

  return locations;
}

function findGeminiProjectDir(home: string, scope: Scope): string | null {
  const geminiTmpRoot = join(home, ".gemini", "tmp");
  if (!existsSync(geminiTmpRoot)) return null;
  for (const projectDirName of safeListDir(geminiTmpRoot)) {
    const projectDir = join(geminiTmpRoot, projectDirName);
    const projectRootPath = readProjectRootMarker(
      join(projectDir, ".project_root"),
    );
    if (!projectRootPath) continue;
    if (getProjectScope(projectRootPath) === scope) {
      return projectDir;
    }
  }
  return null;
}

export function resolveAgentSessionWritePath(
  agent: string,
  filePath: string,
  scope: Scope,
  options: {
    cwd?: string;
    home?: string;
    currentProjectScope?: ProjectScope;
    currentProjectRootPath?: string;
  } = {},
): string | null {
  const cwd = options.cwd ?? process.cwd();
  const home = options.home ?? homedir();
  const currentProjectScope =
    options.currentProjectScope ?? getProjectScope(cwd);
  const currentProjectRootPath =
    options.currentProjectRootPath ?? resolveProjectRootPath(cwd) ?? cwd;
  const normalized = normalizeFilePath(filePath);

  if (scope === "global") {
    switch (agent) {
      case "codex":
        if (normalized === "history.jsonl") {
          return join(home, ".codex", "history.jsonl");
        }
        if (normalized.startsWith("sessions/")) {
          return join(home, ".codex", normalized);
        }
        return null;
      default:
        return null;
    }
  }

  if (!scope.startsWith("project:") || scope !== currentProjectScope) {
    return null;
  }

  switch (agent) {
    case "codex":
      if (normalized.startsWith("sessions/")) {
        return join(home, ".codex", normalized);
      }
      return null;
    case "claude-code": {
      if (!normalized.startsWith("sessions/")) return null;
      const claudeProjectsDir = join(home, ".claude", "projects");
      for (const project of safeListDir(claudeProjectsDir)) {
        const repoUrl = resolveClaudeProjectFolder(project);
        if (repoUrl && `project:${repoUrl}` === scope) {
          return join(
            claudeProjectsDir,
            project,
            normalized.replace(/^sessions\//, ""),
          );
        }
      }
      return join(
        claudeProjectsDir,
        encodeClaudeProjectDir(currentProjectRootPath),
        normalized.replace(/^sessions\//, ""),
      );
    }
    case "gemini": {
      if (!normalized.startsWith("chats/")) return null;
      const projectDir = ensureGeminiProjectDir(
        home,
        currentProjectRootPath,
        scope,
      );
      return join(projectDir, normalized);
    }
    default:
      return null;
  }
}

export function classifyAgentSessionPlacement(
  agent: string,
  filePath: string,
  scope: Scope,
  options: {
    cwd?: string;
    home?: string;
    currentProjectScope?: ProjectScope;
    localByKey?: Map<string, AgentSessionLocation>;
  } = {},
): AgentSessionPlacement {
  const key = `${agent}|${normalizeFilePath(filePath)}|${scope}`;
  const existing = options.localByKey?.get(key);
  if (existing) {
    return { kind: "present", path: existing.path, reason: "present locally" };
  }
  const cwd = options.cwd ?? process.cwd();
  const currentProjectScope =
    options.currentProjectScope ?? getProjectScope(cwd);
  if (scope.startsWith("project:") && scope !== currentProjectScope) {
    return {
      kind: "different_project",
      reason: `belongs to ${scope.replace(/^project:/, "")}`,
    };
  }
  const path = resolveAgentSessionWritePath(agent, filePath, scope, options);
  if (path) {
    return { kind: "restorable", path, reason: "safe restore path available" };
  }
  return {
    kind: "unresolved",
    reason: "no safe restore path on this machine",
  };
}

// Must match the public API upload limit. Duplicated here so the CLI can
// fail-fast with a clear message instead of paying a round-trip to discover
// the server's 413.
export const AGENT_SESSION_MAX_BYTES = 200 * 1024 * 1024;

/**
 * Thrown by uploadLocalFile when a session artifact exceeds the flat
 * agent-session cap. Callers catch this specifically and treat it as a
 * soft-skip (warn and continue) rather than a hard error, so one oversize
 * transcript doesn't block sync of every other file.
 */
export class SessionOversizeError extends Error {
  readonly sizeBytes: number;
  readonly maxBytes: number;
  constructor(path: string, sizeBytes: number) {
    super(
      `${path} is ${Math.round(sizeBytes / (1024 * 1024))} MB, exceeds the ${Math.round(
        AGENT_SESSION_MAX_BYTES / (1024 * 1024),
      )} MB agent-session upload cap`,
    );
    this.name = "SessionOversizeError";
    this.sizeBytes = sizeBytes;
    this.maxBytes = AGENT_SESSION_MAX_BYTES;
  }
}

async function uploadLocalFile(path: string, content: Buffer) {
  const stat = statSync(path);
  if (stat.size > AGENT_SESSION_MAX_BYTES) {
    throw new SessionOversizeError(path, stat.size);
  }
  const contentType = inferContentType(path);
  const sha256 = createHash("sha256").update(content).digest("hex");
  const intent = await getClient().uploads.create({
    filename: basenameSafe(path),
    content_type: contentType,
    size_bytes: stat.size,
    purpose: "agent_session",
  });
  await putUpload(intent, content);
  return {
    object_key: intent.object_key,
    filename: basenameSafe(path),
    content_type: contentType,
    size_bytes: stat.size,
    sha256,
  };
}

async function putUpload(intent: UploadIntent, content: Buffer): Promise<void> {
  const res = await fetch(intent.upload_url, {
    method: "PUT",
    headers: intent.headers,
    body: content,
  });
  if (!res.ok) {
    throw new Error(`upload failed with status ${res.status}`);
  }
}

async function downloadAgentSession(id: string): Promise<Buffer> {
  const res = await getClient().agentSessions.downloadBlob(id);
  if (!res.ok) {
    throw new Error(`download failed with status ${res.status}`);
  }
  return Buffer.from(await res.arrayBuffer());
}

async function promptSessionConflict(
  filePath: string,
): Promise<"local" | "cloud" | "skip"> {
  const answer = await ask(
    `    Conflict for ${filePath}. Use [l]ocal, [c]loud, or [s]kip? `,
  );
  const normalized = answer.trim().toLowerCase();
  if (normalized === "l") return "local";
  if (normalized === "c") return "cloud";
  return "skip";
}

async function promptSessionCloudDeletion(
  filePath: string,
): Promise<"delete" | "local" | "skip"> {
  const answer = await ask(
    `    ${filePath} was deleted in cloud. [d]elete local, [k]eep local and restore cloud, or [s]kip? `,
  );
  const normalized = answer.trim().toLowerCase();
  if (normalized === "d") return "delete";
  if (normalized === "k") return "local";
  return "skip";
}

function readProjectRootMarker(path: string): string | null {
  if (!existsSync(path)) return null;
  try {
    const value = readFileSync(path, "utf-8").trim();
    return value || null;
  } catch {
    return null;
  }
}

function walkFiles(root: string, include: (path: string) => boolean): string[] {
  const files: string[] = [];
  const visit = (dir: string) => {
    for (const entry of safeListDir(dir)) {
      const fullPath = join(dir, entry);
      let stat;
      try {
        stat = statSync(fullPath);
      } catch {
        continue;
      }
      if (stat.isDirectory()) {
        visit(fullPath);
        continue;
      }
      if (stat.isFile() && include(fullPath)) {
        files.push(fullPath);
      }
    }
  };
  visit(root);
  return files;
}

function safeListDir(dir: string): string[] {
  try {
    return readdirSync(dir);
  } catch {
    return [];
  }
}

function printSessionPlacementSection(
  title: string,
  color: (text: string) => string,
  items: {
    session: AgentSession;
    placement: AgentSessionPlacement;
  }[],
): void {
  if (items.length === 0) return;
  console.log(chalk.white(title));
  for (const item of items) {
    console.log(
      `    ${color(formatAgentName(item.session.agent))}  ${item.session.file_path}`,
    );
    console.log(`      ${chalk.gray(item.placement.reason)}`);
  }
  console.log();
}

function formatAgentName(agent: string): string {
  const labels: Record<string, string> = {
    "claude-code": "Claude Code",
    codex: "Codex",
    gemini: "Gemini CLI",
    openclaw: "OpenClaw",
    opencode: "OpenCode",
  };
  return labels[agent] ?? agent;
}

function inferContentType(path: string): string {
  if (path.endsWith(".jsonl")) return "application/x-ndjson";
  if (path.endsWith(".json")) return "application/json";
  if (path.endsWith(".md")) return "text/markdown";
  return "text/plain";
}

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function formatAge(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(ms / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function basenameSafe(path: string): string {
  return normalizeFilePath(path).split("/").pop() ?? "artifact";
}

function resolveHome(path: string): string {
  if (path.startsWith("~/")) {
    return join(homedir(), path.slice(2));
  }
  return path;
}

function extension(path: string): string {
  const normalized = normalizeFilePath(path).toLowerCase();
  const dot = normalized.lastIndexOf(".");
  return dot >= 0 ? normalized.slice(dot) : "";
}
