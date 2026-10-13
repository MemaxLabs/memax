// Words a stale memory's cause suggests for Verify's "Update it" without a
// model: a renamed file's new path in place of the old one. The CLI's
// memax verify and the web's VerifyStale both offer it, so it lives here.

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
