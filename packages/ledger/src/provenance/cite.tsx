import type { ReactNode } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { format } from "../lib/format";

export interface CiteProps {
  n: number | string;
  /** The memory ID it points at ("M-0219"): the tooltip and part of the accessible name. */
  title?: string;
  /** Where the receipt is. Without it, the numeral is not a link. */
  href?: string;
  className?: string;
}

// U+2060 WORD JOINER: the numeral never wraps away from the word before it.
const WORD_JOINER = "⁠";

/** A citation numeral inside serif text, pointing at a receipt. */
export function Cite({ n, title, href, className }: CiteProps) {
  const { strings, Link } = useLedger();
  const label = title
    ? format(strings.cite.sourceTitled, { n, title })
    : format(strings.cite.source, { n });
  let numeral: ReactNode;
  if (href !== undefined) {
    numeral = (
      <Link
        className={cx("mx-cite", className)}
        href={href}
        title={title}
        aria-label={label}
      >
        {n}
      </Link>
    );
  } else {
    numeral = (
      <span className={cx("mx-cite", className)} title={title}>
        <span className="mx-sr">{label}</span>
        <span aria-hidden="true">{n}</span>
      </span>
    );
  }
  return (
    <span className="mx-cite-wrap">
      {WORD_JOINER}
      {numeral}
    </span>
  );
}
