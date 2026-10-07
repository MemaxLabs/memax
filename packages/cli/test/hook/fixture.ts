// A machine for the session-start hook's tests: a temporary home with the
// daemon's files, a linked repository with compiled files, and helpers to
// write the warm cache and run the hook in-process.
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { hookAgent } from "../../src/lib/hook/agents.js";
import {
  hookPaths,
  sessionStart,
  type HookPaths,
  type SessionStartResult,
} from "../../src/lib/hook/session-start.js";
import { writeSeen } from "../../src/lib/hook/seen.js";
import type {
  WarmForget,
  WarmGate,
  WarmSpace,
  WarmTarget,
} from "../../src/lib/hook/warm.js";

export const SPACE_ID = "11111111-1111-4111-8111-111111111111";

export interface Machine {
  base: string;
  home: string;
  repo: string;
  paths: HookPaths;
  /** Writes AGENTS.md (or another file) as compile `ref` with these lines. */
  compiled(ref: string, lines: string[], path?: string): void;
  warm(space: Partial<WarmSpace>): void;
  target(
    compile: string,
    refs: string[],
    over?: Partial<WarmTarget>,
  ): WarmTarget;
  /** Runs the hook in-process and saves what it told, as hook-main does. */
  run(
    agent?: string,
    event?: Record<string, unknown>,
    env?: Record<string, string>,
    now?: Date,
  ): SessionStartResult;
  cleanup(): void;
}

export function header(ref: string, slug = "acme-web"): string {
  return `<!-- Compiled by Memax from ${slug} at 2026-10-07 09:00 UTC (${ref}). Edit it at https://memax.app/${slug}/brief; edits here come back as proposals. -->`;
}

export function machine(opts: { linked?: boolean } = {}): Machine {
  const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-hook-")));
  const home = join(base, "home");
  const repo = join(base, "repo");
  mkdirSync(join(repo, ".git"), { recursive: true });
  const paths = hookPaths(join(home, ".memax", "daemon"));
  mkdirSync(paths.dir, { recursive: true });
  if (opts.linked !== false)
    writeFileSync(
      paths.repos,
      JSON.stringify({
        version: 1,
        repos: [
          {
            root: repo,
            space_id: SPACE_ID,
            space_slug: "acme-web",
            created_at: "2026-10-01T00:00:00Z",
          },
        ],
      }),
    );
  return {
    base,
    home,
    repo,
    paths,
    compiled(ref, lines, path = "AGENTS.md") {
      writeFileSync(
        join(repo, path),
        [
          header(ref),
          "# Acme web brief",
          "",
          "## Decisions",
          ...lines,
          "",
        ].join("\n"),
      );
    },
    warm(space) {
      const s: WarmSpace = {
        slug: "acme-web",
        updated_at: new Date().toISOString(),
        targets: [],
        forgotten: [],
        gates: [],
        ...space,
      };
      writeFileSync(
        paths.warm,
        JSON.stringify({ version: 1, spaces: { [SPACE_ID]: s } }),
      );
    },
    target(compile, refs, over = {}) {
      return {
        kind: "agents_md",
        path: "AGENTS.md",
        label: "AGENTS.md",
        sync_state: "in_sync",
        compile,
        refs,
        open_drift: 0,
        ...over,
      };
    },
    run(agent = "claude-code", event = {}, env = {}, now = new Date()) {
      const res = sessionStart({
        agent: hookAgent(agent),
        event: { cwd: repo, source: "startup", ...event },
        env,
        // Not the repository: the hook finds it from the event or the
        // agents' variables, as it must under Cursor (which runs user hooks
        // from ~/.cursor).
        cwd: base,
        paths,
        now,
      });
      if (res.seen) writeSeen(res.seen.path, paths.seen, res.seen.value);
      return res;
    },
    cleanup() {
      rmSync(base, { recursive: true, force: true });
    },
  };
}

export function forget(
  ref: string,
  at: Date,
  over: Partial<WarmForget> = {},
): WarmForget {
  return { id: `t-${ref}`, ref, with: [], at: at.toISOString(), ...over };
}

export function gate(
  ref: string,
  expires: Date,
  question = "Fly.io or Railway?",
): WarmGate {
  return {
    ref,
    question,
    agent: "codex",
    asked_at: new Date(expires.getTime() - 86_400_000).toISOString(),
    expires_at: expires.toISOString(),
  };
}
