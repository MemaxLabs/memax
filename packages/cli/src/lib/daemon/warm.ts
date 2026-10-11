// Keeps the warm-start cache (~/.memax/daemon/warm.json, lib/hook/warm.ts)
// current for the session-start hook: each linked space's compiled files
// (from every poll of its targets), and its recent forgets and waiting
// gates (fetched when the space changed, at most every few seconds); and,
// on a machine whose home is linked, the Personal space's files here, its
// forgets and its gates (the `home` entry, personal-layer design §7.1).
// The hook reads it without the network, so it is only as fresh as the
// last poll: within seconds while the daemon runs.
import type { V2 } from "memax-sdk";
import {
  readWarm,
  type WarmFile,
  type WarmForget,
  type WarmGate,
  type WarmHome,
  type WarmSpace,
  type WarmTarget,
} from "../hook/warm.js";
import { failureOf, type DaemonApi } from "./api.js";
import { cleanLine } from "./compiler/sanitize.js";
import { writeJsonAtomic } from "./json-file.js";
import type { Logger } from "./log.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";

/** The compiled files agents load at session start (lib/hook/agents.ts). */
const LOADED_KINDS = new Set(["agents_md", "claude_md", "gemini_md"]);
/** The home files a home link writes: what agents load in every session. */
const HOME_KINDS = new Set([
  "claude_user",
  "codex_user",
  "gemini_user",
  "copilot_user",
  "agents_md",
]);
/** The key refresh() and requestRefresh() take for the home link's space. */
export const HOME = "home";
const FORGET_WINDOW_MS = 30 * 24 * 60 * 60_000;
const MAX_FORGETS = 50;
const MAX_GATES = 20;
const QUESTION_CHARS = 240;
/** At most one fetch of a space's forgets and gates this often. */
export const MIN_REFRESH_MS = 5_000;

/** The cached view of a space's targets; a failed compile keeps the last good one. */
export function warmTargets(
  targets: V2.Target[],
  prev: WarmTarget[] = [],
  home = false,
): WarmTarget[] {
  return targets
    .filter(
      (t) =>
        (home
          ? t.root === "home" && HOME_KINDS.has(t.kind)
          : t.root !== "home" && LOADED_KINDS.has(t.kind)) &&
        t.delivery === "local" &&
        !!t.path,
    )
    .map((t) => {
      const last = t.last_compile;
      const good = last && last.status !== "failed" ? last : undefined;
      const before = prev.find((p) => p.path === t.path && p.kind === t.kind);
      const w: WarmTarget = {
        kind: t.kind,
        path: t.path!,
        label: t.label,
        sync_state: t.sync_state,
        open_drift: t.open_drift,
      };
      if (good) {
        w.compile = good.ref;
        w.compiled_at = good.compiled_at;
        // Every memory a line of the compile may cite: the items placed and
        // what connective prose rests on (an open question cites both sides).
        w.refs = [
          ...new Set([
            ...good.refs,
            ...good.files.flatMap((f) => [...f.refs, ...f.cites]),
          ]),
        ].sort();
      } else if (before?.compile) {
        w.compile = before.compile;
        w.compiled_at = before.compiled_at;
        if (before.refs) w.refs = before.refs;
      }
      return w;
    });
}

export function warmForgets(
  tombstones: V2.Tombstone[],
  now = Date.now(),
): WarmForget[] {
  return tombstones
    .filter((t) => now - Date.parse(t.forgotten_at) < FORGET_WINDOW_MS)
    .slice(0, MAX_FORGETS)
    .map((t) => ({
      id: t.id,
      ref: t.ref,
      with: [...t.with],
      at: t.forgotten_at,
    }));
}

export function warmGates(gates: V2.Gate[], now = Date.now()): WarmGate[] {
  return gates
    .filter((g) => g.status === "waiting" && Date.parse(g.expires_at) > now)
    .slice(0, MAX_GATES)
    .map((g) => {
      const q = cleanLine(g.question).text;
      return {
        ref: g.ref,
        question:
          q.length > QUESTION_CHARS ? `${q.slice(0, QUESTION_CHARS - 1)}…` : q,
        ...(g.agent ? { agent: g.agent } : {}),
        asked_at: g.created_at,
        expires_at: g.expires_at,
      };
    });
}

export interface WarmCacheOptions {
  paths: DaemonPaths;
  api: DaemonApi;
  log: Logger;
  now?: () => number;
  saveDelayMs?: number;
  /** At most one fetch of a space's forgets and gates this often (MIN_REFRESH_MS). */
  minRefreshMs?: number;
  /**
   * A linked space's memories newly forgotten (each tombstone's ref and
   * the refs forgotten with it), as soon as a fetch sees them: the
   * daemon's checks drop what they hold of them (checks.ts).
   */
  onForgotten?(spaceId: string, refs: string[]): void;
}

/** Every ref the cache's forgets name: each one's, and those forgotten with it. */
export function forgottenRefs(forgotten: WarmForget[]): string[] {
  return [...new Set(forgotten.flatMap((f) => [f.ref, ...f.with]))];
}

export class WarmCache {
  private data: WarmFile;
  private saveTimer: NodeJS.Timeout | null = null;
  private inflight = new Map<string, Promise<void>>();
  private again = new Set<string>();
  private lastRefresh = new Map<string, number>();
  private pending = new Map<string, NodeJS.Timeout>();
  private warned = new Set<string>();
  private stopped = false;
  private readonly now: () => number;

  constructor(private readonly o: WarmCacheOptions) {
    this.now = o.now ?? Date.now;
    this.data = readWarm(o.paths.warm);
  }

  private space(spaceId: string, slug: string): WarmSpace {
    const s = (this.data.spaces[spaceId] ??= {
      slug,
      updated_at: new Date(this.now()).toISOString(),
      targets: [],
      forgotten: [],
      gates: [],
    });
    s.slug = slug;
    return s;
  }

  /** What a poll of the space's targets says. */
  setTargets(spaceId: string, slug: string, targets: V2.Target[]): void {
    const s = this.space(spaceId, slug);
    const next = warmTargets(targets, s.targets);
    if (JSON.stringify(next) === JSON.stringify(s.targets)) return;
    s.targets = next;
    this.changed(s);
  }

  /** What a poll of this machine's home files says (its home link). */
  setHome(
    link: { space_id: string; space_slug: string; machine_id: string },
    targets: V2.Target[],
  ): void {
    const prev = this.data.home;
    const same =
      prev?.space_id === link.space_id && prev.machine_id === link.machine_id;
    const h: WarmHome = same
      ? prev
      : {
          space_id: link.space_id,
          machine_id: link.machine_id,
          slug: link.space_slug,
          updated_at: new Date(this.now()).toISOString(),
          targets: [],
          forgotten: [],
          gates: [],
        };
    h.slug = link.space_slug;
    const next = warmTargets(targets, h.targets, true);
    if (same && JSON.stringify(next) === JSON.stringify(h.targets)) return;
    h.targets = next;
    this.data.home = h;
    this.changed(h);
  }

  /** The home link is gone: so is its entry. */
  clearHome(): void {
    if (!this.data.home) return;
    delete this.data.home;
    clearTimeout(this.pending.get(HOME));
    this.pending.delete(HOME);
    this.schedule();
  }

  /** Fetches the space's forgets and waiting gates now. */
  refresh(spaceId: string, slug: string): Promise<void> {
    const running = this.inflight.get(spaceId);
    if (running) {
      this.again.add(spaceId);
      return running;
    }
    const p = this.fetch(spaceId, slug).finally(() => {
      this.inflight.delete(spaceId);
      if (this.again.delete(spaceId) && !this.stopped)
        this.requestRefresh(spaceId, slug);
    });
    this.inflight.set(spaceId, p);
    return p;
  }

  /** A refresh soon: now, or once MIN_REFRESH_MS has passed since the last. */
  requestRefresh(spaceId: string, slug: string): void {
    if (this.stopped || this.pending.has(spaceId)) return;
    const wait = Math.max(
      0,
      (this.lastRefresh.get(spaceId) ?? 0) +
        (this.o.minRefreshMs ?? MIN_REFRESH_MS) -
        this.now(),
    );
    if (wait === 0) {
      void this.refresh(spaceId, slug);
      return;
    }
    const t = setTimeout(() => {
      this.pending.delete(spaceId);
      void this.refresh(spaceId, slug);
    }, wait);
    t.unref?.();
    this.pending.set(spaceId, t);
  }

  /** A space's entry, or the home link's (HOME). */
  private entry(key: string): WarmSpace | undefined {
    return key === HOME ? this.data.home : this.data.spaces[key];
  }

  private async fetch(key: string, slug: string): Promise<void> {
    this.lastRefresh.set(key, this.now());
    const spaceId = key === HOME ? this.data.home?.space_id : key;
    if (!spaceId) return;
    try {
      const [gates, tombstones] = await Promise.all([
        this.o.api.waitingGates(spaceId),
        this.o.api.tombstones(spaceId),
      ]);
      const s = this.entry(key);
      if (this.stopped || !s) return;
      if (key !== HOME) this.space(spaceId, slug);
      const forgotten = warmForgets(tombstones, this.now());
      const waiting = warmGates(gates, this.now());
      this.warned.delete(key);
      if (key !== HOME) {
        const before = new Set(forgottenRefs(s.forgotten));
        const fresh = forgottenRefs(forgotten).filter((r) => !before.has(r));
        if (fresh.length) this.o.onForgotten?.(spaceId, fresh);
      }
      if (
        JSON.stringify(forgotten) === JSON.stringify(s.forgotten) &&
        JSON.stringify(waiting) === JSON.stringify(s.gates)
      )
        return;
      s.forgotten = forgotten;
      s.gates = waiting;
      this.changed(s);
    } catch (err) {
      const f = failureOf(err);
      if (!this.warned.has(key)) {
        this.warned.add(key);
        this.o.log.warn("couldn't refresh the warm-start cache", {
          space: slug,
          code: f.code,
          status: f.status,
        });
      }
    }
  }

  /** Keeps only these spaces (the ones linked here). */
  retain(spaceIds: Set<string>): void {
    for (const id of Object.keys(this.data.spaces)) {
      if (spaceIds.has(id)) continue;
      delete this.data.spaces[id];
      clearTimeout(this.pending.get(id));
      this.pending.delete(id);
      this.schedule();
    }
  }

  snapshot(): WarmFile {
    return this.data;
  }

  private changed(s: WarmSpace): void {
    s.updated_at = new Date(this.now()).toISOString();
    this.schedule();
  }

  private schedule(): void {
    if (this.saveTimer || this.stopped) return;
    this.saveTimer = setTimeout(() => {
      this.saveTimer = null;
      this.flush();
    }, this.o.saveDelayMs ?? 100);
    this.saveTimer.unref?.();
  }

  /** Writes the cache now. */
  flush(): void {
    if (this.saveTimer) {
      clearTimeout(this.saveTimer);
      this.saveTimer = null;
    }
    try {
      ensureDaemonDir(this.o.paths);
      writeJsonAtomic(this.o.paths.warm, this.data);
    } catch (err) {
      this.o.log.warn("couldn't write the warm-start cache", {
        error: (err as Error).message,
      });
    }
  }

  /** Waits for fetches in flight, writes once more, and stops. */
  async stop(): Promise<void> {
    this.stopped = true;
    for (const t of this.pending.values()) clearTimeout(t);
    this.pending.clear();
    await Promise.all([...this.inflight.values()]);
    if (this.saveTimer) this.flush();
  }
}
