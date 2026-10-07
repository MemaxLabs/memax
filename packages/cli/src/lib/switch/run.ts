// memax switch: moves a space from V1 to the V2 record (plan 25 §10), or
// back with --back. The server does the work (POST /v2/spaces/{space}:switch):
// it changes no V1 row, so switching back gives V1 exactly as it was. This
// flow shows what moves, asks first, and waits for a switch that runs in
// the background. src/commands/switch.ts prints it.
import { randomUUID } from "node:crypto";
import type { Memax, V2 } from "memax-sdk";
import type { DaemonPaths } from "../daemon/paths.js";
import { resolveSpace } from "../v2-space.js";

export interface SwitchOptions {
  space?: string;
  dryRun?: boolean;
  back?: boolean;
  /** --as: the kind a V1 team hub switches as (project or team). */
  as?: string;
  repository?: string;
  yes?: boolean;
  /** --wait: seconds to wait for a switch running in the background. */
  wait?: string;
  format?: string;
}

export interface SwitchDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  /**
   * Shows the preview and asks whether to switch; null when nobody can be
   * asked (no terminal, or --format json).
   */
  confirm: ((status: V2.SpaceSwitch) => Promise<boolean>) | null;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
}

export type SwitchOutcome =
  /** --dry-run: what would move; nothing changed. */
  | "preview"
  | "switched"
  /** On V2 already: nothing to do. */
  | "already"
  /** Still switching in the background when the wait ran out. */
  | "running"
  /** A step failed; running it again resumes it. */
  | "failed"
  /** The person said no. */
  | "declined"
  /** Nobody to ask, and no --yes. */
  | "needs_yes"
  | "switched_back"
  /** --back on a space that is on V1. */
  | "on_v1";

export interface SwitchReport {
  outcome: SwitchOutcome;
  space: V2.Space;
  status: V2.SpaceSwitch;
}

export const SWITCH_KINDS = ["project", "team"] as const;

/** The space's kind on V2: --as, else what it is. */
export function switchKind(status: V2.SpaceSwitch, as?: string): V2.SpaceKind {
  return (as as V2.SpaceKind | undefined) ?? status.preview.kind;
}

export async function runSwitch(
  o: SwitchOptions,
  d: SwitchDeps,
): Promise<SwitchReport> {
  const { space } = await resolveSpace(d.memax, {
    space: o.space,
    cwd: d.cwd,
    paths: d.paths,
  });
  const spaces = d.memax.v2.spaces;
  let status = await spaces.switchStatus(space.id);
  const report = (outcome: SwitchOutcome): SwitchReport => ({
    outcome,
    space: status.space,
    status,
  });

  if (o.back) {
    if (!status.space.v2_enabled_at) return report("on_v1");
    if (o.dryRun) return report("preview");
    if (!o.yes) {
      if (!d.confirm) return report("needs_yes");
      if (!(await d.confirm(status))) return report("declined");
    }
    status = await spaces.switchToV1(space.id, {
      idempotencyKey: `switch-v1:${space.id}:${randomUUID()}`,
    });
    return report("switched_back");
  }

  if (status.space.v2_enabled_at) return report("already");
  if (o.dryRun) return report("preview");
  // A switch running already: wait for it, whoever started it.
  if (status.state !== "running") {
    if (!o.yes) {
      if (!d.confirm) return report("needs_yes");
      if (!(await d.confirm(status))) return report("declined");
    }
    status = await spaces.switchToV2(space.id, {
      idempotencyKey: `switch-v2:${space.id}:${randomUUID()}`,
      kind: o.as as "project" | "team" | undefined,
      repository: o.repository,
    });
  }
  const sleep =
    d.sleep ?? ((ms: number) => new Promise((r) => setTimeout(r, ms)));
  const now = d.now ?? Date.now;
  const deadline = now() + Math.max(0, Number(o.wait ?? 120)) * 1000;
  while (status.state === "running" && now() < deadline) {
    await sleep(1000);
    status = await spaces.switchStatus(space.id);
  }
  switch (status.state) {
    case "switched":
      return report("switched");
    case "failed":
      return report("failed");
    default:
      return report("running");
  }
}
