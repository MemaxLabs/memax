/**
 * Defense-in-depth guard for LLM-generated memory metadata.
 *
 * The server validates title/summary before persistence (see
 * docs/engineering/llm-metadata-validation-plan.md), so these guards
 * should rarely activate. They exist to prevent broken protocol text
 * from reaching the UI if a pre-validation row is still in the database
 * or a regression bypasses server-side checks.
 *
 * Rule: never attempt to repair bad metadata client-side — just hide it.
 */

const JSON_FIELD_MARKER = /"(?:title|summary)"\s*:/;

/**
 * Returns true if the string looks like raw LLM protocol output
 * (JSON, XML, or contains JSON field markers) rather than display-safe text.
 */
function looksLikeProtocolArtifact(text: string): boolean {
  const trimmed = text.trim();
  if (
    trimmed.startsWith("{") ||
    trimmed.startsWith("[") ||
    trimmed.startsWith("<")
  ) {
    return true;
  }
  return JSON_FIELD_MARKER.test(trimmed);
}

/**
 * Returns the summary if it's display-safe, or empty string if it
 * looks like raw LLM protocol output.
 */
export function sanitizeSummary(summary: string | undefined | null): string {
  if (!summary) return "";
  if (looksLikeProtocolArtifact(summary)) return "";
  return summary;
}

/**
 * Returns the title if it's display-safe, or empty string if it
 * looks like raw LLM protocol output.
 */
export function sanitizeTitle(title: string | undefined | null): string {
  if (!title) return "";
  if (looksLikeProtocolArtifact(title)) return "";
  return title;
}
