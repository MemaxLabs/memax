"use client";

import { useEffect } from "react";
import Link from "next/link";
import { Icon, Seal, Terminal, type TerminalLine } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count, formatShortDate } from "@/lib/v2/copy";
import { targetStatus, type TargetView } from "@/lib/v2/data/targets";
import type { ImportView } from "@/lib/v2/data/imports";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { gitStatusLines } from "./transcript";
import { useSource, useViewer } from "../(app)/_lib/data";
import { useTargets } from "../(app)/_lib/compile";
import { TargetStatus } from "../(app)/_components/target-row";
import { OnboardingPage } from "./frame";
import { useSetupImport, useSetupSpace } from "./queries";
import styles from "./compile-done.module.css";

type Copy = ReturnType<typeof useLocale>["t"]["ledger"]["onboarding"]["done"];

/** While the CLI or the daemon still writes a file. */
const WAIT_FOR_FILES_MS = 2000;

/** What the person did in the import: memories kept, disagreements settled. */
export function importOutcome(view: ImportView | null): {
  kept: number;
  settled: number;
} {
  if (!view) return { kept: 0, settled: 0 };
  return {
    kept: view.memories.filter(
      (m) => m.outcome === "proposed" && m.state === "kept",
    ).length,
    settled: view.conflicts.filter((c) => c.state === "settled").length,
  };
}

/**
 * CompileDone (CompileDone.png, set up · done) at /setup/done: the space's
 * files as compiled and written (each target's state, the same words as
 * the Brief), what `git status` will show for them, and the three next
 * steps. The page follows the files until each is written.
 */
export function CompileDoneScreen() {
  const { t } = useLocale();
  const frame = t.ledger.onboarding.frame;
  const setup = useSetupSpace();
  const space = setup.space ?? null;
  return (
    <OnboardingPage meta={frame.done} width="narrow">
      {space ? (
        <Done space={space} />
      ) : setup.loading ? (
        <p className="mx-sr" role="status">
          {frame.loading}
        </p>
      ) : (
        <section className="mx-panel">
          <p className={styles.empty}>
            {t.ledger.onboarding.cleanup.none.detail}
          </p>
        </section>
      )}
    </OnboardingPage>
  );
}

function Done({ space }: { space: SpaceSummary }) {
  const { t, locale } = useLocale();
  const copy = t.ledger.onboarding.done;
  const source = useSource();
  const viewer = useViewer();
  const targets = useTargets(space);
  const { view } = useSetupImport(space);
  const list = (targets.data ?? []).filter((x) => x.syncState !== "off");
  const writing = list.some(
    (x) => x.syncState === "pending_delivery" || x.syncState === "compiling",
  );
  const { refetch } = targets;
  useEffect(() => {
    if (!writing) return;
    const timer = setInterval(() => void refetch(), WAIT_FOR_FILES_MS);
    return () => clearInterval(timer);
  }, [writing, refetch]);

  const written = list.filter((x) => {
    const s = targetStatus(x).kind;
    return s === "in_sync" || s === "reads" || s === "live";
  });
  const files = list.filter((x) => x.kind !== "chatgpt").length;
  const { kept, settled } = importOutcome(view.data ?? null);
  const compiled = written.length > 0;
  const title = compiled
    ? interpolate(files === 1 ? copy.titleOne : copy.title, {
        space: space.slug,
        n: files,
      })
    : interpolate(copy.notYet, { space: space.slug });
  const keptText = count(copy.memory, copy.memories, kept);
  const settledText = count(copy.conflict, copy.conflicts, settled);
  const lede = !compiled
    ? copy.ledeNotYet
    : kept > 0 && settled > 0
      ? interpolate(copy.lede, { kept: keptText, settled: settledText })
      : kept > 0
        ? interpolate(copy.ledeKept, { kept: keptText })
        : copy.ledeNone;

  const readBefore = (view.data?.summary.files ?? []).map((f) => f.path);
  const status = gitStatusLines(list, readBefore);
  const lines: TerminalLine[] = [{ kind: "cmd", text: copy.gitStatus }];
  for (const line of status) lines.push({ kind: "out", text: line });
  lines.push({ kind: "blank" }, { kind: "dim", text: copy.commit });
  const repo = space.repository?.split("/").at(-1) ?? space.slug;
  const date = formatShortDate(source.now(), viewer?.timeZone ?? "UTC", locale);
  const base = `/${encodeURIComponent(space.slug)}`;

  return (
    <div className={styles.stack}>
      <div className={styles.hero}>
        {compiled ? <Seal date={date} id={space.slug} size={120} /> : null}
        <div className={styles.heroText}>
          <h1 className={styles.title}>{title}</h1>
          <p className={styles.lede}>{lede}</p>
        </div>
      </div>
      <div className={styles.pair}>
        <section className="mx-panel" aria-labelledby="written-title">
          <header className="mx-panel-head">
            <h2 className="mx-panel-title" id="written-title">
              {copy.written}
            </h2>
            <span className="mx-meta">{copy.byCli}</span>
          </header>
          {list.length === 0 ? (
            <p className={styles.empty}>{copy.noTargets}</p>
          ) : (
            list.map((target) => <FileRow key={target.id} target={target} />)
          )}
        </section>
        {status.length > 0 ? (
          <Terminal
            title={interpolate(copy.terminalTitle, { repo })}
            lines={lines}
          />
        ) : null}
      </div>
      <nav className={styles.next} aria-label={copy.next.label}>
        <Link
          className={`${styles.card} ${styles.first}`}
          href={`${base}/today`}
        >
          <b>{copy.next.today}</b>
          <span>{copy.next.todayDetail}</span>
        </Link>
        <Link className={styles.card} href={`${base}/agents?overlay=connect`}>
          <b>{copy.next.cloud}</b>
          <span>{copy.next.cloudDetail}</span>
        </Link>
        <div
          className={`${styles.card} ${styles.later}`}
          aria-disabled="true"
          title={copy.next.teamLater}
        >
          <b>{copy.next.team}</b>
          <span>{copy.next.teamDetail}</span>
          <span className="mx-meta">{copy.next.teamLater}</span>
        </div>
      </nav>
    </div>
  );
}

function FileRow({ target }: { target: TargetView }) {
  return (
    <div className={styles.file}>
      <Icon name="file" size={16} />
      <code>{target.path ?? target.label}</code>
      <span className={styles.status}>
        <TargetStatus target={target} />
      </span>
    </div>
  );
}

export type { Copy as CompileDoneCopy };
