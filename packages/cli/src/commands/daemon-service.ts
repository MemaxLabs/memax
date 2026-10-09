// memax daemon install | uninstall: start the daemon at login with the
// system's own service manager, a launchd agent on macOS or a systemd
// user unit on Linux. Nothing is written without a yes, and the file and
// the commands are shown first.
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir, userInfo } from "node:os";
import { dirname, join } from "node:path";
import chalk from "chalk";
import type { Command } from "commander";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { confirm } from "../lib/prompt.js";
import { binPath, DAEMON_NODE_FLAGS, stopDaemon } from "./daemon-control.js";
import { tildify } from "./v2-output.js";

export interface UnitPlan {
  manager: "launchd" | "systemd";
  path: string;
  content: string;
  enable: string[][];
  disable: string[][];
}

export interface UnitInput {
  platform: NodeJS.Platform;
  home: string;
  node: string;
  bin: string;
  uid: number;
  log: string;
  apiUrl?: string;
  xdgConfigHome?: string;
}

const LABEL = "app.memax.daemon";
const UNIT = "memax-daemon.service";

const xml = (s: string) =>
  s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");

/** A systemd ExecStart word: quoted, with specifiers and variables escaped. */
const sd = (s: string) =>
  `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/%/g, "%%").replace(/\$/g, "$$$$")}"`;

export function planUnit(i: UnitInput): UnitPlan | null {
  if (i.platform === "darwin") {
    const path = join(i.home, "Library", "LaunchAgents", `${LABEL}.plist`);
    const args = [i.node, ...DAEMON_NODE_FLAGS, i.bin, "daemon", "run"]
      .map((a) => `    <string>${xml(a)}</string>`)
      .join("\n");
    const env = i.apiUrl
      ? `  <key>EnvironmentVariables</key>\n  <dict>\n    <key>MEMAX_API_URL</key>\n    <string>${xml(i.apiUrl)}</string>\n  </dict>\n`
      : "";
    const content = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by memax daemon install. Remove it with: memax daemon uninstall -->
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${LABEL}</string>
  <key>ProgramArguments</key>
  <array>
${args}
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ProcessType</key>
  <string>Background</string>
  <key>LowPriorityIO</key>
  <true/>
  <key>StandardErrorPath</key>
  <string>${xml(i.log)}</string>
${env}</dict>
</plist>
`;
    return {
      manager: "launchd",
      path,
      content,
      enable: [["launchctl", "bootstrap", `gui/${i.uid}`, path]],
      disable: [["launchctl", "bootout", `gui/${i.uid}/${LABEL}`]],
    };
  }
  if (i.platform === "linux") {
    const path = join(
      i.xdgConfigHome || join(i.home, ".config"),
      "systemd",
      "user",
      UNIT,
    );
    const env = i.apiUrl
      ? `Environment=${sd(`MEMAX_API_URL=${i.apiUrl}`)}\n`
      : "";
    const content = `# Written by memax daemon install. Remove it with: memax daemon uninstall
[Unit]
Description=Memax daemon: writes compiled agent files into linked repositories
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${[i.node, ...DAEMON_NODE_FLAGS, i.bin].map(sd).join(" ")} daemon run
${env}Restart=on-failure
RestartSec=10
Nice=10

[Install]
WantedBy=default.target
`;
    return {
      manager: "systemd",
      path,
      content,
      enable: [
        ["systemctl", "--user", "daemon-reload"],
        ["systemctl", "--user", "enable", "--now", UNIT],
      ],
      disable: [["systemctl", "--user", "disable", "--now", UNIT]],
    };
  }
  return null;
}

export interface ServiceDeps {
  paths: DaemonPaths;
  plan: UnitPlan | null;
  bin: string;
  yes: boolean;
  interactive: boolean;
  out: (line: string) => void;
  ask: (question: string) => Promise<boolean>;
  run: (cmd: string[]) => { ok: boolean; output: string };
}

function show(
  d: ServiceDeps,
  plan: UnitPlan,
  verb: "writes" | "removes",
): void {
  d.out("");
  d.out(
    `  This ${verb} ${chalk.bold(tildify(plan.path))}${verb === "writes" ? ":" : "."}`,
  );
  if (verb === "writes") {
    d.out("");
    for (const line of plan.content.trimEnd().split("\n"))
      d.out(chalk.gray(`    ${line}`));
  }
  d.out("");
  d.out("  and runs:");
  for (const c of verb === "writes" ? plan.enable : plan.disable)
    d.out(chalk.gray(`    ${c.join(" ")}`));
  d.out("");
}

async function agreed(d: ServiceDeps, question: string): Promise<boolean> {
  if (d.yes) return true;
  if (!d.interactive) {
    d.out(
      chalk.yellow("  Nothing written. Run it again with --yes to go ahead."),
    );
    return false;
  }
  return d.ask(question);
}

function runAll(d: ServiceDeps, cmds: string[][]): boolean {
  for (const c of cmds) {
    const r = d.run(c);
    if (!r.ok) {
      d.out(chalk.red(`  ${c.join(" ")} failed.`));
      if (r.output.trim())
        d.out(chalk.gray(`    ${r.output.trim().split("\n").join("\n    ")}`));
      return false;
    }
  }
  return true;
}

export async function installService(d: ServiceDeps): Promise<number> {
  if (!d.plan) {
    d.out(
      chalk.yellow(
        `  Starting the daemon at login isn't supported on ${process.platform} yet. Run memax daemon start instead.`,
      ),
    );
    return 1;
  }
  if (/[/\\]_npx[/\\]/.test(d.bin)) {
    d.out(
      chalk.yellow(
        "  memax runs from npx's cache, which moves. Install it first, so the service keeps working:",
      ),
    );
    d.out(chalk.gray("    npm install -g memax-cli && memax daemon install"));
    return 1;
  }
  show(d, d.plan, "writes");
  if (!(await agreed(d, `  Write it and start the daemon at login? [y/N] `)))
    return 1;
  // One daemon per user: the service's own instance takes over.
  await stopDaemon({ paths: d.paths, out: () => {} });
  mkdirSync(dirname(d.plan.path), { recursive: true });
  writeFileSync(d.plan.path, d.plan.content, { mode: 0o644 });
  if (!runAll(d, d.plan.enable)) return 1;
  d.out(
    `  ${chalk.green("✓")} The daemon starts at login (${d.plan.manager}). Check it: memax daemon status`,
  );
  return 0;
}

export async function uninstallService(d: ServiceDeps): Promise<number> {
  if (!d.plan || !existsSync(d.plan.path)) {
    d.out(chalk.gray("  The daemon isn't installed as a service."));
    return 0;
  }
  show(d, d.plan, "removes");
  if (!(await agreed(d, `  Remove it and stop the daemon? [y/N] `))) return 1;
  runAll(d, d.plan.disable); // already stopped is fine
  unlinkSync(d.plan.path);
  if (d.plan.manager === "systemd")
    runAll(d, [["systemctl", "--user", "daemon-reload"]]);
  d.out(
    `  ${chalk.green("✓")} Removed. Start the daemon by hand when you want it: memax daemon start`,
  );
  return 0;
}

function deps(opts: { yes?: boolean }): ServiceDeps {
  const paths = daemonPaths();
  const bin = binPath();
  return {
    paths,
    bin,
    plan: planUnit({
      platform: process.platform,
      home: homedir(),
      node: process.execPath,
      bin,
      uid: userInfo().uid,
      log: paths.log,
      apiUrl: process.env.MEMAX_API_URL,
      xdgConfigHome: process.env.XDG_CONFIG_HOME,
    }),
    yes: !!opts.yes,
    interactive: !!process.stdin.isTTY,
    out: (l) => console.log(l),
    ask: (q) => confirm(q),
    run: (cmd) => {
      const r = spawnSync(cmd[0], cmd.slice(1), { encoding: "utf8" });
      return {
        ok: r.status === 0,
        output: `${r.stdout ?? ""}${r.stderr ?? ""}${r.error?.message ?? ""}`,
      };
    },
  };
}

export function registerDaemonServiceCommands(daemon: Command): void {
  daemon
    .command("install")
    .description(
      "Start the daemon at login (launchd on macOS, systemd on Linux); asks first",
    )
    .option("-y, --yes", "Write the service file without asking")
    .action(async (opts: { yes?: boolean }) => {
      process.exitCode = await installService(deps(opts));
    });
  daemon
    .command("uninstall")
    .description("Stop starting the daemon at login")
    .option("-y, --yes", "Remove the service file without asking")
    .action(async (opts: { yes?: boolean }) => {
      process.exitCode = await uninstallService(deps(opts));
    });
}
