import { Command } from "commander";
import chalk from "chalk";
import { createHash } from "node:crypto";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  readdirSync,
  statSync,
  existsSync,
} from "node:fs";
import { join, relative, resolve, dirname } from "node:path";
import { homedir } from "node:os";
import { getClient } from "../lib/client.js";
import {
  getProjectScope,
  resolveProjectScope,
  resolveClaudeProjectFolder,
  normalizeFilePath,
  readMemaxYmlConfig,
  isCanonicalProjectScope,
  type ProjectScope,
} from "../lib/project-context.js";
import { getOrCreateDeviceID } from "../lib/config.js";
import { confirm, ask, confirmDefault } from "../lib/prompt.js";
import { moveFileToTrash } from "../lib/trash.js";
import type { AgentConfig, Scope, SyncPlanAction } from "memax-sdk";

export async function syncAgentMemoryCommand(
  options: SyncAgentOptions = {},
): Promise<void> {
  await syncAgentMemory(options);
}

export async function listAgentConfigsCommand(): Promise<void> {
  let configs;
  try {
    const result = await getClient().configs.list();
    configs = result.configs;
  } catch (err) {
    console.error(
      chalk.red(`  Failed to fetch configs: ${(err as Error).message}\n`),
    );
    return;
  }

  if (!configs || configs.length === 0) {
    console.log(
      chalk.yellow("  No synced configs. Run: memax agents configs sync\n"),
    );
    return;
  }

  // Group by agent
  const byAgent = new Map<
    string,
    { id: string; filePath: string; scope: Scope; updatedAt: string }[]
  >();
  for (const c of configs) {
    const list = byAgent.get(c.agent) ?? [];
    list.push({
      id: c.id,
      filePath: c.file_path,
      scope: c.scope,
      updatedAt: c.updated_at,
    });
    byAgent.set(c.agent, list);
  }

  console.log();
  for (const [agent, files] of byAgent) {
    console.log(`  ${chalk.cyan(agent)}`);
    for (const f of files) {
      const scopeTag =
        f.scope === "global"
          ? chalk.dim("global")
          : chalk.dim(f.scope.replace("project:", ""));
      const age = formatAge(f.updatedAt);
      console.log(
        `    ${f.filePath}  ${scopeTag}  ${chalk.dim(age)}  ${chalk.dim(f.id.slice(0, 8))}`,
      );
    }
    console.log();
  }

  console.log(
    chalk.gray(
      `  ${configs.length} config${configs.length > 1 ? "s" : ""} synced to cloud.\n`,
    ),
  );
}

export async function listDeletedAgentConfigsCommand(): Promise<void> {
  let deleted;
  try {
    deleted = await getClient().configs.listDeleted();
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch deleted configs: ${(err as Error).message}\n`,
      ),
    );
    return;
  }

  if (!deleted.configs || deleted.configs.length === 0) {
    console.log(chalk.gray("  No recoverable deleted configs.\n"));
    return;
  }

  console.log(chalk.bold("\n  Recoverable Deleted Configs\n"));
  deleted.configs.forEach((item, index) => {
    const scopeLabel =
      item.scope === "global"
        ? chalk.dim("global")
        : chalk.dim(item.scope.replace("project:", ""));
    console.log(
      `  ${chalk.bold(String(index + 1).padStart(2, " "))}. ${chalk.cyan(item.agent)} ${item.file_path} ${scopeLabel}`,
    );
    console.log(
      chalk.gray(
        `      deleted ${formatAge(item.deleted_at)} · recoverable until ${item.content_expires_at ? new Date(item.content_expires_at).toLocaleString() : "expired"}`,
      ),
    );
  });
  console.log();
}

export async function restoreDeletedAgentConfigsCommand(): Promise<void> {
  let deleted;
  try {
    deleted = await getClient().configs.listDeleted();
  } catch (err) {
    console.error(
      chalk.red(
        `  Failed to fetch deleted configs: ${(err as Error).message}\n`,
      ),
    );
    return;
  }

  if (!deleted.configs || deleted.configs.length === 0) {
    console.log(chalk.gray("  No recoverable deleted configs.\n"));
    return;
  }

  console.log(chalk.bold("\n  Recover Deleted Configs\n"));
  deleted.configs.forEach((item, index) => {
    const scopeLabel =
      item.scope === "global" ? "global" : item.scope.replace("project:", "");
    console.log(
      `    ${index + 1}. ${item.agent}/${item.file_path} ${chalk.gray(scopeLabel)}`,
    );
  });

  const raw = await ask(
    chalk.gray("\n  Enter indexes to restore (comma-separated): "),
  );
  const indexes = raw
    .split(",")
    .map((value) => Number.parseInt(value.trim(), 10))
    .filter(
      (value) =>
        Number.isInteger(value) && value > 0 && value <= deleted.configs.length,
    );
  if (indexes.length === 0) {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  const cwd = process.cwd();
  const deviceID = getOrCreateDeviceID();
  const currentProjectScope = getProjectScope(cwd);
  let restored = 0;
  for (const index of indexes) {
    const item = deleted.configs[index - 1];
    try {
      const writePath = resolveAgentConfigWritePath(
        item.agent,
        item.file_path,
        item.scope,
        {
          cwd,
          home: homedir(),
          currentProjectScope,
          findClaudeProjectDir,
        },
      );
      const config = await getClient().configs.restore({
        agent: item.agent,
        file_path: item.file_path,
        scope: item.scope,
        device_id: deviceID,
        local_path: writePath ?? undefined,
      });
      if (writePath && !existsSync(writePath)) {
        mkdirSync(dirname(writePath), { recursive: true });
        writeFileSync(writePath, config.content);
        await getClient().configs.ack({
          device_id: deviceID,
          configs: [
            {
              agent: item.agent,
              file_path: item.file_path,
              scope: item.scope,
              content_hash: config.content_hash,
              version: config.version,
              local_path: writePath,
            },
          ],
        });
        console.log(
          chalk.green(`    ✓ ${item.file_path}`),
          chalk.gray("restored to cloud and local machine"),
        );
      } else if (writePath && existsSync(writePath)) {
        console.log(
          chalk.yellow(`    - ${item.file_path}`),
          chalk.gray("restored to cloud; local file already exists"),
        );
      } else {
        console.log(
          chalk.yellow(`    - ${item.file_path}`),
          chalk.gray("restored to cloud; no safe local path on this machine"),
        );
      }
      restored++;
    } catch (err) {
      console.log(
        chalk.red(`    ✗ ${item.file_path}`),
        chalk.gray((err as Error).message),
      );
    }
  }

  console.log(
    chalk.gray(
      `\n  ${restored} config${restored === 1 ? "" : "s"} restored.\n`,
    ),
  );
}

interface AgentConfigPlacement {
  kind: "present" | "restorable" | "different_project" | "unresolved";
  path?: string;
  reason: string;
}

interface ClassifyAgentConfigPlacementOptions extends ResolveAgentConfigWritePathOptions {
  localByKey?: Map<string, AgentConfigLocation>;
}

export function classifyAgentConfigPlacement(
  agent: string,
  filePath: string,
  scope: Scope,
  options: ClassifyAgentConfigPlacementOptions = {},
): AgentConfigPlacement {
  const key = `${agent}|${normalizeFilePath(filePath)}|${scope}`;
  const existing = options.localByKey?.get(key);
  if (existing) {
    return {
      kind: "present",
      path: existing.path,
      reason: "present locally",
    };
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

  const path = resolveAgentConfigWritePath(agent, filePath, scope, options);
  if (path) {
    return {
      kind: "restorable",
      path,
      reason: "safe restore path available",
    };
  }

  return {
    kind: "unresolved",
    reason: "no safe restore path on this machine",
  };
}

export async function doctorAgentConfigsCommand(): Promise<void> {
  const cwd = process.cwd();
  const home = homedir();
  const deviceID = getOrCreateDeviceID();
  const project = resolveProjectScope(cwd);
  const memaxYml = readMemaxYmlConfig(cwd);
  const locations = discoverAgentConfigs();
  const localConfigs = locations.filter((loc) => {
    if (!existsSync(loc.path)) return false;
    try {
      const stat = statSync(loc.path);
      return stat.isFile() && stat.size > 0;
    } catch {
      return false;
    }
  });
  const localByKey = new Map<string, AgentConfigLocation>();
  for (const loc of localConfigs) {
    localByKey.set(`${loc.agent}|${loc.filePath}|${loc.scope}`, loc);
  }

  let cloudConfigs: { configs: AgentConfig[] } | null = null;
  try {
    cloudConfigs = await getClient().configs.list();
  } catch (err) {
    console.error(
      chalk.red(`  Failed to fetch cloud configs: ${(err as Error).message}\n`),
    );
  }

  const scopeSource =
    project.source === "memax_yml"
      ? ".memax.yml project_id"
      : project.source === "git_remote"
        ? "git origin"
        : "no canonical project identity";

  console.log(chalk.bold("\n  Memax Agent Config Doctor\n"));

  console.log(chalk.white("  Device"));
  console.log(`    ID         ${chalk.bold(deviceID)}`);
  console.log(`    Home       ${chalk.gray(home)}`);
  console.log();

  console.log(chalk.white("  Project"));
  console.log(`    CWD        ${chalk.gray(cwd)}`);
  console.log(`    Scope      ${chalk.bold(project.scope)}`);
  console.log(`    Source     ${chalk.gray(scopeSource)}`);
  if (memaxYml?.hub) {
    console.log(`    Hub        ${chalk.gray(memaxYml.hub)}`);
  }
  if (memaxYml?.project_id) {
    console.log(`    project_id ${chalk.gray(memaxYml.project_id)}`);
  }
  if (project.warning) {
    console.log(`    Warning    ${chalk.yellow(project.warning)}`);
  }
  if (project.scope === "project") {
    console.log(
      `    Note       ${chalk.yellow("project-scoped cross-device restore is disabled until git origin or .memax.yml project_id is available")}`,
    );
  }
  console.log();

  const byAgent = new Map<string, AgentConfigLocation[]>();
  for (const loc of localConfigs) {
    const group = byAgent.get(loc.agent) ?? [];
    group.push(loc);
    byAgent.set(loc.agent, group);
  }

  console.log(chalk.white("  Local Discovery"));
  if (localConfigs.length === 0) {
    console.log(`    ${chalk.gray("No local agent configs discovered.")}`);
  } else {
    for (const [agent, group] of byAgent) {
      console.log(`    ${chalk.cyan(formatAgentName(agent))}`);
      for (const loc of group) {
        const scopeLabel =
          loc.scope === "global"
            ? "global"
            : loc.scope.replace(/^project:/, "");
        console.log(
          `      • ${loc.filePath}  ${chalk.gray(scopeLabel)}  ${chalk.gray(loc.path)}`,
        );
      }
    }
  }
  console.log();

  if (!cloudConfigs) {
    console.log(
      chalk.gray(
        "  Cloud inspection skipped because cloud config fetch failed.\n",
      ),
    );
    return;
  }

  const placements = cloudConfigs.configs.map((config) => ({
    config,
    placement: classifyAgentConfigPlacement(
      config.agent,
      config.file_path,
      config.scope,
      {
        cwd,
        home,
        currentProjectScope: project.scope,
        localByKey,
        findClaudeProjectDir,
      },
    ),
  }));

  const present = placements.filter((p) => p.placement.kind === "present");
  const restorable = placements.filter(
    (p) => p.placement.kind === "restorable",
  );
  const differentProject = placements.filter(
    (p) => p.placement.kind === "different_project",
  );
  const unresolved = placements.filter(
    (p) => p.placement.kind === "unresolved",
  );

  console.log(chalk.white("  Cloud Coverage"));
  console.log(`    Present locally     ${chalk.bold(String(present.length))}`);
  console.log(
    `    Restorable here     ${chalk.bold(String(restorable.length))}`,
  );
  console.log(
    `    Other project       ${chalk.bold(String(differentProject.length))}`,
  );
  console.log(
    `    Unresolved here     ${chalk.bold(String(unresolved.length))}`,
  );
  console.log();

  printPlacementSection("  Restorable Here", chalk.cyan, restorable, (item) => [
    item.config.agent,
    item.config.file_path,
    item.placement.path ?? "",
  ]);
  printPlacementSection(
    "  Different Project",
    chalk.yellow,
    differentProject,
    (item) => [item.config.agent, item.config.file_path, item.placement.reason],
  );
  printPlacementSection("  Unresolved", chalk.magenta, unresolved, (item) => [
    item.config.agent,
    item.config.file_path,
    item.placement.reason,
  ]);

  console.log(
    chalk.gray(
      "  Use this command to verify what sync can restore safely on this machine.\n",
    ),
  );
}

export function registerAgentConfigCommands(agentsCmd: Command): void {
  const agentConfigsCmd = agentsCmd
    .command("configs")
    .description("Manage synced agent config files");

  agentConfigsCmd
    .command("sync")
    .description("Sync agent config files bidirectionally with Memax cloud")
    .option("--push", "Force push local configs to cloud (overwrite)")
    .option("--pull", "Force pull cloud configs to local (overwrite)")
    .action(syncAgentMemoryCommand);

  agentConfigsCmd
    .command("list")
    .description("List all synced agent configs in the cloud")
    .action(listAgentConfigsCommand);

  agentConfigsCmd
    .command("deleted")
    .description("List recoverable deleted configs retained in cloud")
    .action(listDeletedAgentConfigsCommand);

  agentConfigsCmd
    .command("restore")
    .description("Restore deleted configs retained in cloud")
    .action(restoreDeletedAgentConfigsCommand);

  agentConfigsCmd
    .command("delete")
    .description("Interactively select and delete synced configs")
    .action(deleteAgentConfigsCommand);

  agentConfigsCmd
    .command("doctor")
    .description(
      "Explain config sync identity, discovery, and safe restore behavior on this machine",
    )
    .action(doctorAgentConfigsCommand);
}

export async function deleteAgentConfigsCommand(): Promise<void> {
  let configs;
  try {
    const result = await getClient().configs.list();
    configs = result.configs;
  } catch (err) {
    console.error(
      chalk.red(`  Failed to fetch configs: ${(err as Error).message}\n`),
    );
    return;
  }

  if (!configs || configs.length === 0) {
    console.log(chalk.yellow("  No synced configs to delete.\n"));
    return;
  }

  // Display numbered list grouped by agent
  const items: {
    id: string;
    agent: string;
    filePath: string;
    scope: Scope;
    version: number;
  }[] = [];
  let currentAgent = "";
  for (const c of configs) {
    if (c.agent !== currentAgent) {
      if (currentAgent) console.log();
      console.log(`  ${chalk.cyan(c.agent)}`);
      currentAgent = c.agent;
    }
    items.push({
      id: c.id,
      agent: c.agent,
      filePath: c.file_path,
      scope: c.scope,
      version: c.version,
    });
    const idx = chalk.dim(`${items.length}.`);
    const scopeTag =
      c.scope === "global"
        ? chalk.dim("global")
        : chalk.dim(c.scope.replace("project:", ""));
    console.log(`    ${idx} ${c.file_path}  ${scopeTag}`);
  }
  console.log();

  const answer = await ask(
    "  Select configs to delete (comma-separated numbers, or 'q' to quit): ",
  );
  if (!answer || answer.trim().toLowerCase() === "q") {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  const indices = answer
    .split(",")
    .map((s) => parseInt(s.trim(), 10))
    .filter((n) => !isNaN(n) && n >= 1 && n <= items.length);

  if (indices.length === 0) {
    console.log(chalk.gray("  No valid selections.\n"));
    return;
  }

  const modeAnswer = await ask(
    "  Delete from [l] this device only, [e] everywhere, or [s] skip? ",
  );
  const mode = modeAnswer.trim().toLowerCase();
  if (mode !== "l" && mode !== "e") {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  console.log();
  for (const i of indices) {
    const item = items[i - 1];
    console.log(chalk.yellow(`    ${item.agent}/${item.filePath}`));
  }
  const ok = await confirm(
    mode === "e"
      ? `\n  Delete ${indices.length} config${indices.length > 1 ? "s" : ""} everywhere? (y/N) `
      : `\n  Remove ${indices.length} config${indices.length > 1 ? "s" : ""} from this device only? (y/N) `,
  );
  if (!ok) {
    console.log(chalk.gray("  Cancelled.\n"));
    return;
  }

  const deviceID = getOrCreateDeviceID();
  const cwd = process.cwd();
  const currentProjectScope = getProjectScope(cwd);
  const locations = discoverAgentConfigs();
  const locByKey = new Map<string, AgentConfigLocation>();
  for (const loc of locations) {
    locByKey.set(`${loc.agent}|${loc.filePath}|${loc.scope}`, loc);
  }
  const resolveDeletePath = (agent: string, filePath: string, scope: Scope) => {
    const loc = locByKey.get(`${agent}|${filePath}|${scope}`);
    if (loc) return loc.path;
    return resolveAgentConfigWritePath(agent, filePath, scope, {
      cwd,
      home: homedir(),
      currentProjectScope,
    });
  };

  let deleted = 0;
  let failed = 0;

  if (mode === "l") {
    // Local-only path: per-item call is still correct because localDelete
    // is a single-device sync-state write, not a cloud delete. No batch
    // endpoint needed.
    for (const i of indices) {
      const item = items[i - 1];
      try {
        const localPath = resolveDeletePath(
          item.agent,
          item.filePath,
          item.scope,
        );
        await getClient().configs.localDelete({
          device_id: deviceID,
          agent: item.agent,
          file_path: item.filePath,
          scope: item.scope,
          local_path: localPath ?? undefined,
        });
        if (localPath && existsSync(localPath)) {
          moveFileToTrash(localPath, "agent-configs");
        }
        console.log(
          chalk.green(`    \u2713 ${item.agent}/${item.filePath}`),
          chalk.gray("removed from this device (moved to Memax trash)"),
        );
        deleted++;
      } catch (err) {
        console.log(
          chalk.red(`    \u2717 ${item.agent}/${item.filePath}`),
          chalk.gray((err as Error).message),
        );
        failed++;
      }
    }
  } else {
    // Cloud-delete path: single batchDelete call commits what it can,
    // then we run per-item local cleanup (trash + ack + print) only for
    // ids the server actually removed.
    //
    // Classification rule (mirrors the algorithm in the B3 plan):
    //   not_found   → server row already gone. Counts as committed for
    //                 local cleanup: the user's target state is reached,
    //                 so trash the local file and ack so the device
    //                 manifest reflects the deletion.
    //   delete_failed → server row still present. Skip local cleanup
    //                 (retry on next sync), surface a red line.
    //   success       → standard committed path.
    const requestedIDs = indices.map((i) => items[i - 1].id);
    const itemByID = new Map(items.map((item) => [item.id, item]));

    let result;
    try {
      result = await getClient().configs.batchDelete(requestedIDs);
    } catch (err) {
      console.error(
        chalk.red(`\n  Batch delete failed: ${(err as Error).message}\n`),
      );
      return;
    }

    // Index skipped ids by reason so the loop can branch per-item.
    const skipReason = new Map<string, string>();
    for (const s of result.skipped) {
      skipReason.set(s.id, s.reason);
    }

    for (const id of requestedIDs) {
      const item = itemByID.get(id);
      if (!item) continue;
      const reason = skipReason.get(id);

      if (reason === "delete_failed") {
        console.log(
          chalk.red(`    \u2717 ${item.agent}/${item.filePath}`),
          chalk.gray("server delete failed (will retry on next sync)"),
        );
        failed++;
        continue;
      }

      // Committed path: real success OR not_found (idempotent — row
      // already gone on the server). Both paths trash the local file
      // and ack so the device manifest records the deletion.
      const localPath = resolveDeletePath(
        item.agent,
        item.filePath,
        item.scope,
      );
      try {
        if (localPath && existsSync(localPath)) {
          moveFileToTrash(localPath, "agent-configs");
        }
        await getClient().configs.ack({
          device_id: deviceID,
          configs: [
            {
              agent: item.agent,
              file_path: item.filePath,
              scope: item.scope,
              version: item.version + 1,
              local_path: localPath ?? undefined,
              deleted: true,
            },
          ],
        });
        deleted++;
        console.log(
          chalk.green(`    \u2713 ${item.agent}/${item.filePath}`),
          chalk.gray(
            reason === "not_found"
              ? "already deleted on server (local copy moved to Memax trash)"
              : "deleted everywhere (local copy moved to Memax trash)",
          ),
        );
      } catch (err) {
        // Local cleanup or ack failed — the server row is already
        // gone, so count as deleted for the summary but warn the
        // user their local state may be inconsistent.
        deleted++;
        console.log(
          chalk.yellow(`    ! ${item.agent}/${item.filePath}`),
          chalk.gray(
            `server deleted but local cleanup failed: ${(err as Error).message}`,
          ),
        );
      }
    }
  }

  const summary =
    mode === "e"
      ? `\n  ${deleted} config${deleted === 1 ? "" : "s"} deleted everywhere${failed > 0 ? `, ${failed} failed` : ""}.\n`
      : `\n  ${deleted} config${deleted === 1 ? "" : "s"} removed from this device${failed > 0 ? `, ${failed} failed` : ""}.\n`;
  console.log(chalk.gray(summary));
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

// --- Agent config sync ---

interface AgentConfigLocation {
  agent: string; // "claude-code", "cursor", "gemini", etc.
  label: string; // display label (e.g. "~/.claude/CLAUDE.md")
  path: string; // absolute path on disk
  filePath: string; // relative path for storage (e.g. "CLAUDE.md", "projects/memax/memory/feedback.md")
  scope: Scope; // "global" or "project:<repo-url>"
}

interface ResolveAgentConfigWritePathOptions {
  cwd?: string;
  home?: string;
  currentProjectScope?: ProjectScope;
  findClaudeProjectDir?: (scope: Scope) => string | null;
}

function findClaudeProjectDir(scope: Scope): string | null {
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

function printPlacementSection(
  title: string,
  color: (text: string) => string,
  items: {
    config: {
      agent: string;
      file_path: string;
      scope: Scope;
    };
    placement: AgentConfigPlacement;
  }[],
  format: (item: {
    config: {
      agent: string;
      file_path: string;
      scope: Scope;
    };
    placement: AgentConfigPlacement;
  }) => [string, string, string],
): void {
  if (items.length === 0) return;
  console.log(chalk.white(title));
  for (const item of items) {
    const [agent, filePath, detail] = format(item);
    console.log(`    ${color(formatAgentName(agent))}  ${filePath}`);
    console.log(`      ${chalk.gray(detail)}`);
  }
  console.log();
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

function discoverAgentConfigs(): AgentConfigLocation[] {
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
  ) =>
    locations.push({
      agent,
      label,
      path,
      filePath: normalizeFilePath(filePath),
      scope,
    });

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

  // OpenClaw
  const openclawMemoryDir = join(home, ".openclaw", "memory");
  if (existsSync(openclawMemoryDir)) {
    try {
      for (const file of readdirSync(openclawMemoryDir)) {
        if (file.endsWith(".md") || file.endsWith(".json")) {
          add(
            "openclaw",
            `~/.openclaw/memory/${file}`,
            join(openclawMemoryDir, file),
            `memory/${file}`,
          );
        }
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

interface SyncAgentOptions {
  push?: boolean;
  pull?: boolean;
  /** Skip conflicts silently (used by setup — conflicts can be resolved later via `memax agents sync`). */
  skipConflicts?: boolean;
}

async function syncAgentMemory(options: SyncAgentOptions = {}): Promise<void> {
  console.log(chalk.bold("\n  Memax Config Sync\n"));
  const projectScopeResolution = resolveProjectScope();
  if (projectScopeResolution.warning) {
    console.log(chalk.yellow(`  Warning: ${projectScopeResolution.warning}`));
    console.log(
      chalk.gray(
        "  Using .memax.yml project_id as the canonical project identity.\n",
      ),
    );
  }

  // Discover local config files
  const locations = discoverAgentConfigs();
  const localConfigs: {
    loc: AgentConfigLocation;
    content: string;
    hash: string;
    updatedAt: string;
  }[] = [];

  for (const loc of locations) {
    if (!existsSync(loc.path)) continue;
    try {
      const stat = statSync(loc.path);
      if (!stat.isFile() || stat.size === 0) continue;
      const content = readFileSync(loc.path, "utf-8");
      if (!content.trim()) continue;
      const hash = createHash("sha256").update(content).digest("hex");
      localConfigs.push({
        loc,
        content,
        hash,
        updatedAt: stat.mtime.toISOString(),
      });
    } catch {
      // Skip unreadable files
    }
  }

  const isBootstrap = localConfigs.length === 0;
  const deviceID = getOrCreateDeviceID();
  if (isBootstrap) {
    console.log(
      chalk.gray(
        "  No local agent configs found. Checking cloud for backups...\n",
      ),
    );
  } else {
    console.log(
      chalk.gray(
        `  Found ${localConfigs.length} local config${localConfigs.length > 1 ? "s" : ""}. Syncing with cloud...\n`,
      ),
    );
  }

  // Build manifest — may be empty on a new device (that's fine, we'll pull from cloud)
  const manifest = localConfigs.map((c) => ({
    agent: c.loc.agent,
    file_path: c.loc.filePath,
    scope: c.loc.scope,
    content_hash: c.hash,
    updated_at: c.updatedAt,
    local_path: c.loc.path,
  }));

  let actions: SyncPlanAction[];
  try {
    const plan = await getClient().configs.sync({
      device_id: deviceID,
      configs: manifest,
    });
    actions = plan.actions;
  } catch (err) {
    console.error(chalk.red(`  Sync failed: ${(err as Error).message}\n`));
    return;
  }

  // Force modes: resolve ALL ambiguous actions in one direction.
  // This includes both "conflict" AND "delete_local" (tombstone-driven
  // deletions where the server decided local should be removed).
  // One-sided actions (local_only push, cloud_only pull) stay as-is.
  if (options.push) {
    actions = actions.map((a) => {
      if (a.action === "conflict") return { ...a, action: "push" as const };
      if (a.action === "delete_local") return { ...a, action: "push" as const };
      return a;
    });
  } else if (options.pull) {
    actions = actions.map((a) => {
      if (a.action !== "conflict") return a;
      if (a.config_id) return { ...a, action: "pull" as const };
      return { ...a, action: "delete_local" as const };
    });
  }

  // Skip conflicts mode (used by setup): only process one-sided actions
  // (local_only push, cloud_only pull), skip all ambiguous/destructive actions.
  if (options.skipConflicts) {
    const skippedCount = actions.filter(
      (a) => a.action === "conflict" || a.action === "delete_local",
    ).length;
    actions = actions.filter(
      (a) => a.action !== "conflict" && a.action !== "delete_local",
    );
    if (skippedCount > 0) {
      console.log(
        chalk.gray(
          `  ${skippedCount} conflict${skippedCount > 1 ? "s" : ""} skipped (resolve with: memax agents sync)\n`,
        ),
      );
    }
  }

  // Filter out project-scoped cloud-only configs that don't belong to the
  // current project. Without this, running `memax agents sync` from ~/
  // would dump project configs (like .cursorrules from repo X) into the
  // home directory.
  const currentProjectScope = projectScopeResolution.scope;
  actions = actions.filter((a) => {
    if (!a.scope.startsWith("project:")) return true; // global → always sync
    // Project-scoped: only sync if it matches the current project
    if (a.scope === currentProjectScope) return true;
    // Cloud-only configs for other projects → skip silently
    if (a.action === "pull" && a.reason === "cloud_only") return false;
    // Conflict/push for configs we have locally → keep (user is in the project)
    return true;
  });

  // Index local configs by (agent, file_path, scope) for quick lookup
  const localByKey = new Map<string, (typeof localConfigs)[number]>();
  for (const c of localConfigs) {
    localByKey.set(`${c.loc.agent}|${c.loc.filePath}|${c.loc.scope}`, c);
  }

  // Index locations by (agent, file_path, scope) for pull path resolution
  const locByKey = new Map<string, AgentConfigLocation>();
  for (const loc of locations) {
    locByKey.set(`${loc.agent}|${loc.filePath}|${loc.scope}`, loc);
  }

  // Resolve a local write path for any config — even ones not discovered locally.
  // This enables pulling configs to a brand-new device where agent dirs don't exist yet.
  const resolveWritePath = (
    agent: string,
    filePath: string,
    scope: Scope,
  ): string | null => {
    const loc = locByKey.get(`${agent}|${filePath}|${scope}`);
    if (loc) return loc.path;

    return resolveAgentConfigWritePath(agent, filePath, scope, {
      cwd: process.cwd(),
      home: homedir(),
      currentProjectScope,
      findClaudeProjectDir,
    });
  };

  // Execute sync plan
  let pushed = 0;
  let pulled = 0;
  let deletedLocal = 0;
  let unchangedCount = 0;
  let skipped = 0;
  let errors = 0;
  const ackConfigs: {
    agent: string;
    file_path: string;
    scope: Scope;
    content_hash?: string;
    version: number;
    local_path?: string;
    deleted?: boolean;
  }[] = [];

  // Group actions by agent for display
  const byAgent = new Map<string, SyncPlanAction[]>();
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
          ackConfigs.push({
            agent: action.agent,
            file_path: action.file_path,
            scope: action.scope,
            content_hash: local.hash,
            version: action.version,
            local_path: local.loc.path,
          });
        }
        unchangedCount++;
        continue;
      }

      if (action.action === "push") {
        const local = localByKey.get(key);
        if (!local) {
          console.log(
            chalk.red(`    \u2717 ${action.file_path}`),
            chalk.gray("local file not found for push"),
          );
          errors++;
          continue;
        }
        try {
          await getClient().configs.upsert({
            agent: action.agent,
            file_path: action.file_path,
            scope: action.scope,
            content: local.content,
            device_id: deviceID,
            local_path: local.loc.path,
          });
          console.log(
            chalk.green(`    \u2191 ${action.file_path}`),
            chalk.gray(
              action.reason === "local_only"
                ? "pushing (new)"
                : "pushing (local newer)",
            ),
          );
          pushed++;
        } catch (err) {
          console.log(
            chalk.red(`    \u2717 ${action.file_path}`),
            chalk.gray((err as Error).message),
          );
          errors++;
        }
        continue;
      }

      if (action.action === "pull") {
        if (!action.config_id) {
          console.log(
            chalk.red(`    \u2717 ${action.file_path}`),
            chalk.gray("missing config ID from server"),
          );
          errors++;
          continue;
        }
        try {
          const writePath = resolveWritePath(
            action.agent,
            action.file_path,
            action.scope,
          );
          if (!writePath) {
            console.log(
              chalk.yellow(`    ? ${action.file_path}`),
              chalk.gray(
                action.scope !== "global" &&
                  action.scope !== currentProjectScope
                  ? "different project \u2014 skipped"
                  : "unknown agent \u2014 skipped",
              ),
            );
            skipped++;
            continue;
          }

          // For new files (not updates), ask user before writing
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

          const config = await getClient().configs.get(action.config_id);
          mkdirSync(dirname(writePath), { recursive: true });
          writeFileSync(writePath, config.content);
          if (action.version) {
            ackConfigs.push({
              agent: action.agent,
              file_path: action.file_path,
              scope: action.scope,
              content_hash: config.content_hash,
              version: action.version,
              local_path: writePath,
            });
          }
          console.log(
            chalk.cyan(`    \u2193 ${action.file_path}`),
            chalk.gray(isNewLocally ? "restored" : "pulling (cloud newer)"),
          );
          pulled++;
        } catch (err) {
          console.log(
            chalk.red(`    \u2717 ${action.file_path}`),
            chalk.gray((err as Error).message),
          );
          errors++;
        }
        continue;
      }

      if (action.action === "delete_local") {
        if (!options.pull) {
          if (!process.stdin.isTTY || !process.stdout.isTTY) {
            console.log(
              chalk.yellow(`    - ${action.file_path}`),
              chalk.gray(
                "cloud deleted this config \u2014 skipped in non-interactive mode",
              ),
            );
            skipped++;
            continue;
          }
          const resolution = await promptCloudDeletion(action.file_path);
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
                chalk.gray("local file missing \u2014 skipped"),
              );
              skipped++;
              continue;
            }
            try {
              await getClient().configs.upsert({
                agent: action.agent,
                file_path: action.file_path,
                scope: action.scope,
                content: local.content,
                device_id: deviceID,
                local_path: local.loc.path,
              });
              console.log(
                chalk.green(`    \u2191 ${action.file_path}`),
                chalk.gray("kept local and restored to cloud"),
              );
              pushed++;
            } catch (err) {
              console.log(
                chalk.red(`    \u2717 ${action.file_path}`),
                chalk.gray((err as Error).message),
              );
              errors++;
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
            moveFileToTrash(writePath, "agent-configs");
          }
          if (action.version) {
            ackConfigs.push({
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
          console.log(
            chalk.red(`    \u2717 ${action.file_path}`),
            chalk.gray((err as Error).message),
          );
          errors++;
        }
        continue;
      }

      if (action.action === "conflict") {
        if (!process.stdin.isTTY || !process.stdout.isTTY) {
          console.log(
            chalk.yellow(`    - ${action.file_path}`),
            chalk.gray("conflict skipped in non-interactive mode"),
          );
          skipped++;
          continue;
        }
        const resolution = await promptConflict(agent, action.file_path);
        if (resolution === "local") {
          const local = localByKey.get(key);
          if (local) {
            try {
              await getClient().configs.upsert({
                agent: action.agent,
                file_path: action.file_path,
                scope: action.scope,
                content: local.content,
                device_id: deviceID,
                local_path: local.loc.path,
              });
              console.log(
                chalk.green(`    \u2191 ${action.file_path}`),
                chalk.gray("kept local"),
              );
              pushed++;
            } catch (err) {
              console.log(
                chalk.red(`    \u2717 ${action.file_path}`),
                chalk.gray((err as Error).message),
              );
              errors++;
            }
          }
        } else if (resolution === "cloud" && action.config_id) {
          try {
            const writePath = resolveWritePath(
              action.agent,
              action.file_path,
              action.scope,
            );
            if (writePath) {
              const config = await getClient().configs.get(action.config_id);
              mkdirSync(dirname(writePath), { recursive: true });
              writeFileSync(writePath, config.content);
              if (action.version) {
                ackConfigs.push({
                  agent: action.agent,
                  file_path: action.file_path,
                  scope: action.scope,
                  content_hash: config.content_hash,
                  version: action.version,
                  local_path: writePath,
                });
              }
              console.log(
                chalk.cyan(`    \u2193 ${action.file_path}`),
                chalk.gray("used cloud"),
              );
              pulled++;
            }
          } catch (err) {
            console.log(
              chalk.red(`    \u2717 ${action.file_path}`),
              chalk.gray((err as Error).message),
            );
            errors++;
          }
        } else if (resolution === "merge" && action.config_id) {
          const local = localByKey.get(key);
          if (local) {
            try {
              const cloudConfig = await getClient().configs.get(
                action.config_id,
              );
              console.log(chalk.gray(`    Merging with LLM...`));
              const merged = await mergeConfigs(
                action.agent,
                action.file_path,
                local.content,
                cloudConfig.content,
              );
              if (merged) {
                // Write merged to local
                const writePath = resolveWritePath(
                  action.agent,
                  action.file_path,
                  action.scope,
                );
                if (writePath) {
                  mkdirSync(dirname(writePath), { recursive: true });
                  writeFileSync(writePath, merged);
                }
                // Push merged to cloud
                await getClient().configs.upsert({
                  agent: action.agent,
                  file_path: action.file_path,
                  scope: action.scope,
                  content: merged,
                  device_id: deviceID,
                  local_path: writePath ?? undefined,
                });
                console.log(
                  chalk.magenta(`    \u2194 ${action.file_path}`),
                  chalk.gray("merged (LLM)"),
                );
                pushed++;
              } else {
                skipped++;
              }
            } catch (err) {
              console.log(
                chalk.red(`    \u2717 ${action.file_path}`),
                chalk.gray((err as Error).message),
              );
              errors++;
            }
          }
        } else {
          console.log(
            chalk.gray(`    - ${action.file_path}`),
            chalk.gray("skipped"),
          );
          skipped++;
        }
      }
    }
  }

  if (ackConfigs.length > 0) {
    try {
      await getClient().configs.ack({
        device_id: deviceID,
        configs: ackConfigs,
      });
    } catch (err) {
      console.log(
        chalk.yellow("\n  Warning: failed to persist sync state"),
        chalk.gray((err as Error).message),
      );
    }
  }

  // Summary
  if (
    pushed === 0 &&
    pulled === 0 &&
    unchangedCount === 0 &&
    skipped === 0 &&
    errors === 0
  ) {
    console.log(
      chalk.gray(
        "  No configs in cloud yet. Push some first from a device that has them.\n",
      ),
    );
    return;
  }

  const parts: string[] = [];
  if (pushed > 0) parts.push(`${pushed} pushed`);
  if (pulled > 0) parts.push(`${pulled} restored`);
  if (deletedLocal > 0) parts.push(`${deletedLocal} deleted locally`);
  if (unchangedCount > 0) parts.push(`${unchangedCount} unchanged`);
  if (skipped > 0) parts.push(`${skipped} skipped`);
  if (errors > 0) parts.push(`${errors} errors`);

  console.log(chalk.bold(`\n  Done: ${parts.join(", ")}`));
  if (pulled > 0) {
    console.log(
      chalk.gray("  Restart your agents for restored configs to take effect."),
    );
  }
  console.log();
}

function formatAgentName(id: string): string {
  const names: Record<string, string> = {
    "claude-code": "Claude Code",
    cursor: "Cursor",
    codex: "Codex",
    gemini: "Gemini CLI",
    copilot: "GitHub Copilot",
    windsurf: "Windsurf",
    openclaw: "OpenClaw",
    opencode: "OpenCode",
    generic: "Generic",
  };
  return names[id] ?? id;
}

async function promptConflict(
  _agent: string,
  filePath: string,
): Promise<"local" | "cloud" | "merge" | "skip"> {
  const answer = await ask(
    chalk.yellow(
      `\n    ${filePath} has changes on both sides.\n` +
        `    [l] Keep local  [c] Use cloud  [m] Merge (LLM)  [s] Skip: `,
    ),
  );
  const a = answer.toLowerCase();
  if (a === "l") return "local";
  if (a === "c") return "cloud";
  if (a === "m") return "merge";
  return "skip";
}

async function promptCloudDeletion(
  filePath: string,
): Promise<"delete" | "local" | "skip"> {
  const answer = await ask(
    chalk.yellow(
      `\n    ${filePath} was deleted in cloud.\n` +
        `    [d] Delete local copy  [k] Keep local and restore cloud  [s] Skip: `,
    ),
  );
  const normalized = answer.toLowerCase();
  if (normalized === "d") return "delete";
  if (normalized === "k") return "local";
  return "skip";
}

async function mergeConfigs(
  agent: string,
  filePath: string,
  localContent: string,
  cloudContent: string,
): Promise<string | null> {
  try {
    const result = await getClient().configs.merge({
      local_content: localContent,
      cloud_content: cloudContent,
      file_path: filePath,
      agent,
    });
    return result.merged_content;
  } catch (err) {
    console.log(chalk.red(`    Merge failed: ${(err as Error).message}`));
    return null;
  }
}
