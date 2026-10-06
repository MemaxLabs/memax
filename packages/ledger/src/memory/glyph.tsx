import type { ReactNode } from "react";
import type { MarkState } from "../lib/types";

export interface GlyphProps {
  state: MarkState;
  size: number;
}

/** A state's shape. Geometric marks, not icons; always decorative (the word carries the meaning). */
export function Glyph({ state, size }: GlyphProps) {
  const s = size;
  const c = s / 2;
  const ring = (dash?: string) => (
    <circle
      cx={c}
      cy={c}
      r={c - 1.25}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeDasharray={dash}
    />
  );
  let shape: ReactNode;
  switch (state) {
    case "proposed":
      shape = ring();
      break;
    case "kept":
      shape = <circle cx={c} cy={c} r={c - 0.5} fill="currentColor" />;
      break;
    case "merged":
      shape = (
        <>
          <circle
            cx={c - s * 0.17}
            cy={c}
            r={c * 0.58}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.25}
          />
          <circle
            cx={c + s * 0.17}
            cy={c}
            r={c * 0.58}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.25}
          />
        </>
      );
      break;
    case "stale":
      shape = ring("2 2.2");
      break;
    case "faded":
      shape = (
        <circle cx={c} cy={c} r={c - 0.5} fill="currentColor" opacity={0.38} />
      );
      break;
    case "conflict":
      shape = (
        <>
          {ring()}
          <path
            d={`M${c} 0.5 A${c - 0.5} ${c - 0.5} 0 0 0 ${c} ${s - 0.5}Z`}
            fill="currentColor"
          />
        </>
      );
      break;
    case "forgotten":
      shape = (
        <rect
          x={0.5}
          y={c - 2.5}
          width={s - 1}
          height={5}
          rx={1}
          fill="currentColor"
        />
      );
      break;
    case "working":
      shape = (
        <path
          className="mx-glyph-arc"
          d={`M${c} 1.25 A${c - 1.25} ${c - 1.25} 0 1 1 1.25 ${c}`}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      );
      break;
    case "off":
      shape = (
        <>
          {ring()}
          <path
            d={`M${s * 0.22} ${s * 0.78} L${s * 0.78} ${s * 0.22}`}
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinecap="round"
          />
        </>
      );
      break;
  }
  return (
    <svg
      className="mx-glyph"
      width={s}
      height={s}
      viewBox={`0 0 ${s} ${s}`}
      aria-hidden="true"
    >
      {shape}
    </svg>
  );
}
