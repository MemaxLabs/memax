import chalk from "chalk";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  readdirSync,
  rmSync,
} from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir } from "node:os";
import { createInterface } from "node:readline";
import type { AgentDef } from "./setup-types.js";

// --- Instruction block ---

export const MEMAX_INSTRUCTION_BLOCK = `
<!-- memax:start -->
## Memax — Persistent Memory

You have access to Memax, a persistent cloud knowledge hub shared across all your AI agents.
Use it proactively — don't wait for the user to ask.

**At session start:** Use memax_recall to check for relevant context about the current project or task.
**During work:** When you discover important decisions, architecture details, debugging solutions,
or useful context — use memax_push to save them for future sessions.
**At session end:** Summarize key decisions, learnings, or context worth remembering and push them.

**What to remember:** Architecture decisions, API conventions, deployment processes, debugging
solutions, team preferences, project-specific knowledge. If you'd want to know it in a future
session, push it now.

**What NOT to remember:** Ephemeral task details, file contents (they're in git), obvious things.

Available tools: memax_recall (search), memax_push (save), memax_get (read full note),
memax_list (browse), memax_forget (delete outdated memories).
<!-- memax:end -->
`.trim();

// --- Instruction injection ---

export function injectInstructions(filePath: string): void {
  mkdirSync(dirname(filePath), { recursive: true });

  let content = "";
  if (existsSync(filePath)) {
    content = readFileSync(filePath, "utf-8");
  }

  // Remove existing memax block (idempotent)
  content = content.replace(
    /\n?<!-- memax:start -->[\s\S]*?<!-- memax:end -->\n?/,
    "",
  );

  // Append the block
  content = content.trimEnd() + "\n\n" + MEMAX_INSTRUCTION_BLOCK + "\n";

  writeFileSync(filePath, content);
}

export function removeInstructions(filePath: string): boolean {
  if (!existsSync(filePath)) return false;

  const content = readFileSync(filePath, "utf-8");
  const cleaned = content.replace(
    /\n?<!-- memax:start -->[\s\S]*?<!-- memax:end -->\n?/,
    "",
  );

  if (cleaned === content) return false;

  writeFileSync(filePath, cleaned.trimEnd() + "\n");
  return true;
}

// --- Skills ---

/** Resolve the agent's global skills directory, or null if skills aren't supported. */
export function agentSkillsDir(agent: AgentDef): string | null {
  const home = homedir();
  switch (agent.id) {
    case "claude-code":
      return join(home, ".claude", "skills");
    case "codex":
      return join(home, ".codex", "skills");
    default:
      return null;
  }
}

/**
 * Merge bundled skills from assets/skills/ into the agent's global skills directory.
 * When a file already exists and differs, prompt the user to choose.
 * Returns the number of skills installed/updated.
 */
export async function installSkills(agent: AgentDef): Promise<number> {
  const targetRoot = agentSkillsDir(agent);
  if (!targetRoot) return 0;

  const sourceRoot = getSkillsAssetDir();
  if (!existsSync(sourceRoot)) return 0;

  const skillNames = readdirSync(sourceRoot, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name);

  if (skillNames.length === 0) return 0;

  let installed = 0;

  for (const skillName of skillNames) {
    const srcDir = join(sourceRoot, skillName);
    const dstDir = join(targetRoot, skillName);
    const files = collectFiles(srcDir);

    let skillChanged = false;

    for (const relPath of files) {
      const srcPath = join(srcDir, relPath);
      const dstPath = join(dstDir, relPath);
      const srcContent = readFileSync(srcPath, "utf-8");

      if (existsSync(dstPath)) {
        const dstContent = readFileSync(dstPath, "utf-8");
        if (srcContent === dstContent) continue; // identical — skip

        // Conflict: ask the user
        const action = await promptSkillConflict(
          agent.name,
          skillName,
          relPath,
        );
        if (action === "skip") continue;
        // action === "replace" → fall through to write
      }

      mkdirSync(dirname(dstPath), { recursive: true });
      writeFileSync(dstPath, srcContent);
      skillChanged = true;
    }

    if (skillChanged || !existsSync(dstDir)) {
      // Ensure directory exists even if all files were skipped
      mkdirSync(dstDir, { recursive: true });
      installed++;
    }
  }

  return installed;
}

/** Remove installed memax skills from the agent's global skills directory. */
export function removeSkills(agent: AgentDef): boolean {
  const targetRoot = agentSkillsDir(agent);
  if (!targetRoot) return false;

  const sourceRoot = getSkillsAssetDir();
  if (!existsSync(sourceRoot)) return false;

  const skillNames = readdirSync(sourceRoot, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name);

  let removed = false;
  for (const skillName of skillNames) {
    const skillDir = join(targetRoot, skillName);
    if (existsSync(skillDir)) {
      rmSync(skillDir, { recursive: true });
      removed = true;
    }
  }
  return removed;
}

/** Recursively collect all relative file paths under a directory. */
export function collectFiles(dir: string, base?: string): string[] {
  const result: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const rel = base ? join(base, entry.name) : entry.name;
    if (entry.isDirectory()) {
      result.push(...collectFiles(join(dir, entry.name), rel));
    } else {
      result.push(rel);
    }
  }
  return result;
}

/** Ask the user what to do when a skill file conflicts. */
export function promptSkillConflict(
  agentName: string,
  skillName: string,
  filePath: string,
): Promise<"replace" | "skip"> {
  return new Promise((resolve) => {
    const rl = createInterface({
      input: process.stdin,
      output: process.stdout,
    });
    rl.question(
      chalk.yellow(
        `  ${agentName}: skill "${skillName}/${filePath}" already exists and differs.\n` +
          `  Replace with Memax version? [y/N] `,
      ),
      (answer) => {
        rl.close();
        resolve(answer.trim().toLowerCase() === "y" ? "replace" : "skip");
      },
    );
  });
}

/** Resolve the bundled skills asset directory (assets/skills/). */
export function getSkillsAssetDir(): string {
  // __dir is dist/commands/ (built) or src/commands/ (dev)
  // Package root is two levels up: dist/commands/../../ = packages/cli/
  const __dir = dirname(fileURLToPath(import.meta.url));
  const pkgRoot = join(__dir, "..", "..");
  return join(pkgRoot, "assets", "skills");
}
