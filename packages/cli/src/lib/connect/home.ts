// memax connect on a machine whose home is linked (personal-layer design
// §6.1, §6.5, §7.2): the agent's own home file joins the link, or moves
// to the file the agent reads now (Codex's AGENTS.override.md), and the
// agent is connected to the Personal space too, so the hook's loads count
// reads of personal facts.
import { randomUUID } from "node:crypto";
import type { Memax, V2 } from "memax-sdk";
import { readHomeLink, writeHomeLink } from "../daemon/home-link.js";
import { tilde } from "../daemon/home-place.js";
import type { DaemonPaths } from "../daemon/paths.js";
import { describe, planHomeFiles } from "../home/files.js";

/** The home file each agent has in this slice. */
export const HOME_KIND: Partial<Record<V2.AgentKind, V2.TargetKind>> = {
  "claude-code": "claude_user",
  codex: "codex_user",
};

export interface HomeFileReport {
  state:
    | "added"
    | "moved"
    | "present"
    /** This machine's home isn't linked (memax link --home). */
    | "not_linked"
    /** It was unlinked (on the web, say): connect never links it again. */
    | "unlinked"
    /** Memax won't write it here (a symlink, a dotfiles repository, …). */
    | "refused"
    /** The agent's folder isn't on this machine. */
    | "absent";
  path?: string;
  note?: string;
}

/**
 * Adds the agent's home file to this machine's link, or moves it to the
 * file the agent reads now; leaves it when it is already there. Through
 * LinkMachine with this one file, which touches no other file of the
 * machine (one stopped on the web stays stopped) and stops this agent's
 * file at its old path, whose block then comes out.
 */
export async function connectHomeFile(
  kind: V2.TargetKind,
  d: {
    memax: Memax;
    paths: DaemonPaths;
    env: Record<string, string | undefined>;
  },
): Promise<HomeFileReport> {
  const link = readHomeLink(d.paths);
  if (!link) return { state: "not_linked" };
  const plan = planHomeFiles({ home: link.home, env: d.env }).find(
    (f) => f.kind === kind,
  );
  if (!plan) return { state: "absent" };
  const label = tilde(link.home, plan.abs);
  if (plan.refusal)
    return { state: "refused", path: label, note: plan.refusal };
  const had = link.files.find((f) => f.kind === kind);
  if (had?.path === plan.path) return { state: "present", path: label };

  const machine = (await d.memax.v2.machines.list(link.space_id)).items.find(
    (m) => m.id === link.machine_id,
  );
  if (!machine?.linked) return { state: "unlinked" };
  const res = await d.memax.v2.machines.link(
    link.space_id,
    {
      device_id: machine.device_id,
      name: machine.name,
      platform: machine.platform,
      files: [{ kind, path: plan.path }],
    },
    { idempotencyKey: randomUUID(), via: "cli" },
  );
  const t = res.targets.find((x) => x.kind === kind && x.path === plan.path);
  if (!t) return { state: "refused", path: label, note: "Memax didn't add it" };
  writeHomeLink(d.paths, {
    ...link,
    files: [
      ...link.files.filter((f) => f.kind !== kind),
      { target_id: t.id, kind, path: plan.path, abs: plan.abs },
    ],
  });
  return {
    state: had ? "moved" : "added",
    path: label,
    note: had ? `from ${tilde(link.home, had.abs)}` : describe(plan),
  };
}

/** The Personal space this machine's home is linked to, when it is one of yours. */
export async function linkedPersonal(
  memax: Memax,
  paths: DaemonPaths,
): Promise<V2.Space | null> {
  const link = readHomeLink(paths);
  if (!link) return null;
  const spaces = (await memax.v2.spaces.list()).items;
  return spaces.find((s) => s.id === link.space_id) ?? null;
}
