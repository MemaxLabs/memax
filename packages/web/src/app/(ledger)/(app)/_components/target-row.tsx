"use client";

import Link from "next/link";
import { Icon, StateMark } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import {
  targetName,
  targetStatus,
  TARGET_READERS,
  type TargetView,
} from "@/lib/v2/data/targets";
import { useAgentName } from "../_lib/frame-copy";
import styles from "./target-row.module.css";

/** Where a target's page is. */
export function targetHref(space: string, target: TargetView): string {
  return `/${encodeURIComponent(space)}/brief/targets/${encodeURIComponent(target.slug)}`;
}

/**
 * A target's state as a mark and its word (D2/D3): in sync, compiling,
 * waiting for the CLI, drifted, held for Review (a pulled hand edit
 * whose proposals wait), off; ChatGPT is "live over connector" (its
 * connector, never "in sync"), and a scoped tool with nothing scoped
 * "reads AGENTS.md". The last two describe delivery, not a state, so
 * they're words without a state mark.
 */
export function TargetStatus({
  target,
  edits = "word",
}: {
  target: TargetView;
  /** How a drifted file reads: "Drifted" (Brief.png) or "1 local edit" (Main.png). */
  edits?: "word" | "count";
}) {
  const { t } = useLocale();
  const s = t.ledger.brief.status;
  const status = targetStatus(target);
  switch (status.kind) {
    case "in_sync":
      return <StateMark state="kept" label={s.inSync} />;
    case "compiling":
      return <StateMark state="working" label={s.compiling} />;
    case "pending":
      return <StateMark state="working" label={s.pending} />;
    case "drifted":
      return (
        <StateMark
          state="proposed"
          label={
            edits === "count"
              ? count(s.localEditsOne, s.localEdits, status.edits)
              : s.drifted
          }
        />
      );
    case "held":
      return (
        <StateMark
          state="proposed"
          label={
            edits === "count"
              ? count(s.holdingOne, s.holding, status.proposals)
              : s.held
          }
        />
      );
    case "off":
      return <StateMark state="off" label={s.off} />;
    case "live":
      return <span className={styles.delivery}>{s.live}</span>;
    case "reads":
      return (
        <span className={styles.delivery}>
          {interpolate(s.reads, { file: status.file })}
        </span>
      );
  }
}

/** What a row names a target by: its file, or the tool when it writes none (Cursor reads AGENTS.md). */
export function useTargetName() {
  const agentName = useAgentName();
  return (target: TargetView) =>
    targetStatus(target).kind === "reads"
      ? agentName(TARGET_READERS[target.kind][0] ?? target.kind)
      : targetName(target);
}

/**
 * One compiled file in a list, linking to its page: the Brief's
 * "Compiled to", TargetPreview's "Other targets", Review's "Keeping
 * recompiles" and a memory's "Reaches" (`compact`, after SyncTarget),
 * or Today's "Compiled context" (`plain`: no icon, a wider status).
 */
export function TargetRow({
  space,
  target,
  variant = "compact",
}: {
  space: string;
  target: TargetView;
  variant?: "compact" | "plain";
}) {
  const name = useTargetName();
  const label = name(target);
  const tool = targetStatus(target).kind === "reads";
  return (
    <Link
      href={targetHref(space, target)}
      className={`${variant === "plain" ? styles.plain : `mx-target is-compact ${styles.compact}`} ${styles.row}`}
    >
      {variant === "compact" ? (
        <Icon name="file" size={16} className="mx-target-icon" />
      ) : null}
      <span className={styles.name}>
        <code
          className={`${variant === "compact" ? "mx-target-path" : styles.path} ${tool ? styles.tool : ""}`}
          title={label}
        >
          {label}
        </code>
      </span>
      <span className={`mx-target-status ${styles.status}`}>
        <TargetStatus
          target={target}
          edits={variant === "plain" ? "count" : "word"}
        />
      </span>
    </Link>
  );
}
