// One evaluator of a memory's conditions against a repository's tree at a
// commit (the stale-detection design §1.1, §2.3): what each kind needs from
// the tree, and what it says about the condition. It reads a view of the
// tree, never git or GitHub, so the daemon (git plumbing) and the server
// (GitHub's API, through the compile service) evaluate the same way; the
// daemon runs a verbatim copy (packages/cli/scripts/sync-compiler.mjs).
// Nothing here does I/O, and nothing it returns holds a file's contents:
// outcomes, paths and a declared constraint.
//
//   path_exists     the path is in the tree (a file, a folder or a
//                   submodule), or a path under the glob's literal folder
//                   matches it
//   file_contains   the file holds the text, literally and case-sensitively;
//                   with word, where the characters on either side of it
//                   aren't ASCII letters, digits or underscores (where a \b
//                   would match, for text that starts and ends with one)
//   dep_present     the manifest declares the dependency, and the lowest
//                   version its declared constraint allows satisfies range,
//                   if there is one (versions.ts); a bare manifest name
//                   means any manifest of that name in the repository
//   file_unchanged  the file's blob id is the one attached
//
// Unknown is never a failure: anything the view can't say (a listing past
// its cap, a file past its size cap, a blob this clone doesn't have, a
// manifest it can't parse, a constraint outside the subset) is unknown. A
// path that isn't a file where a file is needed is unknown too.
import { globMatcher, globPrefix } from "./glob.js";
import {
  ecosystemOf,
  findDeclared,
  manifestKind,
  readManifest,
  type Manifest,
  type ManifestKind,
} from "./manifests.js";
import { lowestAllowed, satisfies } from "./versions.js";

/** A condition as checked: its kind and fields (the server's Condition). */
export interface CheckCondition {
  kind: string;
  path?: string;
  glob?: string;
  text?: string;
  word?: boolean;
  manifest?: string;
  name?: string;
  range?: string;
  blob?: string;
}

export type CheckOutcome = "holds" | "fails" | "unknown";

export type CheckChange =
  | "removed"
  | "renamed"
  | "no_match"
  | "text_missing"
  | "dependency_removed"
  | "constraint_changed"
  | "blob_changed";

/** Why a condition fails: paths and a declared constraint, never contents. */
export interface CheckEvidence {
  change: CheckChange;
  path?: string;
  renamed_to?: string;
  constraint?: string;
}

export interface CheckEvaluation {
  outcome: CheckOutcome;
  /** On a failure only. */
  evidence?: CheckEvidence;
  /** dep_present on a bare manifest name: the manifest that held. */
  resolved?: string;
}

/** What a path is at the commit. */
export interface TreeEntry {
  type: "blob" | "tree" | "commit";
  /** Its object id (a blob's is what file_unchanged compares). */
  id: string;
}

/**
 * The tree at one commit, as far as it was read. undefined is always
 * "couldn't say", and evaluates to unknown.
 */
export interface TreeView {
  /** What is at a path; null when nothing is. */
  entry(path: string): TreeEntry | null | undefined;
  /** A blob's contents as text. */
  text(path: string): string | undefined;
  /** Every file under a folder ("" the whole tree), sorted. */
  under(prefix: string): readonly string[] | undefined;
  /** Every file with this last segment, anywhere, sorted. */
  named(name: string): readonly string[] | undefined;
  /**
   * Whether a blob holds text as containsText says, for a view that can
   * answer on the bytes without decoding them (the same answer, for
   * UTF-8). Without it, file_contains reads text().
   */
  contains?(path: string, text: string, word: boolean): boolean | undefined;
}

/** What a condition needs read from the tree. */
export interface CheckNeeds {
  /** Paths whose entry (type and id) it reads. */
  readonly entries: readonly string[];
  /** Paths whose contents it reads. */
  readonly texts: readonly string[];
  /** Folders whose files it lists. */
  readonly prefixes: readonly string[];
  /** Last segments whose files it finds anywhere (a bare manifest name). */
  readonly names: readonly string[];
}

/** One empty list for every need a condition hasn't (a space has thousands). */
const NONE: readonly string[] = Object.freeze([]);

/** The kinds a tree can answer (pr_state is GitHub's). */
export const CHECKED_KINDS = [
  "path_exists",
  "file_contains",
  "dep_present",
  "file_unchanged",
];

/** What a condition needs, or null for a kind no tree answers. */
export function checkNeeds(c: CheckCondition): CheckNeeds | null {
  const n = (need: Partial<CheckNeeds>): CheckNeeds => ({
    entries: need.entries ?? NONE,
    texts: need.texts ?? NONE,
    prefixes: need.prefixes ?? NONE,
    names: need.names ?? NONE,
  });
  switch (c.kind) {
    case "path_exists":
      if (c.path) return n({ entries: [c.path] });
      if (c.glob) return n({ prefixes: [globPrefix(c.glob)] });
      return null;
    case "file_contains":
      if (!c.path || !c.text) return null;
      return n({ entries: [c.path], texts: [c.path] });
    case "file_unchanged":
      if (!c.path || !c.blob) return null;
      return n({ entries: [c.path] });
    case "dep_present": {
      const kind = c.manifest ? manifestKind(c.manifest) : null;
      if (!kind || !c.name) return null;
      return c.manifest!.includes("/")
        ? n({ entries: [c.manifest!], texts: [c.manifest!] })
        : n({ names: [kind] });
    }
    default:
      return null;
  }
}

const UNKNOWN: CheckEvaluation = { outcome: "unknown" };

/**
 * A repository path in the form the server's evidence takes
 * (conditions.ValidPath): evidence only names paths in it.
 */
export function validPath(p: string): boolean {
  if (p === "" || [...p].length > 300 || !/^[A-Za-z0-9._@+()[\]/-]+$/.test(p))
    return false;
  return p.split("/").every((s) => s !== "" && s !== "." && s !== "..");
}

function fails(
  change: CheckChange,
  path?: string,
  more: Partial<CheckEvidence> = {},
): CheckEvaluation {
  return {
    outcome: "fails",
    evidence: { change, ...(path && validPath(path) ? { path } : {}), ...more },
  };
}

const matchers = new Map<string, (path: string) => boolean>();

function matcherOf(glob: string): (path: string) => boolean {
  let m = matchers.get(glob);
  if (!m) {
    if (matchers.size >= 4096) matchers.clear();
    m = globMatcher(glob);
    matchers.set(glob, m);
  }
  return m;
}

/** Whether a sorted list has a path under prefix that matches. */
function anyUnder(
  sorted: readonly string[],
  prefix: string,
  match: (p: string) => boolean,
): boolean {
  const from = prefix ? `${prefix}/` : "";
  let lo = 0;
  let hi = sorted.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] < from) lo = mid + 1;
    else hi = mid;
  }
  for (let i = lo; i < sorted.length && sorted[i].startsWith(from); i++)
    if (match(sorted[i])) return true;
  return false;
}

const isWord = (code: number) =>
  (code >= 48 && code <= 57) ||
  (code >= 65 && code <= 90) ||
  (code >= 97 && code <= 122) ||
  code === 95;

/** Whether text holds needle; with word, apart from letters, digits and _ on either side. */
export function containsText(
  text: string,
  needle: string,
  word: boolean,
): boolean {
  if (!word) return text.includes(needle);
  for (let i = text.indexOf(needle); i >= 0; i = text.indexOf(needle, i + 1))
    if (
      !isWord(text.charCodeAt(i - 1)) &&
      !isWord(text.charCodeAt(i + needle.length))
    )
      return true;
  return false;
}

/** A declared constraint as evidence may carry it (one line, at most 64 characters). */
function constraintOf(s: string | null): Partial<CheckEvidence> {
  const chars = s ? [...s] : [];
  if (!s || chars.length > 64 || chars.some((ch) => ch < " " || ch === "\x7f"))
    return {};
  return { constraint: s };
}

/**
 * What a condition says at the commit the tree is from. prev.resolved is
 * the manifest a bare dep_present held in last: it is looked in first, and
 * named when it no longer holds.
 */
export function evaluate(
  c: CheckCondition,
  tree: TreeView,
  prev?: { resolved?: string },
): CheckEvaluation {
  switch (c.kind) {
    case "path_exists":
      return c.path
        ? pathExists(c.path, tree)
        : c.glob
          ? globExists(c.glob, tree)
          : UNKNOWN;
    case "file_contains":
      return fileContains(c, tree);
    case "file_unchanged":
      return fileUnchanged(c, tree);
    case "dep_present":
      return depPresent(c, tree, prev?.resolved);
    default:
      return UNKNOWN;
  }
}

function pathExists(path: string, tree: TreeView): CheckEvaluation {
  const e = tree.entry(path);
  if (e === undefined) return UNKNOWN;
  return e === null ? fails("removed", path) : { outcome: "holds" };
}

function globExists(glob: string, tree: TreeView): CheckEvaluation {
  const prefix = globPrefix(glob);
  const files = tree.under(prefix);
  if (!files) return UNKNOWN;
  return anyUnder(files, prefix, matcherOf(glob))
    ? { outcome: "holds" }
    : fails("no_match", prefix);
}

/** A file's text, or the evaluation when there is none to read. */
function fileText(path: string, tree: TreeView): string | CheckEvaluation {
  const e = tree.entry(path);
  if (e === undefined) return UNKNOWN;
  if (e === null) return fails("removed", path);
  if (e.type !== "blob") return UNKNOWN;
  return tree.text(path) ?? UNKNOWN;
}

function fileContains(c: CheckCondition, tree: TreeView): CheckEvaluation {
  if (!c.path || !c.text) return UNKNOWN;
  let found: boolean | undefined;
  if (tree.contains) {
    const e = tree.entry(c.path);
    if (e === undefined) return UNKNOWN;
    if (e === null) return fails("removed", c.path);
    if (e.type !== "blob") return UNKNOWN;
    found = tree.contains(c.path, c.text, c.word === true);
  } else {
    const text = fileText(c.path, tree);
    if (typeof text !== "string") return text;
    found = containsText(text, c.text, c.word === true);
  }
  if (found === undefined) return UNKNOWN;
  return found ? { outcome: "holds" } : fails("text_missing", c.path);
}

function fileUnchanged(c: CheckCondition, tree: TreeView): CheckEvaluation {
  if (!c.path || !c.blob) return UNKNOWN;
  const e = tree.entry(c.path);
  if (e === undefined) return UNKNOWN;
  if (e === null) return fails("removed", c.path);
  // A folder, a submodule, or a blob id of the other hash (SHA-1 and SHA-256).
  if (e.type !== "blob" || e.id.length !== c.blob.length) return UNKNOWN;
  return e.id === c.blob.toLowerCase()
    ? { outcome: "holds" }
    : fails("blob_changed", c.path);
}

/**
 * Each view's manifests, read once: a space's dep_present conditions on a
 * bare name read every manifest of that name, each of them.
 */
const manifestsRead = new WeakMap<TreeView, Map<string, Manifest | null>>();

function manifestAt(
  tree: TreeView,
  kind: ManifestKind,
  path: string,
): Manifest | null {
  let read = manifestsRead.get(tree);
  if (!read) {
    read = new Map();
    manifestsRead.set(tree, read);
  }
  let m = read.get(path);
  if (m === undefined) {
    const text = tree.text(path);
    m = text === undefined ? null : readManifest(kind, text);
    // Only what was read: a text the view couldn't give may come later.
    if (text !== undefined) read.set(path, m);
  }
  return m;
}

function depPresent(
  c: CheckCondition,
  tree: TreeView,
  resolved?: string,
): CheckEvaluation {
  const kind = c.manifest ? manifestKind(c.manifest) : null;
  if (!kind || !c.name) return UNKNOWN;
  const eco = ecosystemOf(kind);
  const bare = !c.manifest!.includes("/");
  let paths: readonly string[];
  if (bare) {
    const named = tree.named(kind);
    if (!named) return UNKNOWN;
    if (named.length === 0) return fails("removed", resolved);
    // The one it held in first, so it stays the one it holds in.
    paths =
      resolved && named.includes(resolved)
        ? [resolved, ...named.filter((p) => p !== resolved)]
        : named;
  } else {
    const e = tree.entry(c.manifest!);
    if (e === undefined) return UNKNOWN;
    if (e === null) return fails("removed", c.manifest);
    if (e.type !== "blob") return UNKNOWN;
    paths = [c.manifest!];
  }
  let unknown = false;
  let incomplete = false;
  let declared: { path: string; constraint: string | null } | undefined;
  for (const path of paths) {
    const m = manifestAt(tree, kind, path);
    if (!m) {
      unknown = true;
      continue;
    }
    if (!m.complete) incomplete = true;
    for (const d of findDeclared(m, c.name)) {
      const held: CheckEvaluation = {
        outcome: "holds",
        ...(bare ? { resolved: path } : {}),
      };
      if (!c.range) return held;
      declared ??= { path, constraint: d.constraint };
      const low = d.constraint === null ? null : lowestAllowed(d.constraint);
      if (!low) {
        unknown = true;
        continue;
      }
      const ok = satisfies(low, c.range, eco);
      if (ok === null) return UNKNOWN;
      if (ok) return held;
    }
  }
  if (unknown) return UNKNOWN;
  if (declared)
    return fails(
      "constraint_changed",
      declared.path,
      constraintOf(declared.constraint),
    );
  if (incomplete) return UNKNOWN;
  if (!bare) return fails("dependency_removed", c.manifest);
  // The manifest it held in went away, or none of them declares it now.
  if (resolved && !paths.includes(resolved)) return fails("removed", resolved);
  return fails(
    "dependency_removed",
    resolved ?? (paths.length === 1 ? paths[0] : undefined),
  );
}
