// Which drift hashes Memax wrote to a path in the past, from the target's
// recent runs. Asked only for content the daemon doesn't recognise, before
// calling it a hand edit: an AGENTS.md from an earlier compile, brought
// back by `git pull` or a branch switch, isn't one.
import type { DaemonApi } from "./api.js";

const RUNS = 100;
const FRESH_MS = 60_000;

export class KnownCompiles {
  private cache = new Map<string, { at: number; hashes: Set<string> }>();

  constructor(
    private readonly api: DaemonApi,
    private readonly now: () => number = Date.now,
  ) {}

  /**
   * Whether any of the target's last runs wrote `hash` to `path`; null
   * when the runs couldn't be read (then the caller neither writes nor
   * reports).
   */
  async has(
    target: string,
    path: string,
    hash: string,
  ): Promise<boolean | null> {
    const key = `${path}\u0000${hash}`;
    let entry = this.cache.get(target);
    if (entry?.hashes.has(key)) return true;
    if (!entry || this.now() - entry.at > FRESH_MS) {
      try {
        const runs = await this.api.runs(target, RUNS);
        const hashes = new Set<string>();
        for (const run of runs) {
          for (const f of run.files) {
            if (f.path) hashes.add(`${f.path}\u0000${f.drift_sha256}`);
          }
        }
        entry = { at: this.now(), hashes };
        this.cache.set(target, entry);
      } catch {
        return null;
      }
    }
    return entry.hashes.has(key);
  }

  forget(target: string): void {
    this.cache.delete(target);
  }
}
