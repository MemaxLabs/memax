import type { InputHTMLAttributes, KeyboardEvent, ReactNode, Ref } from "react";
import { Icon } from "../brand/icon";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { useControllableState } from "../lib/use-controllable-state";
import { Kbd } from "../primitives/kbd";
import { Segmented } from "../primitives/segmented";

export type CommandMode = "ask" | "remember";

export interface CommandBarProps {
  /** Controlled text. */
  query?: string;
  /** Uncontrolled starting text. */
  defaultQuery?: string;
  onQueryChange?: (query: string) => void;
  /** Controlled mode. */
  mode?: CommandMode;
  defaultMode?: CommandMode;
  onModeChange?: (mode: CommandMode) => void;
  /** Enter in the field (never while an IME is composing). */
  onSubmit?: (query: string, mode: CommandMode) => void;
  /** Defaults to the locale's "Ask your context, or remember something…". */
  placeholder?: string;
  /** Tab in the field switches Ask / Remember, as the footer says. On by default. */
  tabSwitchesMode?: boolean;
  /**
   * The result: `.mx-section-label` headings and a `.mx-answer` in the serif,
   * with Cite and Highlight inside it. Sources follow, then actions.
   */
  children?: ReactNode;
  /** The key legend. */
  footer?: boolean;
  /**
   * The keycap for "keep answer as memory" in the legend. ⌘↵ as drawn;
   * pass the platform's own ("Ctrl+Enter") off a Mac.
   */
  keepShortcut?: string;
  /** Extra attributes for the field (combobox wiring, aria-controls…). */
  inputProps?: Omit<
    InputHTMLAttributes<HTMLInputElement>,
    "value" | "defaultValue" | "onChange"
  >;
  inputRef?: Ref<HTMLInputElement>;
  className?: string;
  ref?: Ref<HTMLDivElement>;
}

/**
 * ⌘K: one field to ask your context or remember something; answers cite their
 * receipts. Render it inside `CommandDialog`, which supplies the dialog, the
 * scrim, focus trapping and Escape.
 */
export function CommandBar({
  query,
  defaultQuery = "",
  onQueryChange,
  mode,
  defaultMode = "ask",
  onModeChange,
  onSubmit,
  placeholder,
  tabSwitchesMode = true,
  children,
  footer = true,
  keepShortcut = "⌘↵",
  inputProps,
  inputRef,
  className,
  ref,
}: CommandBarProps) {
  const { strings } = useLedger();
  const c = strings.command;
  const [text, setText] = useControllableState(
    query,
    defaultQuery,
    onQueryChange,
  );
  const [current, setMode] = useControllableState(
    mode,
    defaultMode,
    onModeChange,
  );

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    inputProps?.onKeyDown?.(event);
    if (event.defaultPrevented || event.nativeEvent.isComposing) return;
    const plain =
      !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey;
    if (event.key === "Tab" && plain && tabSwitchesMode) {
      event.preventDefault();
      setMode(current === "ask" ? "remember" : "ask");
    } else if (event.key === "Enter" && plain && onSubmit) {
      event.preventDefault();
      onSubmit(text, current);
    }
  };

  return (
    <div ref={ref} className={cx("mx-cmd", className)}>
      <div className="mx-cmd-input">
        <Icon name={current === "ask" ? "search" : "plus"} size={18} />
        <input
          aria-label={current === "ask" ? c.ask : c.remember}
          placeholder={placeholder ?? c.placeholder}
          {...inputProps}
          ref={inputRef}
          className={cx("mx-cmd-field", inputProps?.className)}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <Segmented<CommandMode>
          size="sm"
          label={c.mode}
          value={current}
          onChange={setMode}
          options={[
            { value: "ask", label: c.ask },
            { value: "remember", label: c.remember },
          ]}
        />
      </div>
      {children ? <div className="mx-cmd-body">{children}</div> : null}
      {footer ? (
        <div className="mx-cmd-foot">
          <span>
            <Kbd>↵</Kbd> {c.open}
          </span>
          <span>
            <Kbd>{keepShortcut}</Kbd> {c.keepAnswer}
          </span>
          <span>
            <Kbd>Tab</Kbd> {c.switchMode}
          </span>
          <span className="mx-cmd-foot-end">
            <Kbd>Esc</Kbd>
            <span className="mx-sr">{c.close}</span>
          </span>
        </div>
      ) : null}
    </div>
  );
}
