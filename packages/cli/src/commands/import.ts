import { Command } from "commander";
import chalk from "chalk";
import {
  readFileSync,
  readdirSync,
  statSync,
  watch,
  existsSync,
} from "node:fs";
import { join, relative, extname, resolve } from "node:path";
import { getClient } from "../lib/client.js";
import {
  normalizeFilePath,
  detectProjectContext,
} from "../lib/project-context.js";
import { listSyncSources, updateSyncSourceRun } from "../lib/config.js";
import { confirm } from "../lib/prompt.js";

interface ImportOptions {
  boundary?: string;
  watch?: boolean;
  ignore?: string;
  yes?: boolean;
}

const DEFAULT_IGNORE = new Set([
  "node_modules",
  ".git",
  ".next",
  "dist",
  "__pycache__",
  ".env",
  ".env.local",
  ".DS_Store",
]);

const SUPPORTED_EXTENSIONS = new Set([
  ".md",
  ".txt",
  ".ts",
  ".tsx",
  ".js",
  ".jsx",
  ".go",
  ".py",
  ".rs",
  ".yaml",
  ".yml",
  ".json",
  ".toml",
  ".sh",
  ".bash",
  ".zsh",
  ".css",
  ".html",
  ".sql",
  ".graphql",
  ".proto",
  ".dockerfile",
]);

// Duplicated in agent-configs.ts. Tiny + private; extracting to a
// shared lib would create a third file for an 8-line helper.
function formatAge(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(ms / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export async function importCommand(
  directory: string | undefined,
  options: ImportOptions,
): Promise<void> {
  const dir = directory ?? ".";
  const syncRoot = resolve(dir);

  const customIgnore = options.ignore
    ? new Set(options.ignore.split(",").map((s) => s.trim()))
    : new Set<string>();
  const ignoreSet = new Set([...DEFAULT_IGNORE, ...customIgnore]);
  const ignorePatterns = [...customIgnore].sort();
  const projectContext = detectProjectContext(syncRoot);

  console.log(chalk.blue("Scanning"), dir);

  const files = walkDir(syncRoot, ignoreSet);

  if (files.length === 0) {
    console.log(chalk.yellow("No supported files found."));
    updateSyncSourceRun(syncRoot, {
      ignorePatterns,
      defaultBoundary: options.boundary,
      mode: options.watch ? "watch" : "manual",
      scanCount: 0,
      pushed: 0,
      skipped: 0,
      errors: 0,
    });
    return;
  }

  console.log(chalk.gray(`Found ${files.length} files to import`));

  // Confirm if many files (>10) unless -y is passed
  if (files.length > 10 && !options.yes) {
    console.log(
      chalk.yellow(
        `\n  This will push ${files.length} files. Continue? (y/N) `,
      ),
    );
    const confirmed = await confirm("  ");
    if (!confirmed) {
      console.log(chalk.gray("  Cancelled.\n"));
      return;
    }
  }

  console.log();

  let pushed = 0;
  let skipped = 0;
  let errors = 0;

  for (const file of files) {
    const result = await pushFile(file, syncRoot, projectContext, options);
    if (result === "pushed") pushed++;
    else if (result === "skipped") skipped++;
    else if (result === "error") errors++;
  }

  updateSyncSourceRun(syncRoot, {
    ignorePatterns,
    defaultBoundary: options.boundary,
    mode: options.watch ? "watch" : "manual",
    scanCount: files.length,
    pushed,
    skipped,
    errors,
  });

  console.log();
  console.log(
    chalk.blue(`Imported ${pushed} files`),
    skipped > 0 ? chalk.gray(`(${skipped} skipped)`) : "",
    errors > 0 ? chalk.red(`(${errors} errors)`) : "",
  );
  console.log(
    chalk.gray(
      "  Missing local files are retained in Memax until removed explicitly.",
    ),
  );

  if (options.watch) {
    console.log(
      chalk.cyan(`\nWatching ${dir} for changes... (Ctrl+C to stop)`),
    );

    let debounceTimer: NodeJS.Timeout | null = null;
    const pendingChanges = new Set<string>();

    watch(syncRoot, { recursive: true }, (_eventType, filename) => {
      if (!filename) return;
      const fullPath = join(syncRoot, filename);

      if (!isSupportedFile(filename) || isIgnored(filename, ignoreSet)) return;

      pendingChanges.add(fullPath);
      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(async () => {
        for (const file of pendingChanges) {
          if (!existsSync(file)) {
            console.log(
              chalk.gray("  ~"),
              buildSyncSourcePath(syncRoot, file),
              chalk.gray("[deleted locally, retained in Memax]"),
            );
            continue;
          }
          await pushFile(file, syncRoot, projectContext, options);
        }
        pendingChanges.clear();
      }, 500);
    });
  }
}

async function pushFile(
  file: string,
  syncRoot: string,
  projectContext: Record<string, string>,
  options: ImportOptions,
): Promise<"pushed" | "skipped" | "error"> {
  try {
    const content = readFileSync(file, "utf-8");
    if (!content.trim()) return "skipped";

    const relPath = buildSyncSourcePath(syncRoot, file);
    const ext = extname(file);
    const contentType =
      ext === ".md"
        ? "markdown"
        : ext === ".json" || ext === ".yaml" || ext === ".yml"
          ? "structured"
          : "code";

    const memory = await getClient().push(content, {
      title: relPath,
      source: "import",
      sourcePath: relPath,
      contentType,
      projectContext,
    });

    console.log(
      chalk.green("  +"),
      relPath,
      chalk.gray(`[${memory.kind}/${memory.stability}]`),
    );
    return "pushed";
  } catch (err) {
    console.log(chalk.red("  x"), file, chalk.gray((err as Error).message));
    return "error";
  }
}

export function buildSyncSourcePath(syncRoot: string, file: string): string {
  const relativePath = relative(syncRoot, file);
  if (
    !relativePath ||
    relativePath.startsWith("..") ||
    relativePath === "." ||
    resolve(syncRoot, relativePath) !== resolve(file)
  ) {
    throw new Error("file is outside sync root");
  }
  return normalizeFilePath(relativePath);
}

export function importStatusCommand(): void {
  const sources = listSyncSources();

  console.log(chalk.bold("\n  Memax Import Status\n"));

  if (sources.length === 0) {
    console.log(chalk.gray("  No import sources recorded yet."));
    console.log(chalk.gray("  Run: memax import <dir>\n"));
    return;
  }

  for (const source of sources) {
    console.log(`  ${chalk.cyan(source.root_path)}`);
    console.log(`    kind             ${chalk.gray(source.kind)}`);
    console.log(
      `    deletion policy  ${chalk.gray("retain missing files in Memax")}`,
    );
    if (source.default_boundary) {
      console.log(
        `    boundary         ${chalk.gray(source.default_boundary)}`,
      );
    }
    if (source.ignore_patterns.length > 0) {
      console.log(
        `    ignore           ${chalk.gray(source.ignore_patterns.join(", "))}`,
      );
    }
    if (source.last_sync_at) {
      console.log(
        `    last import      ${chalk.gray(formatAge(source.last_sync_at))} (${chalk.gray(source.last_mode ?? "manual")})`,
      );
    }
    if (source.last_scan_count !== undefined) {
      console.log(
        `    last run         ${chalk.gray(`${source.last_scan_count} scanned, ${source.last_pushed ?? 0} pushed, ${source.last_skipped ?? 0} skipped, ${source.last_errors ?? 0} errors`)}`,
      );
    }
    console.log();
  }
}

function isSupportedFile(filename: string): boolean {
  return SUPPORTED_EXTENSIONS.has(extname(filename));
}

function isIgnored(filename: string, ignoreSet: Set<string>): boolean {
  const parts = filename.split(/[/\\]/);
  return parts.some((part) => ignoreSet.has(part) || part.startsWith("."));
}

function walkDir(dir: string, ignore: Set<string>): string[] {
  const files: string[] = [];

  function walk(currentDir: string): void {
    let entries: string[];
    try {
      entries = readdirSync(currentDir);
    } catch {
      return;
    }

    for (const entry of entries) {
      if (ignore.has(entry) || entry.startsWith(".")) continue;

      const fullPath = join(currentDir, entry);
      let stat;
      try {
        stat = statSync(fullPath);
      } catch {
        continue;
      }

      if (stat.isDirectory()) {
        walk(fullPath);
      } else if (stat.isFile() && SUPPORTED_EXTENSIONS.has(extname(entry))) {
        files.push(fullPath);
      }
    }
  }

  walk(dir);
  return files;
}

export function registerImportCommand(program: Command): void {
  const importCmd = program
    .command("import [directory]")
    .description("Import a directory of files into your Memax workspace")
    .option("-w, --watch", "Watch for changes (coming soon)")
    .option(
      "-b, --boundary <level>",
      "Visibility level: private, team, org",
      "private",
    )
    .option("--ignore <patterns>", "Comma-separated directories to ignore")
    .option("-y, --yes", "Skip confirmation for large imports")
    .action(importCommand);

  importCmd
    .command("status")
    .description("Show registered import sources and last run status")
    .action(importStatusCommand);
}
