"use client";

export function StarGlyph({
  size = 13,
  color = "var(--signature)",
  breathe,
  className = "",
}: {
  size?: number;
  color?: string;
  breathe?: boolean;
  className?: string;
}) {
  return (
    <span
      aria-hidden
      className={`inline-flex shrink-0 items-center justify-center leading-none ${breathe ? "state-slow-breathe" : ""} ${className}`}
      style={{
        width: size,
        height: size,
        fontSize: size,
        color,
        // The ✦ glyph ink sits below its em-box center in most fonts, so
        // a -1px nudge brings the star's visual center onto the text's
        // ink-center when the parent centers boxes (flex items-center).
        transform: "translateY(-1px)",
      }}
    >
      ✦
    </span>
  );
}
