// What a memory's conditions check and why one stopped holding, in words,
// for `memax check` and `memax verify`. The causes say what the web's
// VerifyStale says (web `verify-en.ts`, check*), so a person reads the
// same sentence in either place. Paths, names and text come from the
// conditions and their evidence: never a file's contents.
import type { V2 } from "memax-sdk";

/** What a condition checks: "`docs/adr.md` exists". */
export function conditionWords(c: V2.Condition): string {
  switch (c.kind) {
    case "path_exists":
      return c.glob ? `something matches ${c.glob}` : `${c.path ?? ""} exists`;
    case "file_contains":
      return `${c.path} contains ${quote(c.text)}${c.word ? " as a word" : ""}`;
    case "dep_present":
      return `${c.manifest} lists ${c.name}${c.range ? ` ${c.range}` : ""}`;
    case "file_unchanged":
      return `${c.path} is unchanged`;
    case "pr_state":
      return `${c.repo}#${c.number} is ${c.state}`;
  }
}

/** The path a condition names, or its glob or manifest. */
export function ownPath(c: V2.Condition): string {
  switch (c.kind) {
    case "path_exists":
      return c.path ?? c.glob ?? "";
    case "dep_present":
      return c.manifest;
    case "file_contains":
    case "file_unchanged":
      return c.path;
    case "pr_state":
      return "";
  }
}

/** Why a condition stopped holding, as VerifyStale says it. */
export function evidenceWords(
  c: V2.Condition,
  e: V2.CheckEvidence | undefined,
  branch: string,
): string {
  const own = ownPath(c);
  const path = e?.path ?? own;
  switch (e?.change) {
    case "renamed":
      return `${path} was renamed to ${e.renamed_to ?? ""} on ${branch}.`;
    case "no_match":
      return `Nothing on ${branch} matches ${own} any more.`;
    case "text_missing":
      return `${path} no longer contains ${c.kind === "file_contains" ? quote(c.text) : ""} on ${branch}.`;
    case "dependency_removed":
      return `${path} no longer lists ${c.kind === "dep_present" ? c.name : ""} on ${branch}.`;
    case "constraint_changed":
      return `${path} now asks for ${c.kind === "dep_present" ? c.name : ""} ${e.constraint ?? ""} on ${branch}.`;
    case "blob_changed":
      return `${path} changed on ${branch}.`;
    default:
      return `${path} is gone from ${branch}.`;
  }
}

/** Text a condition looks for, quoted so its spaces show. */
function quote(text: string): string {
  return `"${text}"`;
}

/**
 * The words with a renamed file's new path: every mention of its old
 * path, else of its old file name when only that changed and the
 * statement names it once. Null when the statement names neither, so
 * there's nothing to suggest without a model.
 */
export function renameSuggestion(
  statement: string,
  from: string,
  to: string,
): string | null {
  if (!from || !to || from === to) return null;
  if (statement.includes(from)) return statement.split(from).join(to);
  const base = (p: string) => p.slice(p.lastIndexOf("/") + 1);
  const [a, b] = [base(from), base(to)];
  if (a === b || !a) return null;
  // The file name as a token of its own: not inside a longer name, and
  // not another folder's file of the same name.
  const before = /[\w./-]/;
  const after = /[\w-]/;
  const hits: number[] = [];
  for (let i = statement.indexOf(a); i >= 0; i = statement.indexOf(a, i + 1))
    if (
      !before.test(statement[i - 1] ?? "") &&
      !after.test(statement[i + a.length] ?? "")
    )
      hits.push(i);
  if (hits.length !== 1) return null;
  const at = hits[0];
  return statement.slice(0, at) + b + statement.slice(at + a.length);
}
