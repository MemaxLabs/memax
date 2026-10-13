// What changed in this clone where a check found a memory's condition
// failing: the last commit, at or before the commit the failure began at,
// that touched the condition's paths, and its diff for those paths. For
// `memax verify` to show in the terminal (the stale-detection design §3.1:
// none of it left the machine, so only the machine can show it).
import { runGit } from "../daemon/git-tree.js";

/** The diff's lines shown at most; the rest are counted. */
export const MAX_DIFF_LINES = 60;

export type LocalChange =
  | {
      kind: "change";
      commit: string;
      author: string;
      /** YYYY-MM-DD. */
      date: string;
      subject: string;
      lines: string[];
      /** Lines past MAX_DIFF_LINES, left out. */
      more: number;
    }
  /** The commit isn't in this clone (fetch first). */
  | { kind: "missing" };

/**
 * The change behind a failure at `at`, for these paths (a glob when
 * `glob`); null when git can't say (nothing touched them, or git failed).
 */
export async function localChange(
  root: string,
  at: string,
  paths: string[],
  glob = false,
): Promise<LocalChange | null> {
  const specs = paths
    .filter(Boolean)
    .map((p) => `:(${glob ? "glob" : "literal"})${p}`);
  if (!/^[0-9a-f]{7,64}$/i.test(at) || specs.length === 0) return null;
  const have = await runGit(root, ["cat-file", "-e", `${at}^{commit}`]);
  if (!have) return null;
  if (have.code !== 0) return { kind: "missing" };
  const log = await runGit(
    root,
    [
      "log",
      "-1",
      "-M90%",
      "--format=%H%x1f%h%x1f%an%x1f%as%x1f%s",
      at,
      "--",
      ...specs,
    ],
    { maxBytes: 64 * 1024 },
  );
  if (!log || log.code !== 0) return null;
  const [full, commit, author, date, subject] = log.stdout
    .toString()
    .trim()
    .split("\x1f");
  if (!full || !commit) return null;
  const show = await runGit(
    root,
    [
      "show",
      "--no-color",
      "--no-ext-diff",
      "--format=",
      "-M90%",
      full,
      "--",
      ...specs,
    ],
    { maxBytes: 1024 * 1024 },
  );
  if (!show || show.code !== 0) return null;
  const all = show.stdout.toString().replace(/\n+$/, "").split("\n");
  return {
    kind: "change",
    commit,
    author: author ?? "",
    date: date ?? "",
    subject: subject ?? "",
    lines: all.slice(0, MAX_DIFF_LINES),
    more: Math.max(0, all.length - MAX_DIFF_LINES),
  };
}
