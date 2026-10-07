import type { HTMLAttributes, ReactNode, Ref } from "react";
import { cx } from "../lib/cx";

export interface KbdProps extends HTMLAttributes<HTMLElement> {
  children?: ReactNode;
  ref?: Ref<HTMLElement>;
}

/** A keycap, set in mono. Every primary action in Memax has one. */
export function Kbd({ children, className, ...rest }: KbdProps) {
  return (
    <kbd className={cx("mx-kbd", className)} {...rest}>
      {children}
    </kbd>
  );
}
