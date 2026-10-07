import { useRef, type KeyboardEvent } from "react";

export interface RovingRadioOptions {
  count: number;
  /** Index of the checked option, or -1 when none is. */
  selected: number;
  onSelect: (index: number) => void;
  isDisabled?: (index: number) => boolean;
}

export interface RovingRadioItemProps {
  ref: (element: HTMLElement | null) => void;
  tabIndex: 0 | -1;
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  onClick: () => void;
}

/**
 * The WAI-ARIA radio group keyboard pattern for button-rendered radios.
 *
 * - The group is one tab stop: the checked option, or the first enabled one.
 * - Arrow keys move focus and check the option they land on, wrapping around;
 *   Home and End go to the ends. Disabled options are skipped.
 * - Space and Enter activate the focused button natively, which checks it.
 */
export function useRovingRadio({
  count,
  selected,
  onSelect,
  isDisabled = () => false,
}: RovingRadioOptions): {
  getItemProps: (index: number) => RovingRadioItemProps;
  focusItem: (index: number) => void;
} {
  const elements = useRef<Array<HTMLElement | null>>([]);

  const enabled = (i: number) => i >= 0 && i < count && !isDisabled(i);
  const step = (from: number, delta: 1 | -1) => {
    let i = from;
    for (let n = 0; n < count; n++) {
      i = (i + delta + count) % count;
      if (enabled(i)) return i;
    }
    return from;
  };
  const first = () => step(count - 1, 1);
  const last = () => step(0, -1);
  const tabStop = enabled(selected) ? selected : first();

  const getItemProps = (index: number): RovingRadioItemProps => ({
    ref: (element) => {
      elements.current[index] = element;
    },
    tabIndex: index === tabStop ? 0 : -1,
    onKeyDown: (event) => {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      let next: number;
      switch (event.key) {
        case "ArrowRight":
        case "ArrowDown":
          next = step(index, 1);
          break;
        case "ArrowLeft":
        case "ArrowUp":
          next = step(index, -1);
          break;
        case "Home":
          next = first();
          break;
        case "End":
          next = last();
          break;
        default:
          return;
      }
      event.preventDefault();
      if (!enabled(next)) return;
      elements.current[next]?.focus();
      onSelect(next);
    },
    onClick: () => {
      if (enabled(index)) onSelect(index);
    },
  });

  const focusItem = (index: number) => elements.current[index]?.focus();

  return { getItemProps, focusItem };
}
