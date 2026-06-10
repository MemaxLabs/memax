/**
 * Inline markdown snippet — renders bold/italic/code with optional search highlight.
 * Strips block-level markdown and truncates to maxLen.
 *
 * `flat` mode strips all inline emphasis (`**bold**`, `*italic*`, `` `code` ``)
 * and renders pure plain text. Use this in list-row previews where the row's
 * scan anchor is the title and the summary should be a single muted glance —
 * any in-preview emphasis creates a second scan anchor that fights the title.
 * Full markdown rendering is reserved for detail views. See kitchen section 04
 * "Flat vs full summary" for the visual comparison and rule.
 */
export function MarkdownSnippet({
  text,
  maxLen = 120,
  highlight,
  flat = false,
}: {
  text: string;
  maxLen?: number;
  highlight?: string;
  flat?: boolean;
}) {
  let cleaned = text
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/^>\s+/gm, "")
    .replace(/^\*\s+/gm, "")
    .replace(/^[-+]\s+/gm, "")
    .replace(/^\d+\.\s+/gm, "")
    .replace(/^---+$/gm, "")
    .replace(/~~([^~]+)~~/g, "$1")
    .replace(/__([^_]+)__/g, "**$1**")
    .replace(/(?<!\w)_([^_]+)_(?!\w)/g, "*$1*")
    .replace(/\n{2,}/g, " ")
    .replace(/\n/g, " ")
    .replace(/\s{2,}/g, " ")
    .trim();

  // Flat mode: strip inline emphasis markers so the preview renders as one
  // quiet glance with no second scan anchor fighting the row's title.
  if (flat) {
    cleaned = cleaned
      .replace(/\*\*([^*]+)\*\*/g, "$1")
      .replace(/\*([^*]+)\*/g, "$1")
      .replace(/`([^`]+)`/g, "$1");
  }

  let trimmed = cleaned.slice(0, maxLen);

  // Fix broken inline tokens at boundary
  const btCount = (trimmed.match(/`/g) || []).length;
  if (btCount % 2 !== 0) trimmed = trimmed.slice(0, trimmed.lastIndexOf("`"));
  const boldCount = (trimmed.match(/\*\*/g) || []).length;
  if (boldCount % 2 !== 0)
    trimmed = trimmed.slice(0, trimmed.lastIndexOf("**"));
  const italicCount = (trimmed.replace(/\*\*/g, "\0\0").match(/\*/g) || [])
    .length;
  if (italicCount % 2 !== 0) {
    for (let i = trimmed.length - 1; i >= 0; i--) {
      if (
        trimmed[i] === "*" &&
        (i === 0 || trimmed[i - 1] !== "*") &&
        (i === trimmed.length - 1 || trimmed[i + 1] !== "*")
      ) {
        trimmed = trimmed.slice(0, i);
        break;
      }
    }
  }

  trimmed = trimmed.trimEnd();
  // If token-fixing sliced everything away, fall back to plain text
  if (!trimmed) {
    trimmed = cleaned
      .slice(0, maxLen)
      .replace(/[*`_~#]/g, "")
      .trim();
  }
  const wasTruncated = cleaned.length > maxLen;
  const parts = trimmed.split(/(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`)/g);

  const renderHighlight = (str: string, key: string) => {
    if (!highlight?.trim()) return <span key={key}>{str}</span>;
    const escaped = highlight.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const segs = str.split(new RegExp(`(${escaped})`, "gi"));
    return (
      <span key={key}>
        {segs.map((s, j) =>
          s.toLowerCase() === highlight.toLowerCase() ? (
            <mark
              key={j}
              className="bg-amber-200/70 dark:bg-amber-500/30 text-foreground rounded-sm px-0.5"
            >
              {s}
            </mark>
          ) : (
            s
          ),
        )}
      </span>
    );
  };

  return (
    <>
      {parts.map((part, i) => {
        if (part.startsWith("**") && part.endsWith("**"))
          return (
            <strong key={i} className="font-semibold text-fg-2">
              {renderHighlight(part.slice(2, -2), `b${i}`)}
            </strong>
          );
        if (part.startsWith("*") && part.endsWith("*"))
          return <em key={i}>{renderHighlight(part.slice(1, -1), `i${i}`)}</em>;
        if (part.startsWith("`") && part.endsWith("`"))
          return (
            <code key={i} className="text-[0.9em] bg-surface-2 px-0.5 rounded">
              {part.slice(1, -1)}
            </code>
          );
        return renderHighlight(part, `t${i}`);
      })}
      {wasTruncated && <span className="text-fg-3">…</span>}
    </>
  );
}
