import type { ReactNode, Ref } from "react";
import { Icon } from "../brand/icon";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import type { MarkState, SyncStatus } from "../lib/types";
import { StateMark } from "../memory/state-mark";

export interface SyncTargetProps {
  /** The native context file ("CLAUDE.md", ".cursor/rules/memax.mdc", "ChatGPT project"). */
  path: string;
  /** The tool that reads it ("Claude Code"). */
  tool: string;
  /** `drifted` means a person must pull the edit or overwrite it. */
  status: SyncStatus;
  detail?: ReactNode;
  time?: string;
  /** On drift, "Pull edit" (secondary) before "Overwrite" (danger). */
  action?: ReactNode;
  /** Only the path and a fixed-width status, for side panels. */
  compact?: boolean;
  className?: string;
  ref?: Ref<HTMLDivElement>;
}

const STATUS_MARK: Record<SyncStatus, MarkState> = {
  synced: "kept",
  drifted: "proposed",
  pending: "working",
  off: "off",
};

/** A compile target: a tool's native context file that Memax writes, and whether it still matches. */
export function SyncTarget({
  path,
  tool,
  status,
  detail,
  time,
  action,
  compact,
  className,
  ref,
}: SyncTargetProps) {
  const { strings } = useLedger();
  return (
    <div
      ref={ref}
      className={cx(
        "mx-target",
        `is-${status}`,
        compact && "is-compact",
        className,
      )}
    >
      <Icon name="file" size={16} className="mx-target-icon" />
      <div className="mx-target-id">
        <code className="mx-target-path" title={path}>
          {path}
        </code>
        <span className="mx-target-tool">{tool}</span>
      </div>
      <div className="mx-target-status">
        <StateMark state={STATUS_MARK[status]} label={strings.sync[status]} />
        {detail != null ? (
          <span className="mx-target-detail">{detail}</span>
        ) : null}
      </div>
      <span className="mx-meta mx-target-time">{time ?? ""}</span>
      {action ? <div className="mx-target-action">{action}</div> : <span />}
    </div>
  );
}
