import type { HTMLAttributes, ReactNode, Ref } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import type { StatementState } from "../lib/types";

export interface MemoryTextProps extends HTMLAttributes<HTMLElement> {
  children?: ReactNode;
  /** Roman for kept, italic for proposed, a dotted underline for stale, ink-3 for merged. */
  state?: StatementState;
  /** Italic in ink-2 even when the state isn't `proposed`. */
  unconfirmed?: boolean;
  /** `md` is the `memory` style (16/24), `lg` is `memory-lg` (20/30). */
  size?: "md" | "lg";
  as?: "p" | "span" | "div" | "blockquote";
  ref?: Ref<HTMLElement>;
}

/**
 * A memory statement outside a row (a Brief fact, an answer's source). It
 * applies the state typography, so screens never set the serif or the state
 * styles by hand, and it tells assistive technology the state in words.
 */
export function MemoryText({
  children,
  state = "kept",
  unconfirmed,
  size = "md",
  as: Tag = "p",
  className,
  ref,
  ...rest
}: MemoryTextProps) {
  const { strings } = useLedger();
  return (
    <Tag
      ref={ref as Ref<HTMLParagraphElement & HTMLQuoteElement>}
      className={cx(
        "mx-mem",
        size === "lg" && "is-lg",
        `is-${state}`,
        (unconfirmed ?? state === "proposed") && "is-unconfirmed",
        className,
      )}
      {...rest}
    >
      {children}
      {state === "kept" ? null : (
        <span className="mx-sr">
          {" "}
          {format(strings.memoryText.stateSuffix, {
            state: strings.state[state],
          })}
        </span>
      )}
    </Tag>
  );
}
