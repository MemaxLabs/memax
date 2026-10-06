import { Fragment } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";

export interface DiffPart {
  op: "eq" | "add" | "del";
  text: string;
}

/** A word-level diff (whitespace kept as its own tokens), by longest common subsequence. */
export function wordDiff(before: string, after: string): DiffPart[] {
  const a = before.split(/(\s+)/);
  const b = after.split(/(\s+)/);
  const n = a.length;
  const m = b.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () =>
    new Array<number>(m + 1).fill(0),
  );
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i]![j] =
        a[i] === b[j]
          ? dp[i + 1]![j + 1]! + 1
          : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const out: DiffPart[] = [];
  const push = (op: DiffPart["op"], text: string) => {
    const last = out[out.length - 1];
    if (last && last.op === op) last.text += text;
    else out.push({ op, text });
  };
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      push("eq", a[i]!);
      i++;
      j++;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      push("del", a[i]!);
      i++;
    } else {
      push("add", b[j]!);
      j++;
    }
  }
  while (i < n) push("del", a[i++]!);
  while (j < m) push("add", b[j++]!);
  return out;
}

export interface DiffProps {
  before: string;
  after: string;
  className?: string;
}

/**
 * A word diff in the serif: removed words struck in vermilion, added words
 * underlined in ink (never green: they aren't kept yet). It inherits size and
 * style, so inside a proposal it is italic. Assistive technology hears
 * "Removed:" and "Added:" before each change.
 */
export function Diff({ before, after, className }: DiffProps) {
  const { strings } = useLedger();
  return (
    <span className={cx("mx-diff", className)}>
      {wordDiff(before, after).map((part, i) => {
        if (part.op === "eq") return <Fragment key={i}>{part.text}</Fragment>;
        const Tag = part.op === "add" ? "ins" : "del";
        return (
          <Tag key={i} className={`mx-diff-${part.op}`}>
            <span className="mx-sr">
              {part.op === "add"
                ? strings.diff.added
                : strings.diff.removed}{" "}
            </span>
            {part.text}
          </Tag>
        );
      })}
    </span>
  );
}
