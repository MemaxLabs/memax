/**
 * Parse inline citations ([N] and [N-M]) from AI answer text.
 *
 * Streaming-safe: partial tokens like "[1" render as plain text until
 * the closing "]" arrives. Out-of-range citations (index < 1 or > sourceCount)
 * degrade to plain text so no dead buttons render.
 */

export type AnswerNode =
  | { kind: "text"; text: string }
  | { kind: "citation"; index: number };

const CITATION_RE = /\[(\d+)(?:-(\d+))?\]/g;

export function parseAnswerCitations(
  text: string,
  sourceCount: number,
): AnswerNode[] {
  if (!text) return [];

  const nodes: AnswerNode[] = [];
  let lastIndex = 0;

  for (const match of text.matchAll(CITATION_RE)) {
    const matchStart = match.index!;

    // Emit preceding text
    if (matchStart > lastIndex) {
      nodes.push({ kind: "text", text: text.slice(lastIndex, matchStart) });
    }

    const start = parseInt(match[1], 10);
    const end = match[2] ? parseInt(match[2], 10) : start;

    if (start < 1 || end < start || end > sourceCount) {
      // Out of range or malformed — render as plain text
      nodes.push({ kind: "text", text: match[0] });
    } else {
      // Expand range into individual citation nodes
      for (let i = start; i <= end; i++) {
        nodes.push({ kind: "citation", index: i });
      }
    }

    lastIndex = matchStart + match[0].length;
  }

  // Trailing text
  if (lastIndex < text.length) {
    nodes.push({ kind: "text", text: text.slice(lastIndex) });
  }

  return nodes;
}
