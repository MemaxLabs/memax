// memax link / memax unlink: tie a working copy to a V2 space, so the
// daemon writes the space's compiled files into it.
import { randomUUID } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import chalk from "chalk";
import type { Command } from "commander";
import type { Memax, V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { controlRequest } from "../lib/daemon/control.js";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { linkRepo, unlinkRepo } from "../lib/daemon/registry.js";
import { DeviceState } from "../lib/daemon/state.js";
import {
  gitRoot,
  removeMemaxYmlSpace,
  writeMemaxYmlSpace,
} from "../lib/project-context.js";
import { confirmDefault } from "../lib/prompt.js";
import { resolveSpace, SpaceChoiceError } from "../lib/v2-space.js";
import { apiFailureMessage, tildify } from "./v2-output.js";

export interface LinkOptions {
  space?: string;
  yes?: boolean;
}

export interface LinkDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  out: (line: string) => void;
  /** Whether to ask before changing a target (a TTY). */
  interactive: boolean;
}

const NOT_IN_GIT =
  "Run memax link inside a git repository; Memax writes its files at the repository's root.";

export async function link(o: LinkOptions, d: LinkDeps): Promise<number> {
  const root = gitRoot(d.cwd);
  if (!root) {
    d.out(chalk.red(`  ${NOT_IN_GIT}`));
    return 1;
  }
  const { space } = await resolveSpace(d.memax, {
    space: o.space,
    cwd: d.cwd,
    paths: d.paths,
    pick: true,
  });
  writeMemaxYmlSpace(root, space.slug);
  const previous = linkRepo(d.paths, {
    root,
    space_id: space.id,
    space_slug: space.slug,
  });
  const targets = (await d.memax.v2.targets.list(space.id)).items;

  d.out("");
  d.out(
    `  ${chalk.green("●")} Linked ${chalk.bold(tildify(root))} to ${chalk.bold(space.slug)}.`,
  );
  if (previous && previous.space_id !== space.id) {
    d.out(
      chalk.gray(
        `    It was linked to ${previous.space_slug}; files from there stay as they are.`,
      ),
    );
  }
  d.out("");
  d.out(`  ${chalk.gray(".memax.yml".padEnd(12))}space: ${space.slug}`);
  const local = targets.filter(
    (t) => t.delivery === "local" && t.path && t.sync_state !== "off",
  );
  const names = local.map((t) => t.label);
  d.out(
    `  ${chalk.gray("Writes".padEnd(12))}${names.length > 0 ? names.join(" · ") : "nothing yet: the space has no file targets"}`,
  );
  d.out("");

  for (const t of local) await checkExisting(t, root, o, d);

  const running = await controlRequest(d.paths, { cmd: "reload" }, 1_500);
  if (running) {
    await controlRequest(d.paths, { cmd: "wake", space: space.id }, 1_500);
    d.out(`  The daemon writes them here within seconds of each compile.`);
  } else {
    d.out(
      `  ${chalk.yellow("○")} Start the daemon to write them: ${chalk.bold("memax daemon start")}`,
    );
  }
  d.out("");
  return 0;
}

/** A file already there that Memax didn't write: say what happens to it. */
async function checkExisting(
  t: V2.Target,
  root: string,
  o: LinkOptions,
  d: LinkDeps,
): Promise<void> {
  if (
    !t.path ||
    t.kind === "cursor_mdc" ||
    t.kind === "copilot" ||
    t.kind === "windsurf" ||
    t.kind === "claude_rules"
  )
    return;
  const abs = join(root, t.path);
  if (!existsSync(abs)) return;
  let content = "";
  try {
    content = readFileSync(abs, "utf8");
  } catch {
    return;
  }
  // A whole compile Memax wrote is its own. A file with a Memax block (V1's
  // instructions, say) is the person's: the daemon holds it until Memax
  // manages just the block.
  const block = content.includes("<!-- memax:start -->");
  if (content.includes("Compiled by Memax") && !block) return;
  const shim = t.kind === "claude_md" || t.kind === "gemini_md";
  if (shim && !t.settings.user_owned) {
    d.out(
      `  ${chalk.yellow("○")} ${t.path} is yours. Memax can keep one block in it and leave the rest alone.`,
    );
    const yes =
      o.yes ||
      (d.interactive &&
        (await confirmDefault(
          `    Manage one block in ${t.path} instead of the whole file? [Y/n] `,
        )));
    if (yes) {
      try {
        await d.memax.v2.targets.update(
          t.id,
          {
            settings: { user_owned: true },
            reason: `${t.path} already existed in the repository`,
          },
          { idempotencyKey: randomUUID(), via: "cli", ifMatch: t.version },
        );
        d.out(`    ${chalk.green("✓")} Memax manages one block in ${t.path}.`);
      } catch (err) {
        d.out(
          chalk.yellow(`    Couldn't change it: ${apiFailureMessage(err)}`),
        );
      }
    } else if (!d.interactive) {
      d.out(
        chalk.gray(
          `    Run memax link --yes to let Memax manage one block in it.`,
        ),
      );
    }
    d.out("");
    return;
  }
  if (!shim && block) {
    d.out(
      `  ${chalk.yellow("○")} ${t.path} has a Memax block in it, so Memax leaves the file alone.`,
    );
    d.out(chalk.gray(`    Take the block out to let Memax compile ${t.path}.`));
    d.out("");
  } else if (!shim) {
    d.out(
      `  ${chalk.yellow("○")} ${t.path} is already here and Memax didn't write it. Memax won't overwrite it:`,
    );
    d.out(
      chalk.gray(
        `    it shows as a hand edit you can pull in as proposals, or overwrite, in the app.`,
      ),
    );
    d.out("");
  }
}

export interface UnlinkDeps {
  paths: DaemonPaths;
  cwd: string;
  out: (line: string) => void;
}

export async function unlink(d: UnlinkDeps): Promise<number> {
  const root = gitRoot(d.cwd);
  if (!root) {
    d.out(chalk.red(`  ${NOT_IN_GIT}`));
    return 1;
  }
  const removedYml = removeMemaxYmlSpace(root);
  const entry = unlinkRepo(d.paths, root);
  if (!entry && !removedYml) {
    d.out(chalk.gray(`  ${tildify(root)} isn't linked to a space.`));
    return 0;
  }
  const running = await controlRequest(d.paths, { cmd: "reload" }, 1_500);
  if (!running) {
    const state = new DeviceState(d.paths);
    state.forgetRepo(root);
    state.flush();
  }
  const from = entry ? ` from ${chalk.bold(entry.space_slug)}` : "";
  d.out("");
  d.out(
    `  ${chalk.green("✓")} Unlinked ${chalk.bold(tildify(root))}${from}. The files Memax wrote stay as they are.`,
  );
  d.out("");
  return 0;
}

export function registerLinkCommands(program: Command): void {
  program
    .command("link")
    .description(
      "Link this repository to a space, so the daemon writes its compiled files here",
    )
    .option(
      "--space <slug>",
      "The space to link (default: .memax.yml, then the git remote)",
    )
    .option(
      "-y, --yes",
      "Let Memax manage one block in a CLAUDE.md you already have",
    )
    .action(async (opts: LinkOptions) => {
      try {
        process.exitCode = await link(opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          out: (l) => console.log(l),
          interactive: !!process.stdin.isTTY && !!process.stdout.isTTY,
        });
      } catch (err) {
        console.error(
          chalk.red(
            `  ${err instanceof SpaceChoiceError ? err.message : apiFailureMessage(err)}`,
          ),
        );
        process.exitCode = 1;
      }
    });

  program
    .command("unlink")
    .description(
      "Stop writing compiled files into this repository (the files stay)",
    )
    .action(async () => {
      process.exitCode = await unlink({
        paths: daemonPaths(),
        cwd: process.cwd(),
        out: (l) => console.log(l),
      });
    });
}
