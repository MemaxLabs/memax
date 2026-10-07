import { useId } from "react";
import { cx } from "../lib/cx";
import { useLedger } from "../i18n/provider";
import { MarkPaths } from "./mark";

export interface SealProps {
  /** The date of the Keep, already formatted ("Oct 2"). */
  date: string;
  /** The memory's ID ("M-0219"). */
  id?: string;
  /** 112–136 on a memory's page, 76 on a just-kept card, 20–24 inline (ring and mark only). */
  size?: number;
  /** Stamp it: set this only on the moment of Keep. Reduced motion shows it at rest. */
  animate?: boolean;
  /** The word on the ring. Defaults to the locale's "Kept". */
  label?: string;
  className?: string;
}

// Wide (CJK) glyphs take about 1.5× the advance of a mono Latin glyph, so a
// Chinese label needs a smaller size to fit the ring without overlapping.
const WIDE = /[ᄀ-ᅟ⺀-꓏가-힣豈-﫿︰-﹏＀-｠￠-￦]/u;
function visualLength(text: string): number {
  let n = 0;
  for (const ch of text) n += WIDE.test(ch) ? 1.5 : 1;
  return n;
}

/**
 * The seal: what a person's Keep leaves on a memory. A ring with the word, the
 * date and the ID set around the Memax mark, stamped about 6° askew in `seal`.
 * Never decorate with it, and never more than one per view.
 */
export function Seal({
  date,
  id,
  size = 64,
  animate = false,
  label,
  className,
}: SealProps) {
  const { strings } = useLedger();
  const word = label ?? strings.seal.label;
  const uid = useId().replace(/[^A-Za-z0-9_-]/g, "");
  const S = size;
  const c = S / 2;
  const rOut = c - 1;
  const rIn = c - S * 0.2;
  const small = S < 40;
  const ring =
    [word, date, id].filter(Boolean).join(" · ").toUpperCase() + " · ";
  const approxCirc = 2 * Math.PI * (rIn + (rOut - rIn) * 0.35);
  const fs = Math.min(
    S * 0.125,
    (approxCirc * 0.94) / (visualLength(ring) * 0.66),
  );
  const rText = rIn + (rOut - rIn - fs * 0.7) / 2;
  const circ = 2 * Math.PI * rText;
  const pathId = `mx-seal-${uid}`;
  const markOffset = c - S * (small ? 0.27 : 0.17);
  const markScale = (S * (small ? 0.54 : 0.34)) / 128;
  return (
    <span
      className={cx("mx-seal", animate && "is-stamping", className)}
      style={{ width: S, height: S }}
      role="img"
      aria-label={[word, date, id]
        .filter(Boolean)
        .join(strings.common.listSeparator)}
    >
      <svg width={S} height={S} viewBox={`0 0 ${S} ${S}`} aria-hidden="true">
        <defs>
          <path
            id={pathId}
            d={`M ${c} ${c - rText} a ${rText} ${rText} 0 1 1 -0.01 0`}
          />
        </defs>
        <circle
          cx={c}
          cy={c}
          r={rOut}
          className="mx-seal-ring"
          strokeWidth={small ? 1.25 : 1.5}
        />
        {small ? null : (
          <circle
            cx={c}
            cy={c}
            r={rIn}
            className="mx-seal-ring"
            strokeWidth={1}
          />
        )}
        {small ? null : (
          <text
            className="mx-seal-text"
            style={{ fontSize: `${fs.toFixed(2)}px` }}
          >
            <textPath
              href={`#${pathId}`}
              textLength={(circ - fs * 0.4).toFixed(1)}
              lengthAdjust="spacing"
            >
              {ring}
            </textPath>
          </text>
        )}
        <g
          className="mx-seal-mark"
          transform={`translate(${markOffset} ${markOffset}) scale(${markScale})`}
        >
          <MarkPaths />
        </g>
      </svg>
    </span>
  );
}
