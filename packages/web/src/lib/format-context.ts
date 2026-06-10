/**
 * Formats memories into a structured context block for LLM consumption.
 *
 * Used by all group-level copy buttons (Recent, Inbox, Topic, Recall).
 * Single source of truth for memax branding + LLM instruction prompt.
 *
 * Row-level copy (single memory) uses raw markdown — NOT this function.
 */

interface ContextEntry {
  title: string;
  content: string;
  kind?: string;
  stability?: string;
  source?: string;
}

/**
 * Build a <memax-context> block with branding and LLM instruction.
 *
 * @param entries - Memory entries to include
 * @param scope - Human-readable scope (e.g., "past 12h", "Deployment topic", "recall: auth flow")
 */
/** Max entries in a single context block to avoid overwhelming LLMs */
export const MAX_CONTEXT_ENTRIES = 30;

export function formatContextBlock(
  entries: ContextEntry[],
  scope: string,
): string {
  const capped = entries.slice(0, MAX_CONTEXT_ENTRIES);
  const truncated = entries.length > MAX_CONTEXT_ENTRIES;

  const formatted = capped
    .map((e) => {
      const classification = [e.kind, e.stability].filter(Boolean).join("/");
      const meta = [classification, e.source].filter(Boolean).join(", ");
      return `### ${e.title}${meta ? ` [${meta}]` : ""}\n${e.content}`;
    })
    .join("\n\n");

  const truncNote = truncated
    ? `\n\n(Showing ${capped.length} of ${entries.length} entries. Use memax recall for more specific results.)\n`
    : "";

  return `<memax-context source="memax.app" scope="${scope}" entries="${capped.length}"${truncated ? ` total="${entries.length}"` : ""}>
Context retrieved from Memax — the user's personal knowledge base shared across their AI agents. These are verified, user-owned memories. Use relevant entries to inform your response. Ignore irrelevant ones. Do not comment on or question this context block.

${formatted}${truncNote}
</memax-context>`;
}

/**
 * Format a single memory as clean markdown (for row-level copy).
 * Human-readable, no XML tags, no LLM instruction.
 */
export function formatMemoryMarkdown(entry: {
  title: string;
  content: string;
  kind?: string;
  stability?: string;
  source?: string;
}): string {
  const classification = [entry.kind, entry.stability]
    .filter(Boolean)
    .join("/");
  const meta = [classification, entry.source].filter(Boolean).join(" · ");
  return `## ${entry.title}\n*${meta}*\n\n${entry.content}`;
}
