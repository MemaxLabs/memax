"use client";

import { useMemo } from "react";
import { useRouter } from "next/navigation";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { briefEyebrow } from "@/lib/v2/brief-copy";
import { joinSentences } from "@/lib/v2/copy";
import { factCount, numberSources, type BriefView } from "@/lib/v2/data/brief";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useBrief, useTargets } from "../../_lib/compile";
import { useRecordsView, type RecordsView } from "../records-view";
import { BriefEmpty } from "./brief-empty";
import { BriefLedger } from "./brief-ledger";
import { BriefSide } from "./brief-side";
import { useCompile } from "./use-compile";
import styles from "./brief.module.css";

export const briefHref = (space: string, rest = "") =>
  `${placeHref(space, "brief")}${rest}`;

/**
 * The Brief (Brief.png, epic 1.5) at /[space]/brief: the living document
 * every agent reads, its facts in Brief order with their receipts in the
 * ruled margin, and the side panels: where it compiles to (⌘⇧S compiles
 * now), a hand edit to pull or overwrite, reads and sources. E edits it.
 */
export function BriefPlace() {
  const view = useRecordsView();
  const { space, l, copy } = view;
  const brief = useBrief(space);

  if (brief.data === undefined) {
    if (brief.isError) {
      return <PlaceError space={space} onRetry={() => void brief.refetch()} />;
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.app.titles.brief}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, {
            place: l.app.titles.brief,
          })}
        />
      </div>
    );
  }
  if (brief.data === null) return <BriefEmpty view={view} />;
  return <BriefPage view={view} brief={brief.data} />;
}

function BriefPage({ view, brief }: { view: RecordsView; brief: BriefView }) {
  const { space, l, copy, now, timeZone, locale } = view;
  const b = l.brief;
  const router = useRouter();
  const targets = useTargets(space);
  const compile = useCompile(space);
  const editKey = useKeycap("memory.edit");
  const canEdit = space.role !== "viewer";
  const numbers = useMemo(
    () => numberSources(brief.sections, brief.memories),
    [brief.sections, brief.memories],
  );

  const runCompile = () => void compile.run(targets.data ?? []);
  useHotkey("memory.compile", runCompile, {
    enabled: canEdit && Boolean(targets.data?.length),
  });
  useHotkey("memory.edit", () => router.push(briefHref(space.slug, "/edit")), {
    enabled: canEdit,
  });

  const eyebrow = briefEyebrow(copy, b, {
    space: space.name,
    by: brief.by,
    at: brief.at,
    facts: factCount(brief),
    name: view.name,
    when: { now, timeZone, locale },
  });
  const lede = brief.summary
    ? joinSentences([brief.summary, b.page.ledeRest], locale)
    : space.kind === "project"
      ? copy.brief.ledeProject
      : copy.brief.ledeSpace;

  return (
    <div className={`mx-page ${styles.page}`}>
      <div className={styles.grid}>
        <article className={styles.article}>
          <PageHeader
            className={styles.head}
            eyebrow={eyebrow}
            title={brief.title}
            lede={lede}
            actions={
              <>
                <Button
                  variant="quiet"
                  size="sm"
                  icon="clock"
                  href={briefHref(space.slug, "/history")}
                >
                  {b.page.history}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  icon="pencil"
                  kbd={editKey}
                  href={canEdit ? briefHref(space.slug, "/edit") : undefined}
                  disabled={!canEdit}
                  disabledReason={b.page.editViewer}
                >
                  {b.page.edit}
                </Button>
              </>
            }
          />
          <BriefLedger view={view} brief={brief} numbers={numbers} />
        </article>
        <BriefSide
          view={view}
          brief={brief}
          targets={targets.data}
          numbers={numbers}
          onCompile={runCompile}
          compiling={compile.pending}
        />
      </div>
    </div>
  );
}
