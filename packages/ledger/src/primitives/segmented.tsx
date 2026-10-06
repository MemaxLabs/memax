import type { ReactNode, Ref } from "react";
import { cx } from "../lib/cx";
import { useControllableState } from "../lib/use-controllable-state";
import { useRovingRadio } from "../lib/use-roving-radio";

export interface SegmentedOption<V extends string = string> {
  value: V;
  /** Put counts in the label ("Conflicts 1"). */
  label: ReactNode;
  /** Shown as the option's tooltip and read as its description. */
  hint?: string;
  disabled?: boolean;
}

export interface SegmentedProps<V extends string = string> {
  /** Two to four options. */
  options: ReadonlyArray<SegmentedOption<V>>;
  /** Controlled value. */
  value?: V;
  /** Uncontrolled starting value; the first option when omitted. */
  defaultValue?: V;
  /** Fires when the checked option changes. */
  onChange?: (value: V) => void;
  size?: "sm" | "md";
  /** The group's accessible name ("Autonomy", "Filter"). */
  label: string;
  disabled?: boolean;
  className?: string;
  ref?: Ref<HTMLDivElement>;
}

/**
 * A small set of mutually exclusive choices: an agent's autonomy, list filters,
 * Ask / Remember. A radio group with one tab stop and arrow-key selection.
 */
export function Segmented<V extends string = string>({
  options,
  value,
  defaultValue,
  onChange,
  size = "md",
  label,
  disabled = false,
  className,
  ref,
}: SegmentedProps<V>) {
  const [current, setCurrent] = useControllableState<V | undefined>(
    value,
    defaultValue ?? options[0]?.value,
    (next) => {
      if (next !== undefined) onChange?.(next);
    },
  );
  const selected = options.findIndex((o) => o.value === current);
  const { getItemProps } = useRovingRadio({
    count: options.length,
    selected,
    isDisabled: (i) => disabled || Boolean(options[i]?.disabled),
    onSelect: (i) => {
      const option = options[i];
      if (option && option.value !== current) setCurrent(option.value);
    },
  });
  return (
    <div
      ref={ref}
      className={cx("mx-seg", `mx-seg--${size}`, className)}
      role="radiogroup"
      aria-label={label}
      aria-disabled={disabled || undefined}
    >
      {options.map((option, i) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={i === selected}
          title={option.hint}
          disabled={disabled || option.disabled}
          className={cx("mx-seg-opt", i === selected && "is-on")}
          {...getItemProps(i)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
