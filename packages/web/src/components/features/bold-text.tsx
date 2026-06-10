/**
 * Renders **bold** markers in translation strings.
 * Use for instructional text where action words need emphasis.
 */
export function BoldText({
  text,
  className,
}: {
  text: string;
  className?: string;
}) {
  const parts = text.split(/\*\*(.+?)\*\*/);
  return (
    <span className={className}>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <span key={i} className="font-semibold text-fg-1">
            {part}
          </span>
        ) : (
          part
        ),
      )}
    </span>
  );
}
