// What memax init takes, what it depends on (so tests swap the machine,
// the browser and the agents' config writers), and what it reports.
import type { Memax, V2 } from "memax-sdk";
import type { DaemonPaths } from "../daemon/paths.js";
import type { AgentEntry } from "./agents.js";
import type { Git } from "./git.js";

export interface InitOptions {
  space?: string;
  /** Take the recommended answer to every question (CI and scripts). */
  yes?: boolean;
  format?: string;
  timing?: boolean;
  /** Detect and scan only: show what would be uploaded, upload nothing. */
  dryRun?: boolean;
  /** --no-personal: leave machine-local memory where it is. */
  personal?: boolean;
  /** --no-connect: don't write the agents' MCP settings or hooks. */
  connect?: boolean;
  /** --no-daemon: write the files once instead of starting the daemon. */
  daemon?: boolean;
  /** Seconds to wait for the judge (default 20). */
  wait?: string;
  /** Sign in with a code confirmed in any browser (RFC 8628). */
  device?: boolean;
}

/** The answers a person gives; tests script them. */
export interface Prompter {
  /** A yes/no question; Enter takes `def`. */
  confirm(question: string, def: boolean): Promise<boolean>;
  /** A free answer, trimmed. */
  ask(question: string): Promise<string>;
}

/** What writing one agent's MCP settings did. */
export type McpOutcome =
  | "written"
  | "present"
  | "unsupported"
  | { error: string };

export interface InitDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  home: string;
  /** PATH and the platform, to find agents without running them. */
  env: { path: string; platform: NodeJS.Platform };
  out: (line: string) => void;
  /** A person answers questions (a TTY, and not --format json). */
  interactive: boolean;
  prompt: Prompter;
  git: Git;
  /** Whether the CLI has credentials to try. */
  hasCredentials: () => boolean;
  /** Signs the person in (the browser); false when they didn't. */
  signIn: () => Promise<boolean>;
  /** Whether an agent's MCP settings already name Memax. */
  hasMcp: (agent: AgentEntry) => boolean;
  /** Writes an agent's MCP settings, as memax setup --mcp does. */
  writeMcp: (agent: AgentEntry) => Promise<McpOutcome>;
  /** Starts the daemon; false when it couldn't. */
  startDaemon: () => Promise<boolean>;
  /**
   * Compiles every target and writes the files here (memax compile);
   * pending waits for the compiles already asked for instead.
   */
  compile: (
    space: string,
    timeoutSeconds: number,
    opts?: { pending?: boolean },
  ) => Promise<CompileOutcome | null>;
  /** The web app's origin, for links ("" when unknown). */
  appUrl: string;
  version: string;
  now: () => number;
  sleep: (ms: number) => Promise<void>;
}

export interface CompileOutcome {
  timedOut: boolean;
  writer: "daemon" | "compile" | null;
  targets: Array<{
    label: string;
    kind: V2.TargetKind;
    sync_state: V2.SyncState;
    files: string[];
  }>;
}

/** One step's time against its budget (plan 25 §7.3). */
export interface StepTiming {
  step: string;
  ms: number;
  budgetMs: number | null;
}

/** What init did, for --format json and the closing summary. */
export interface InitReport {
  space: { slug: string; created: boolean } | null;
  personal: { slug: string; switched: boolean } | null;
  agents: Array<{
    kind: V2.AgentKind;
    name: string;
    /** What showed it: "~/.claude", ".cursor/ in this repo". */
    where: string;
    found: boolean;
    mcp: string;
    /**
     * Its session-start hook, for an agent init connected (null for the
     * rest): written, present, plugin (the Claude Code plugin runs it),
     * unsupported (the agent has none), windows, or "failed: …".
     */
    hook: string | null;
    autonomy: V2.Autonomy | null;
  }>;
  files: Array<{
    path: string;
    kind: string;
    location: string;
    statements: number;
    sent: number;
    skipped: number;
    hidden: number;
    compiled: boolean;
  }>;
  imports: Array<{
    space: string;
    id: string;
    proposed: number;
    folded: number;
    existing: number;
    refused: number;
    conflicts: number;
  }>;
  secrets: number;
  hidden: number;
  settled: number;
  kept: number;
  waiting: number;
  brief: string | null;
  targets: CompileOutcome["targets"];
  review: string | null;
  timings: StepTiming[];
  done: string[];
}

export function emptyReport(): InitReport {
  return {
    space: null,
    personal: null,
    agents: [],
    files: [],
    imports: [],
    secrets: 0,
    hidden: 0,
    settled: 0,
    kept: 0,
    waiting: 0,
    brief: null,
    targets: [],
    review: null,
    timings: [],
    done: [],
  };
}
