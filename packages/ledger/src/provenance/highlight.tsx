import type { ReactNode } from "react";
import { cx } from "../lib/cx";

export interface HighlightProps {
  children?: ReactNode;
  className?: string;
}

/** The highlighter: the span of serif text that matched a recall or a search. Ink on highlight only. */
export function Highlight({ children, className }: HighlightProps) {
  return <mark className={cx("mx-hl", className)}>{children}</mark>;
}
