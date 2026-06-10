"use client";

import type { CSSProperties } from "react";

interface MemaxStarProps {
  className?: string;
  style?: CSSProperties;
  animated?: boolean;
}

export function MemaxStar({
  className = "",
  style,
  animated = false,
}: MemaxStarProps) {
  return (
    <span
      aria-hidden
      className={`${className}${animated ? " state-slow-breathe" : ""}`}
      style={{ lineHeight: 1, ...style }}
    >
      ✦
    </span>
  );
}
