/**
 * Pure helpers for compose-modal.tsx — split out so unit tests can
 * exercise the title-derivation logic without pulling React + Tiptap
 * into the test runtime.
 */

const TITLE_FALLBACK = "Untitled";
const TITLE_MAX = 80;

/**
 * Derive a title from the first meaningful line of markdown content.
 *
 *   - Empty / whitespace-only input → `"Untitled"`
 *   - Strip leading heading markers (`#`, `##`, ...), bullet markers
 *     (`-`, `*`), blockquote (`>`), numbered list (`1.`)
 *   - Cap at 80 characters
 *   - If stripping markdown punctuation leaves an empty string, fall
 *     back to the original line so we don't surface `"Untitled"` for a
 *     line the user clearly intended to be a title.
 */
export function deriveTitleFromContent(content: string): string {
  const firstLine = content
    .split("\n")
    .map((s) => s.trim())
    .find(Boolean);
  if (!firstLine) return TITLE_FALLBACK;
  const stripped = firstLine.replace(/^(\s*([#>*-]+|\d+\.)\s+)+/, "").trim();
  const candidate = stripped || firstLine;
  return candidate.slice(0, TITLE_MAX);
}
