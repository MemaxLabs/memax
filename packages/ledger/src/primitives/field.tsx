import {
  useId,
  type InputHTMLAttributes,
  type ReactNode,
  type Ref,
} from "react";
import { Icon, type IconName } from "../brand/icon";
import { cx } from "../lib/cx";

export interface FieldProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "size"
> {
  /** Sentence case, never ending in a colon. Without a label, pass `aria-label`. */
  label?: ReactNode;
  hint?: ReactNode;
  /** Replaces the hint and turns the edge vermilion. */
  error?: ReactNode;
  icon?: IconName;
  /** For paths, IDs and commands. */
  mono?: boolean;
  /** Applies to the wrapper; everything else goes to the `<input>`. */
  className?: string;
  ref?: Ref<HTMLInputElement>;
}

/** A labelled single-line input. Hint and error are announced as its description. */
export function Field({
  label,
  hint,
  error,
  icon,
  mono,
  className,
  id,
  ref,
  "aria-describedby": describedBy,
  ...rest
}: FieldProps) {
  const autoId = useId();
  const inputId = id ?? `mx-f-${autoId}`;
  const message = error ?? hint;
  const messageId = `${inputId}-msg`;
  return (
    <div className={cx("mx-field", error != null && "is-error", className)}>
      {label != null ? (
        <label className="mx-field-label" htmlFor={inputId}>
          {label}
        </label>
      ) : null}
      <div className="mx-field-box">
        {icon ? <Icon name={icon} size={16} /> : null}
        <input
          ref={ref}
          id={inputId}
          className={cx("mx-field-input", mono && "is-mono")}
          aria-invalid={error != null ? true : undefined}
          aria-describedby={
            cx(describedBy, message != null && messageId) || undefined
          }
          {...rest}
        />
      </div>
      {message != null ? (
        <p
          id={messageId}
          className={cx("mx-field-msg", error != null && "is-error")}
        >
          {message}
        </p>
      ) : null}
    </div>
  );
}
