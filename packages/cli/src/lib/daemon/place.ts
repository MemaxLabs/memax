// Where a delivery writes: a linked repository, or this machine's home
// folder (a home link). The delivery, its hand-edit checks and a stopped
// target's clean-up are the same for both; what differs is which files a
// target may write, how a path is resolved, what is created on the way,
// and what happens to Memax's own files when a target stops.
import type { V2 } from "memax-sdk";
import {
  ensureParents,
  outputRefusal,
  resolveInRepo,
  validRepoPath,
} from "./safe-path.js";

export type PlaceTarget = Pick<V2.Target, "id" | "kind" | "path" | "root">;

export interface Place {
  kind: "repo" | "home";
  /** The repository's or the home folder's real path. */
  root: string;
  /** Why `file` isn't something `target` may write here, or null. */
  refusal(target: PlaceTarget, file: string): string | null;
  /** The absolute path of `rel`; throws UnsafePathError on the way out. */
  resolve(rel: string): Promise<string>;
  /** Creates the folders `rel` needs, refusing any that leads out. */
  ensureParents(rel: string): Promise<void>;
  /** Why nothing may be written to `rel` right now, or null. */
  unwritable(target: PlaceTarget, rel: string): Promise<string | null>;
  /** The mode of a file Memax creates (else the process's umask decides). */
  newFileMode?: number;
  /**
   * A stopped target's files Memax owns whole: in a repository they stay
   * (they are committed, and a person deletes them); at home Memax takes
   * them out while they hold exactly what it wrote.
   */
  removesOwnedWhenOff: boolean;
}

export function repoPlace(root: string): Place {
  return {
    kind: "repo",
    root,
    refusal: (t, file) =>
      t.root === "home"
        ? "is a home file, written by a home link, never into a repository"
        : outputRefusal(t, file),
    resolve: (rel) => resolveInRepo(root, rel),
    ensureParents: (rel) => ensureParents(root, rel),
    unwritable: async () => null,
    removesOwnedWhenOff: false,
  };
}

/** Whether `p` could be a file of a target, wherever it is. */
export function plausiblePath(p: string | undefined): p is string {
  return !!p && validRepoPath(p);
}
