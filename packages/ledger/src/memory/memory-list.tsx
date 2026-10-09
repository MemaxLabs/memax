import type { HTMLAttributes, ReactNode, Ref } from "react";
import { cx } from "../lib/cx";

export interface MemoryListProps extends HTMLAttributes<HTMLUListElement> {
  /** `MemoryRow`s and `Redaction`s. */
  children?: ReactNode;
  ref?: Ref<HTMLUListElement>;
}

/**
 * The list that memory rows live in. Inside a `.mx-panel` it behaves like the
 * rows did on their own: the last row loses its bottom rule.
 */
export function MemoryList({ children, className, ...rest }: MemoryListProps) {
  return (
    <ul className={cx("mx-list", className)} {...rest}>
      {children}
    </ul>
  );
}
