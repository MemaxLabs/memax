// Files a pull holds (DriftResolve, Pull: "Both lines go to Review as
// proposals. The file stays as it is until you keep or reject them."). The
// server says so: the target is `held`, `holds` names the files, and each
// baseline file that is an accepted hand edit carries `held`.
//
// Against a server that predates holds (no `held` on an accepted edit),
// the daemon can't tell whether the pulled proposals are decided, so it
// doesn't write: an accepted edit with the delivered run still named is a
// pull (an overwrite forgets the run), and it is held.
import type { V2 } from "memax-sdk";

export function heldPaths(t: V2.Target): string[] {
  const out = new Set<string>();
  for (const h of t.holds ?? []) out.add(h.path);
  for (const f of t.delivered?.files ?? []) {
    if (!f.observation) continue;
    if (f.held === true) out.add(f.path);
    else if (f.held === undefined && t.delivered?.compile) out.add(f.path);
  }
  if (out.size === 0 && t.sync_state === "held" && t.path) out.add(t.path);
  return [...out];
}

/** "holding for 2 proposals in Review" */
export function holdDetail(t: V2.Target): string {
  const n = (t.holds ?? []).reduce((k, h) => k + h.proposals.length, 0);
  if (n === 0) return "holding a pulled edit until its proposals are decided";
  return `holding for ${n === 1 ? "1 proposal" : `${n} proposals`} in Review`;
}
