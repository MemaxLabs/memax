import { Fragment, type ReactNode } from "react";

const MARK_CLASS =
  "bg-amber-200/70 dark:bg-amber-500/30 text-foreground rounded-sm px-0.5";

const REGEX_ESCAPE = /[.*+?^${}()|[\]\\]/g;

function tokenize(query: string): string[] {
  const seen = new Set<string>();
  const tokens: string[] = [];
  for (const raw of query.trim().toLowerCase().split(/\s+/)) {
    if (!raw) continue;
    if (seen.has(raw)) continue;
    seen.add(raw);
    tokens.push(raw);
  }
  return tokens;
}

/**
 * Wrap occurrences of any whitespace-separated token from `query` in <mark>.
 * Case-insensitive via RegExp (`i` flag) so offsets remain aligned with the
 * original `text` — avoids the Unicode case-fold pitfall where
 * `"İstanbul".toLowerCase()` expands to 9 code units and breaks offset-based
 * slicing. Tokens are joined longest-first so the alternation prefers the
 * longer token at any given position (e.g. "login" wins over "log"). Returns
 * the original string unchanged when there is no token or no match, so
 * callers can pass `query` unconditionally.
 */
export function highlightMatch(text: string, query: string): ReactNode {
  if (!text) return text;
  const tokens = tokenize(query);
  if (tokens.length === 0) return text;

  const sorted = [...tokens].sort((a, b) => b.length - a.length);
  const pattern = sorted.map((t) => t.replace(REGEX_ESCAPE, "\\$&")).join("|");
  const re = new RegExp(pattern, "gi");

  const nodes: ReactNode[] = [];
  let cursor = 0;
  let segmentIdx = 0;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    const start = match.index;
    const end = start + match[0].length;
    if (match[0].length === 0) {
      // Zero-length alternatives would loop forever; step past the cursor.
      re.lastIndex += 1;
      continue;
    }
    if (start > cursor) {
      nodes.push(
        <Fragment key={`t-${segmentIdx}`}>
          {text.slice(cursor, start)}
        </Fragment>,
      );
    }
    nodes.push(
      <mark key={`m-${segmentIdx}`} className={MARK_CLASS}>
        {text.slice(start, end)}
      </mark>,
    );
    cursor = end;
    segmentIdx++;
  }

  if (segmentIdx === 0) return text;
  if (cursor < text.length) {
    nodes.push(<Fragment key="tail">{text.slice(cursor)}</Fragment>);
  }
  return <>{nodes}</>;
}
