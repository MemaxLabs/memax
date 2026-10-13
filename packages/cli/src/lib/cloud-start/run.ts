// `memax hook cloud-start`: a cloud agent's session start (cloud
// warm-start design §3). A sandbox has no daemon and no warm cache, so it
// asks: the compile each committed file's header names, its drift hash,
// the commit and the branch, in one request with a 2.5 s deadline, and
// prints what changed in the record since, in the same <memax-context>
// block the local hook prints. Nothing at all on any failure, and nothing
// on a person's own machine, where the local hook speaks. Its own files
// ($HOME/.memax/start/) keep a resumed or compacted session, or a second
// hook in the same one, from hearing the same thing twice.
import { createHash, randomUUID } from "node:crypto";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { type HookAgent, wrapFor } from "../hook/agents.js";
import { renderBlock } from "../hook/render.js";
import { claimSession, readSeen, seenPath, type Seen } from "../hook/seen.js";
import { toSection } from "./answer.js";
import {
  apiUrl,
  credential,
  gitState,
  loadedFiles,
  personsMachine,
  repoRoot,
} from "./inputs.js";
import { postStart, type StartOutcome, type StartRequest } from "./request.js";

/** The request's deadline: then nothing is printed (design §3.6). */
export const DEADLINE_MS = 2_500;

const SOURCES = new Set(["startup", "resume", "clear", "compact"]);

export interface CloudStartInput {
  agent: HookAgent;
  /** The agent's stdin event, parsed ({} when there was none). */
  event: Record<string, unknown>;
  env: Record<string, string | undefined>;
  cwd: string;
  /** The sandbox's home: its own files go in $HOME/.memax/start/. */
  home: string;
  now: Date;
  /** The CLI's version, for User-Agent. */
  version: string;
  /** The request (tests pass a fake). */
  post?: (r: StartRequest) => Promise<StartOutcome>;
}

export interface CloudStartResult {
  /** What to print, wrapped for the agent; "" prints nothing. */
  output: string;
  /** Saved after printing: what this session has been told. */
  seen?: { path: string; dir: string; value: Seen };
  /** Why it printed nothing, for --debug: never a header, never the key. */
  skipped?: string;
  /** How long the request took, for --debug. */
  requestMs?: number;
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v !== "" ? v : undefined;
}

/** The start's own directory in the sandbox. */
export function startDir(home: string): string {
  return join(home, ".memax", "start");
}

export async function cloudStart(
  i: CloudStartInput,
): Promise<CloudStartResult> {
  const nothing = (skipped: string, requestMs?: number): CloudStartResult => ({
    output: "",
    skipped,
    ...(requestMs !== undefined ? { requestMs } : {}),
  });
  if (personsMachine(i.home))
    return nothing(
      "this is a person's machine (~/.memax): the local hook speaks",
    );
  const root = repoRoot(i.event, i.env, i.cwd);
  if (!root) return nothing("not in a git repository");
  const loaded = loadedFiles(root, i.agent);
  if (loaded.length === 0)
    return nothing("no compiled file here has Memax's header");
  const spaces = [...new Set(loaded.map((l) => l.file.space))];
  if (spaces.length > 1)
    return nothing(`the compiled files name ${spaces.length} spaces`);
  const cred = credential(i.env);
  if (!cred) return nothing("no MEMAX_START_KEY");
  const base = apiUrl(i.env);
  if (!base) return nothing("MEMAX_API_URL isn't an http(s) URL");

  const session =
    str(i.event.session_id) ??
    str(i.event.sessionId) ??
    str(i.env.CLAUDE_CODE_REMOTE_SESSION_ID);
  // Copilot says `new` where Claude Code says `startup`.
  const raw = str(i.event.source) ?? "startup";
  const source = SOURCES.has(raw) ? raw : "startup";
  const dir = startDir(i.home);
  if (session) {
    try {
      if (!claimSession(dir, `${i.agent.kind}:${session}`, source))
        return nothing("this session already started");
    } catch {
      // an unwritable home: go on, at worst a second hook prints too
    }
  }

  const files: V2.SessionStartFileInput[] = loaded.map((l) => ({
    path: l.file.path,
    compile: l.file.compile,
    drift_sha256: l.drift,
  }));
  const git = gitState(root);
  const body: V2.SessionStartInput = {
    agent: i.agent.kind === "other" ? "other" : (i.agent.kind as V2.AgentKind),
    ...(session ? { session_ref: session.slice(0, 255) } : {}),
    source: source as V2.SessionStartInput["source"],
    files,
    ...(git.commit ? { commit: git.commit } : {}),
    ...(git.branch ? { branch: git.branch } : {}),
    loaded_at: i.now.toISOString(),
  };
  // The same session, source and compiles count once, whoever sends them.
  const key = session
    ? `memax-start-${createHash("sha256")
        .update(
          [
            session,
            source,
            ...files.map((f) => `${f.path}\u0000${f.compile}`),
          ].join("\u0000"),
        )
        .digest("hex")
        .slice(0, 48)}`
    : `memax-start-${randomUUID()}`;
  const url = new URL(
    `/v2/spaces/${encodeURIComponent(spaces[0])}/session-starts`,
    base,
  );
  const started = performance.now();
  const outcome = await (i.post ?? postStart)({
    url,
    key: cred.key,
    body: JSON.stringify(body),
    headers: {
      "Idempotency-Key": key,
      "X-Memax-Via": "cli",
      "User-Agent": `memax-start/${i.version}`,
    },
    deadlineMs: DEADLINE_MS,
    env: i.env,
  });
  const requestMs = Math.round(performance.now() - started);
  if (!outcome.ok) return nothing(outcome.reason, requestMs);

  // What this session was told before (a resume, a compact). Per session:
  // the committed files don't change, so a new session hears it all.
  const seenFile = session
    ? seenPath(join(dir, "seen"), `${root}\u0000${session}`, i.agent.kind)
    : null;
  let section;
  try {
    section = toSection(
      outcome.data,
      loaded.map((l) => l.file),
      seenFile ? readSeen(seenFile) : null,
      i.now,
    );
  } catch (err) {
    return nothing(`the answer: ${(err as Error).message}`, requestMs);
  }
  const block = renderBlock({
    repo: section.block,
    maxTokens: i.agent.maxTokens,
    mode: "cloud",
  });
  return {
    output: wrapFor(i.agent.format, block),
    ...(seenFile
      ? {
          seen: { path: seenFile, dir: join(dir, "seen"), value: section.seen },
        }
      : {}),
    ...(block === "" ? { skipped: "nothing changed" } : {}),
    requestMs,
  };
}
