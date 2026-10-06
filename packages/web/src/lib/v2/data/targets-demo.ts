import { CommandFailedError } from "./command-error";
import {
  DEMO_AGENTS_MD,
  DEMO_CHATGPT,
  DEMO_CLAUDE_MD,
  DEMO_CURSOR_FILE,
  type DemoCompiled,
  type DemoVariant,
} from "./demo-compiled";
import { DEMO_DRIFT, DEMO_TARGETS, demoRun } from "./demo-targets-data";
import type { ReviewItem } from "./review";
import type {
  CompiledFileView,
  DriftItemView,
  DriftResolution,
  TargetPreviewView,
  TargetsSource,
  TargetView,
} from "./targets";

/**
 * The demo's targets: the boards' four files behind TargetsSource, with
 * this browser session's commands applied. Compile now, a settings
 * change and an overwrite go through `compiling`, then settle in sync
 * after `settleMs`, as if the CLI had written the file. A pull holds the
 * file until each proposal it wrote is kept or rejected in Review, then
 * compiles it the same way. Commands replay by idempotency key like the
 * server does.
 */

const PERSON = {
  kind: "person" as const,
  self: true,
  initials: "ZZ",
  name: "Ziyang",
};

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

/** The compiler's header time, in UTC as it writes it: "2026-10-05 21:31 UTC". */
function headerStamp(iso: string): string {
  return `${new Date(iso).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

/** The content as compiled for the target's settings, with the run's ID and time in the header. */
function compiledFor(target: TargetView): DemoCompiled {
  const variant: DemoVariant = `${target.settings.include}:${target.settings.stale}`;
  const base =
    target.kind === "agents_md"
      ? DEMO_AGENTS_MD[variant]
      : target.kind === "chatgpt"
        ? DEMO_CHATGPT[variant]
        : target.kind === "claude_md"
          ? DEMO_CLAUDE_MD
          : DEMO_CURSOR_FILE;
  const run = target.lastCompile;
  if (!run) return base;
  return {
    ...base,
    content: base.content.replace(
      /at \d{4}-\d\d-\d\d \d\d:\d\d UTC \(C-\d+\)/,
      `at ${headerStamp(run.at)} (${run.ref})`,
    ),
  };
}

function fileView(
  target: TargetView,
  compiled: DemoCompiled,
): CompiledFileView {
  const path =
    target.kind === "chatgpt"
      ? null
      : target.kind === "cursor_mdc"
        ? DEMO_CURSOR_FILE.path
        : (target.path ?? target.label);
  return {
    path,
    label: path ?? "ChatGPT project instructions",
    content: compiled.content,
    bytes: compiled.bytes,
    lines: compiled.lines,
    refs: compiled.refs,
    cites: compiled.cites,
    dropped: compiled.dropped,
  };
}

export function createDemoTargets({
  now,
  commandDelayMs = 240,
  settleMs = 1200,
  nextRef,
  propose,
  decided = () => false,
}: {
  now: () => Date;
  commandDelayMs?: number;
  /** How long a compile takes before the file reads in sync. */
  settleMs?: number;
  /** Hands out the next display ID ("M-0439"). */
  nextRef: () => string;
  /** Puts new proposals in a space's Review queue. */
  propose: (slug: string, items: ReviewItem[]) => void;
  /** Whether a proposal was kept or rejected this session. */
  decided?: (slug: string, ref: string) => boolean;
}): TargetsSource {
  const lists = new Map<string, TargetView[]>(
    Object.entries(DEMO_TARGETS).map(([slug, list]) => [slug, [...list]]),
  );
  const drift = new Map<string, DriftItemView[]>(Object.entries(DEMO_DRIFT));
  const replays = new Map<string, unknown>();
  // The latest compile asked of each target, so an older one doesn't settle over it.
  const latest = new Map<string, number>();
  let runs = 885;

  const listOf = (slug: string) => lists.get(slug) ?? [];
  const find = (slug: string, id: string) => {
    const found = listOf(slug).find((t) => t.id === id);
    if (!found) throw new CommandFailedError({ kind: "not-found" });
    return found;
  };
  const put = (slug: string, next: TargetView) => {
    lists.set(
      slug,
      listOf(slug).map((t) => (t.id === next.id ? next : t)),
    );
    return next;
  };

  /**
   * The space's targets after this session's decisions: a hold whose
   * proposals were all kept or rejected lifts, and the file compiles.
   */
  function fresh(slug: string): TargetView[] {
    for (const t of listOf(slug)) {
      if (t.syncState !== "held") continue;
      const holding = t.holding.filter((ref) => !decided(slug, ref));
      if (holding.length === t.holding.length) continue;
      if (holding.length > 0) put(slug, { ...t, holding });
      else recompile(slug, { ...t, holding, syncState: "in_sync" });
    }
    return listOf(slug);
  }

  /** Starts a compile of the target, and settles it after settleMs. */
  function recompile(slug: string, target: TargetView): TargetView {
    const ref = `C-${String(runs++).padStart(4, "0")}`;
    const at = now().toISOString();
    const compiling = put(slug, {
      ...target,
      syncState:
        target.syncState === "drifted" || target.syncState === "held"
          ? target.syncState
          : "compiling",
      lastCompile: target.lastCompile && {
        ...target.lastCompile,
        ref,
        at,
        status: "compiled",
      },
    });
    const token = (latest.get(target.id) ?? 0) + 1;
    latest.set(target.id, token);
    setTimeout(() => {
      const live = listOf(slug).find((t) => t.id === target.id);
      // A newer command took over, or compiling was stopped meanwhile.
      if (!live || latest.get(target.id) !== token || live.syncState === "off")
        return;
      // A hand edit, or a pull's hold, keeps the file as it is.
      const drifted = live.syncState === "drifted" || live.syncState === "held";
      const run = live.lastCompile;
      put(slug, {
        ...live,
        syncState: drifted ? live.syncState : "in_sync",
        lastCompile: run && {
          ...demoRun(ref, compiledFor(live), run.files, at),
          status: drifted ? "compiled" : "delivered",
        },
      });
    }, settleMs);
    return compiling;
  }

  async function command<T>(key: string, run: () => T): Promise<T> {
    await sleep(commandDelayMs);
    if (replays.has(key)) return replays.get(key) as T;
    const result = run();
    replays.set(key, result);
    return result;
  }

  function preview(slug: string, id: string): TargetPreviewView | undefined {
    const target = fresh(slug).find((t) => t.id === id);
    if (!target) return undefined;
    const run = target.lastCompile;
    if (!run) {
      return {
        target,
        compile: null,
        reads: target.reads,
        files: [],
        copies: [],
      };
    }
    const compiled = compiledFor(target);
    const view = fileView(target, compiled);
    return {
      target,
      compile: { ref: run.ref, at: run.at },
      reads: target.reads,
      files: target.kind === "chatgpt" ? [] : [view],
      copies: target.kind === "chatgpt" ? [view] : [],
    };
  }

  return {
    peekList: (slug) => fresh(slug),
    peekPreview: preview,
    peekDrift: (slug, id) => {
      const target = fresh(slug).find((t) => t.id === id);
      return target ? { target, items: drift.get(id) ?? [] } : undefined;
    },
    async list({ space }) {
      return fresh(space.slug);
    },
    async preview({ space, target }) {
      const found = preview(space.slug, target.id);
      if (!found) throw new CommandFailedError({ kind: "not-found" });
      return found;
    },
    async drift({ space, target }) {
      fresh(space.slug);
      const found = find(space.slug, target.id);
      return { target: found, items: drift.get(target.id) ?? [] };
    },
    compile({ space, target, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = find(space.slug, target.id);
        if (live.syncState === "off") {
          throw new CommandFailedError({ kind: "decided" });
        }
        return recompile(space.slug, live);
      });
    },
    configure({ space, target, change, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = find(space.slug, target.id);
        if (live.version !== target.version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: live.version,
          });
        }
        const next: TargetView = {
          ...live,
          version: live.version + 1,
          settings: { ...live.settings, ...change.settings },
          delivery: change.delivery ?? live.delivery,
          syncState:
            change.enabled === false
              ? "off"
              : change.enabled === true && live.syncState === "off"
                ? "compiling"
                : live.syncState,
        };
        put(space.slug, next);
        return next.syncState === "off" ? next : recompile(space.slug, next);
      });
    },
    resolveDrift({ space, target, mode, idempotencyKey }) {
      return command(idempotencyKey, (): DriftResolution => {
        const live = find(space.slug, target.id);
        const items = drift.get(live.id) ?? [];
        if (items.length === 0) {
          throw new CommandFailedError({ kind: "decided" });
        }
        drift.set(live.id, []);
        const cleared = { ...live, openDrift: 0 };
        if (mode === "stop") {
          return {
            target: put(space.slug, { ...cleared, syncState: "off" }),
            proposals: [],
            waiting: 0,
            skipped: 0,
          };
        }
        if (mode === "overwrite") {
          return {
            target: recompile(space.slug, {
              ...cleared,
              holding: [],
              syncState: "in_sync",
            }),
            proposals: [],
            waiting: 0,
            skipped: 0,
          };
        }
        // Pull: each change becomes a proposal by the person on the
        // device; a removed line waits on a person, never forgotten.
        const at = now().toISOString();
        const proposals: ReviewItem[] = [];
        let waiting = 0;
        for (const change of items.flatMap((i) => i.changes)) {
          if (change.kind === "remove") {
            waiting += 1;
            continue;
          }
          proposals.push({
            ref: nextRef(),
            version: 1,
            statement: change.kind === "edit" ? change.newText : change.text,
            section: "conventions",
            state: "proposed",
            lifecycle: "proposed",
            external: false,
            by: PERSON,
            action: change.kind === "edit" ? "updated" : "proposed",
            at,
            session: null,
            updates: change.kind === "edit" ? change.ref : null,
            conflictsWith: null,
            intoSpace: null,
          });
        }
        propose(space.slug, proposals);
        // The file stays as edited until each proposal is decided.
        const holding = proposals.map((p) => p.ref);
        return {
          target: put(space.slug, {
            ...cleared,
            holding,
            syncState: holding.length > 0 ? "held" : "in_sync",
          }),
          proposals: proposals.map((p) => ({
            ref: p.ref,
            statement: p.statement,
          })),
          waiting,
          skipped: 0,
        };
      });
    },
  };
}
