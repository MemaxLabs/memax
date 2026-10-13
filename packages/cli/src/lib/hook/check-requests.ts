// The session-start hook's check requests (~/.memax/daemon/checks/requests/):
// one small file per session start in a linked repository, which the
// daemon takes to check that repository soon (trigger session_start,
// naming the agent), so a memory's page can say "checked when Claude
// Code started". The hook only writes the file: no socket, no git.
import {
  mkdirSync,
  readdirSync,
  readFileSync,
  renameSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { uniqueName } from "./hash.js";

export interface CheckRequest {
  /** The repository's root, as repos.json links it. */
  root: string;
  agent: string;
  at: string;
}

/** A request older than this is dropped unread: its session moved on. */
export const CHECK_REQUEST_TTL_MS = 10 * 60_000;

/** Queues a request; a failure only means no run for this session. */
export function requestCheck(dir: string, req: CheckRequest): void {
  try {
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    const name = uniqueName();
    const tmp = join(dir, `.${name}.tmp`);
    writeFileSync(tmp, JSON.stringify(req), { mode: 0o600 });
    renameSync(tmp, join(dir, `${name}.json`));
  } catch {
    // the next session asks again
  }
}

/** Every waiting request, oldest first, each removed as it is read. */
export function takeCheckRequests(
  dir: string,
  now = Date.now(),
): CheckRequest[] {
  let names: string[];
  try {
    names = readdirSync(dir)
      .filter((n) => n.endsWith(".json"))
      .sort();
  } catch {
    return [];
  }
  const out: CheckRequest[] = [];
  for (const n of names) {
    const file = join(dir, n);
    let req: Partial<CheckRequest> | null = null;
    try {
      req = JSON.parse(readFileSync(file, "utf8")) as Partial<CheckRequest>;
    } catch {
      // half-written or not ours: dropped below
    }
    try {
      unlinkSync(file);
    } catch {
      // another reader took it
    }
    if (typeof req?.root !== "string" || typeof req.agent !== "string")
      continue;
    if (!(now - Date.parse(req.at ?? "") < CHECK_REQUEST_TTL_MS)) continue;
    out.push({ root: req.root, agent: req.agent, at: req.at! });
  }
  return out;
}
