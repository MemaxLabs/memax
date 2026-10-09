import type { HTMLAttributes } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import type { MarkState } from "../lib/types";
import { Glyph } from "./glyph";

export interface StateMarkProps extends Omit<
  HTMLAttributes<HTMLSpanElement>,
  "children"
> {
  state: MarkState;
  /**
   * `true` shows the locale's state word, a string replaces it ("In sync",
   * "Kept by you"), and `false` shows the glyph alone, which then keeps the
   * state word as its accessible name and tooltip.
   */
  label?: boolean | string;
  size?: number;
}

/**
 * A state as a shape, a colour and a word, never colour alone. Ochre means a
 * person is needed: "waiting for Codex" is `working`, not `proposed`.
 */
export function StateMark({
  state,
  label = true,
  size = 10,
  className,
  ...rest
}: StateMarkProps) {
  const { strings } = useLedger();
  const visible = label !== false && label !== "";
  const text =
    typeof label === "string" && label !== "" ? label : strings.state[state];
  return (
    <span
      className={cx("mx-state", `mx-state--${state}`, className)}
      title={visible ? undefined : text}
      role={visible ? undefined : "img"}
      aria-label={visible ? undefined : text}
      {...rest}
    >
      <Glyph state={state} size={size} />
      {visible ? <span className="mx-state-label">{text}</span> : null}
    </span>
  );
}
