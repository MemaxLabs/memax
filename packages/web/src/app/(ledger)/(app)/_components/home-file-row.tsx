"use client";

import Link from "next/link";
import { Icon, StateMark } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import {
  homeFileStatus,
  primaryTarget,
  type HomeFileGroup,
} from "@/lib/v2/data/home-files";
import type { MachineView } from "@/lib/v2/data/machines";
import { homeStatusText, homeToolName } from "@/lib/v2/machines-copy";
import { useSource, useViewer } from "../_lib/data";
import { useAgentName } from "../_lib/frame-copy";
import { targetHref } from "./target-row";
import styles from "./home-file-row.module.css";

/** The clock and zone a row words times in, wherever it's drawn (a place, or the first run). */
export function useClock() {
  const source = useSource();
  const viewer = useViewer();
  const { locale } = useLocale();
  return {
    now: source.now(),
    timeZone:
      viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
    locale,
  };
}

/**
 * A Personal space's home file, once across the machines it's written
 * on (personal-layer design §8): the file, the agent it's for, and how
 * it stands on every machine ("In sync on 2 machines", "Hand edit on
 * work-mbp"); when it's behind, which machine and why, on its own line.
 * It opens one machine's copy: the one that needs the person, else the
 * first. `compact` sits in side panels, `plain` in Today's "Compiled
 * context", like TargetRow's.
 */
export function HomeFileRow({
  space,
  group,
  machines,
  variant = "compact",
}: {
  space: string;
  group: HomeFileGroup;
  /** The space's machines; without them a file behind just waits. */
  machines: readonly MachineView[] | null | undefined;
  variant?: "compact" | "plain";
}) {
  const { t } = useLocale();
  const m = t.ledger.machines;
  const agentName = useAgentName();
  const when = useClock();
  const status = homeStatusText(
    m,
    homeFileStatus(group, machines, when.now),
    when,
  );
  const target = primaryTarget(group, machines, when.now);
  return (
    <Link
      href={targetHref(space, target)}
      className={`${styles.row} ${variant === "plain" ? styles.plain : styles.compact}`}
    >
      {variant === "compact" ? (
        <Icon name="file" size={16} className={styles.icon} />
      ) : null}
      <span className={styles.body}>
        <code className={styles.path} title={group.label}>
          {group.label}
        </code>
        <span className={styles.meta}>
          <span className={styles.tool}>
            {homeToolName(m, group, agentName)}
          </span>
          <StateMark state={status.state} label={status.label} />
        </span>
        {status.detail ? (
          <span className={styles.detail}>{status.detail}</span>
        ) : null}
      </span>
    </Link>
  );
}
