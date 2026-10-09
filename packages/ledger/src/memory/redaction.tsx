import type { Ref } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import { StateMark } from "./state-mark";

export interface RedactionProps {
  /** When it was forgotten, already formatted ("Oct 3"). */
  date: string;
  /** Who forgot it ("Jiahao"). Omit it when the reader asked for it ("at your request"). */
  by?: string;
  id?: string;
  /** What was rewritten ("removed from 4 files and 5 agents"). */
  detail?: string;
  /** The bar's length, as a CSS length. */
  width?: string;
  /** `li` inside a `MemoryList` (the default), `div` on a tombstone page. */
  as?: "li" | "div";
  className?: string;
  ref?: Ref<HTMLElement>;
}

/**
 * A forgotten memory. The bar draws across once and stays, so the forgetting
 * is visible; the words are gone everywhere, and are never shown, even to
 * their author.
 */
export function Redaction({
  date,
  by,
  id,
  detail,
  width = "62%",
  as: Tag = "li",
  className,
  ref,
}: RedactionProps) {
  const { strings } = useLedger();
  const caption = [
    by
      ? format(strings.redaction.by, { date, name: by })
      : format(strings.redaction.atYourRequest, { date }),
    id,
    detail,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Tag
      ref={ref as Ref<HTMLLIElement & HTMLDivElement>}
      className={cx("mx-redaction", className)}
    >
      <div className="mx-row-mark" aria-hidden="true">
        <StateMark state="forgotten" label={false} />
      </div>
      <div className="mx-row-body">
        <span
          className="mx-redaction-bar"
          style={{ width }}
          role="img"
          aria-label={strings.redaction.bar}
        />
        <p className="mx-redaction-cap">{caption}</p>
      </div>
    </Tag>
  );
}
