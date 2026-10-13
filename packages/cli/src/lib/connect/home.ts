// memax connect on a machine whose home is linked (personal-layer design
// §6.1, §6.5, §7.2): the agent's own home file joins the link, or moves
// to the file the agent reads now (Codex's AGENTS.override.md), and the
// agent is connected to the Personal space too, so the hook's loads count
// reads of personal facts.
import { randomUUID } from "node:crypto";
import type { Memax, V2 } from "memax-sdk";
import {
  readHomeLink,
  writeHomeLink,
  type HomeFile,
  type HomeLink,
} from "../daemon/home-link.js";
import { tilde } from "../daemon/home-place.js";
import type { DaemonPaths } from "../daemon/paths.js";
import { describe, homeFile, planHomeFiles } from "../home/files.js";

/** The home file each agent has. Gemini CLI shares Antigravity's ~/.gemini/AGENTS.md. */
export const HOME_KIND: Partial<Record<V2.AgentKind, V2.TargetKind>> = {
  "claude-code": "claude_user",
  codex: "codex_user",
  "gemini-cli": "gemini_user",
  copilot: "copilot_user",
  windsurf: "windsurf_user",
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
  const plan = planHomeFiles({ home: link.home, env: d.env }, [kind]).find(
    (f) => f.kind === kind,
  );
  if (!plan) return { state: "absent" };
  const label = tilde(link.home, plan.abs);
  if (plan.refusal)
    return { state: "refused", path: label, note: plan.refusal };
  const had = link.files.find((f) => f.kind === kind);
  if (had?.path === plan.path) {
    // Who reads it changed (Gemini CLI listed AGENTS.md): the server's
    // read_by and home.json follow, so the hook and fading know.
    const was = had.read_by ?? [];
    if (
      plan.readBy.length === 0 ||
      (was.length === plan.readBy.length &&
        plan.readBy.every((r) => was.includes(r)))
    )
      return { state: "present", path: label };
    const target = (
      await d.memax.v2.targets.list(link.space_id, {
        machine: link.machine_id,
      })
    ).items.find((t) => t.id === had.target_id);
    if (!target) return { state: "present", path: label };
    await d.memax.v2.targets.update(
      target.id,
      {
        settings: { read_by: plan.readBy },
        reason: `${plan.agent} read it on this machine`,
      },
      { idempotencyKey: randomUUID(), via: "cli", ifMatch: target.version },
    );
    writeHomeLink(d.paths, {
      ...link,
      files: link.files.map((f) =>
        f.target_id === had.target_id ? homeFile(f.target_id, plan) : f,
      ),
    });
    return { state: "present", path: label, note: `read by ${plan.agent}` };
  }

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
      files: [
        {
          kind,
          path: plan.path,
          ...(plan.readBy.length > 0 ? { read_by: plan.readBy } : {}),
        },
      ],
    },
    { idempotencyKey: randomUUID(), via: "cli" },
  );
  const t = res.targets.find((x) => x.kind === kind && x.path === plan.path);
  if (!t) return { state: "refused", path: label, note: "Memax didn't add it" };
  writeHomeLink(d.paths, {
    ...link,
    files: [...link.files.filter((f) => f.kind !== kind), homeFile(t.id, plan)],
  });
  return {
    state: had ? "moved" : "added",
    path: label,
    note: [
      had ? `from ${tilde(link.home, had.abs)}` : describe(plan),
      plan.note,
    ]
      .filter(Boolean)
      .join(". "),
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

/**
 * Who reads a home file, on the server (the target's read_by, for fading)
 * and in home.json (for the hook): `change` applied, written only when it
 * changes something.
 */
export async function setReaders(
  d: { memax: Memax; paths: DaemonPaths },
  link: HomeLink,
  file: HomeFile,
  change: (readers: string[]) => string[],
  reason: string,
): Promise<void> {
  const target = (
    await d.memax.v2.targets.list(link.space_id, { machine: link.machine_id })
  ).items.find((t) => t.id === file.target_id);
  if (!target) return;
  const was = [...(target.settings.read_by ?? [])].sort();
  const next = [...new Set(change(was))].sort();
  if (next.join() !== was.join()) {
    await d.memax.v2.targets.update(
      target.id,
      { settings: { read_by: next }, reason },
      { idempotencyKey: randomUUID(), via: "cli", ifMatch: target.version },
    );
  }
  const now = readHomeLink(d.paths) ?? link;
  writeHomeLink(d.paths, {
    ...now,
    files: now.files.map((f) => {
      if (f.target_id !== file.target_id) return f;
      const { read_by: _, ...rest } = f;
      return next.length > 0 ? { ...rest, read_by: next } : rest;
    }),
  });
}

/**
 * The canonical copy in this machine's link, added through LinkMachine
 * when it isn't (OpenCode's instructions and Cursor's hook read it).
 * `added`: it wasn't in the link, so it isn't on disk yet.
 */
export async function linkedCanonical(d: {
  memax: Memax;
  paths: DaemonPaths;
  env: Record<string, string | undefined>;
}): Promise<
  | { link: HomeLink; canonical: HomeFile; added: boolean }
  | { report: HomeFileReport }
> {
  const link = readHomeLink(d.paths);
  if (!link) return { report: { state: "not_linked" } };
  const had = link.files.find((f) => f.kind === "agents_md");
  if (had) return { link, canonical: had, added: false };
  const res = await connectHomeFile("agents_md", d);
  if (res.state !== "added") return { report: res };
  const now = readHomeLink(d.paths);
  const canonical = now?.files.find((f) => f.kind === "agents_md");
  if (!now || !canonical)
    return {
      report: { state: "refused", note: "Memax didn't add its canonical copy" },
    };
  return { link: now, canonical, added: true };
}

/**
 * memax connect cursor on a machine whose home is linked (personal-layer
 * design §7.1): Cursor loads no file of the Personal space, so its
 * session-start hook prints the canonical copy's kept lines at every
 * session start; the canonical's read_by says Cursor reads it, for the
 * web and fading. `needsCompile`: the canonical isn't on disk yet.
 */
export async function connectCursorHome(d: {
  memax: Memax;
  paths: DaemonPaths;
  env: Record<string, string | undefined>;
}): Promise<HomeFileReport & { needsCompile?: boolean }> {
  const got = await linkedCanonical(d);
  if ("report" in got) return got.report;
  const { link, canonical, added } = got;
  const before = canonical.read_by ?? [];
  await setReaders(
    d,
    link,
    canonical,
    (r) => [...r, "cursor"],
    "Cursor's session-start hook prints it on this machine",
  );
  const path = tilde(link.home, canonical.abs);
  const note = "Cursor's session-start hook prints it at every session start";
  if (!added && before.includes("cursor"))
    return { state: "present", path, note };
  return { state: "added", path, note, needsCompile: added };
}
