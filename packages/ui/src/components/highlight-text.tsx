/** Highlights matching substring in plain text (case-insensitive). */
export function HighlightText({
  text,
  highlight,
}: {
  text: string;
  highlight?: string;
}) {
  if (!highlight?.trim()) return <>{text}</>;
  const escaped = highlight.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const parts = text.split(new RegExp(`(${escaped})`, "gi"));
  return (
    <>
      {parts.map((s, i) =>
        s.toLowerCase() === highlight.toLowerCase() ? (
          <mark
            key={i}
            className="bg-amber-200/70 dark:bg-amber-500/30 text-inherit rounded-sm px-0.5"
          >
            {s}
          </mark>
        ) : (
          s
        ),
      )}
    </>
  );
}
