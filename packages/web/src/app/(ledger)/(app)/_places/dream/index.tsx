"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Button,
  DreamCard,
  MemoryList,
  MemoryRow,
  PageHeader,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatClock } from "@/lib/v2/copy";
import {
  needsYouOf,
  standing,
  type DreamActionView,
  type DreamEditionView,
  type DreamEditionsPage,
  type NoteAuthors,
} from "@/lib/v2/data/dream";
import type { MemoryListItem } from "@/lib/v2/data/memories";
import {
  becameText,
  conflictNote,
  dreamFailureText,
  factNote,
  foldNote,
  headingText,
  issueLine,
  ledeText,
  nextText,
  runFailureText,
  staleNote,
  undoAllText,
  type DreamWords,
} from "@/lib/v2/dream-copy";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { SETTINGS_HREF, editionHref, placeHref } from "@/lib/v2/places";
import { dreamDate } from "@/lib/v2/today-copy";
import { EmptyState } from "../../_components/empty-state";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { StatementText } from "../../_components/statement-text";
import { useToast } from "../../_components/toasts";
import {
  useDreamCommands,
  useEdition,
  useEditions,
  type DreamOutcome,
} from "../../_lib/dream";
import { MemoryListRow, memoryHref } from "../memories/memory-rows";
import { useRecordsView, type RecordsView } from "../records-view";
import styles from "./dream.module.css";

/** Faded rows shown before "N more" (DreamEdition.png shows two). */
const FADED_SHOWN = 2;

/**
 * A Dream edition (DreamEdition.png) at /[space]/dream (the latest) and
 * /[space]/dream/[n]: what Dream read overnight, each change it made
 * with its Undo, what needs a person, and the editions before it.
 */
export function DreamPlace({ editionRef }: { editionRef: string }) {
  const view = useRecordsView();
  const { space, l, copy } = view;
  const edition = useEdition(space, editionRef);
  const editions = useEditions(space);

  if (edition.data === undefined) {
    if (edition.isError) {
      return (
        <PlaceError space={space} onRetry={() => void edition.refetch()} />
      );
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.dream.title}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, { place: l.dream.title })}
        />
      </div>
    );
  }
  if (edition.data === null) {
    return (
      <NoEdition view={view} editionRef={editionRef} page={editions.data} />
    );
  }
  return <Edition view={view} edition={edition.data} page={editions.data} />;
}

function words(view: RecordsView): DreamWords {
  return {
    dc: view.l.dream,
    app: view.copy,
    rc: view.rc,
    locale: view.locale,
    timeZone: view.timeZone,
    agentName: view.agentName,
  };
}

/** No edition yet (or none by that number). */
function NoEdition({
  view,
  editionRef,
  page,
}: {
  view: RecordsView;
  editionRef: string;
  page: DreamEditionsPage | undefined;
}) {
  const { space, l } = view;
  const dc = l.dream;
  const w = words(view);
  const latest = editionRef === "latest";
  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader eyebrow={view.eyebrow} title={dc.title} />
      <EmptyState
        title={
          latest
            ? dc.empty.title
            : interpolate(dc.empty.notFound, { ref: `D-${editionRef}` })
        }
        detail={latest ? dc.empty.detail : undefined}
        meta={
          latest && page?.schedule
            ? nextText(w, page.schedule.nextAt, dc.empty.next)
            : undefined
        }
        action={
          !latest && page?.items.length ? (
            <Button variant="secondary" href={editionHref(space.slug)}>
              {dc.empty.latest}
            </Button>
          ) : undefined
        }
      />
    </div>
  );
}

function Edition({
  view,
  edition: e,
  page,
}: {
  view: RecordsView;
  edition: DreamEditionView;
  page: DreamEditionsPage | undefined;
}) {
  const { space, l, timeZone, locale } = view;
  const dc = l.dream;
  const w = words(view);
  const router = useRouter();
  const toast = useToast();
  const commands = useDreamCommands(space);
  const resolveKey = useKeycap("dream.resolve");

  const needsYou = needsYouOf(e);
  const conflicts = needsYou.filter((m) => m.state === "conflict");
  const compareHref = (ref: string) =>
    `${placeHref(space.slug, "review")}/${encodeURIComponent(ref)}/compare`;
  const resolveHref = conflicts[0] ? compareHref(conflicts[0].ref) : null;
  useHotkey(
    "dream.resolve",
    () => {
      if (resolveHref) router.push(resolveHref);
    },
    { enabled: resolveHref !== null },
  );

  const report = (
    outcome: DreamOutcome<unknown>,
    done: string,
    ref: string,
  ) => {
    toast(
      outcome.ok
        ? { text: done }
        : { text: dreamFailureText(w, outcome.failure, ref) },
    );
  };
  const undo = async (a: DreamActionView) => {
    const ref = a.memory?.ref ?? a.brief?.ref ?? e.ref;
    const outcome = await commands.undo(a);
    report(
      outcome,
      a.kind === "brief"
        ? dc.done.undoneBrief
        : a.kind === "fade"
          ? interpolate(dc.done.restored, { ref })
          : interpolate(dc.done.undone, { ref }),
      ref,
    );
  };
  /** A faded memory: undo the fade, or restore it once the fade can't be undone. */
  const restore = async (a: DreamActionView) => {
    const ref = a.memory?.ref ?? e.ref;
    let outcome: DreamOutcome<unknown> = await commands.undo(a);
    if (
      !outcome.ok &&
      outcome.failure.kind === "undo-refused" &&
      outcome.failure.reason === "window_passed"
    ) {
      outcome = await commands.restore(ref, a.version);
    }
    report(outcome, interpolate(dc.done.restored, { ref }), ref);
  };
  const undoAll = async (kind: DreamActionView["kind"]) => {
    const outcome = await commands.undoAll(e.ref, kind);
    if (!outcome.ok) {
      toast({ text: dreamFailureText(w, outcome.failure, e.ref) });
      return;
    }
    toast({
      text: undoAllText(
        w,
        kind === "fade",
        outcome.value.undone,
        outcome.value.refused.length,
      ),
    });
  };
  const runNow = async () => {
    const outcome = await commands.runNow();
    toast({
      text: outcome.ok ? dc.side.queued : runFailureText(w, outcome.failure),
    });
  };

  const folds = standing(e, "fold");
  const facts = standing(e, "propose");
  const repeats = standing(e, "dedupe");
  const faded = standing(e, "fade").filter((a) => a.memory?.state === "faded");
  const briefs = standing(e, "brief");
  const waitingFacts = facts.filter((a) => a.memory?.state === "proposed");
  const flagFor = new Map(
    e.actions
      .filter(
        (a) =>
          !a.undone &&
          (a.kind === "conflict" || a.kind === "stale") &&
          a.memory,
      )
      .map((a) => [a.memory!.ref, a]),
  );
  const withFor = (ref: string) =>
    flagFor.get(ref)?.related ??
    e.surfaced.find((s) => s.memory.ref === ref)?.with ??
    null;

  const undoButton = (a: DreamActionView, label: string = dc.folded.undo) => {
    if (!a.undoable) return undefined;
    return (
      <Button
        variant="quiet"
        size="sm"
        disabled={commands.pending !== null}
        onClick={() => void undo(a)}
      >
        {label}
      </Button>
    );
  };

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        eyebrow={interpolate(dc.eyebrow, { space: space.name, n: e.n })}
        title={headingText(w, e)}
        lede={ledeText(w, e)}
        actions={
          <>
            <Button
              variant="secondary"
              icon="settings"
              href={`${SETTINGS_HREF}#dream`}
            >
              {dc.settings}
            </Button>
            {resolveHref ? (
              <Button variant="primary" kbd={resolveKey} href={resolveHref}>
                {conflicts.length === 1 ? dc.resolve : dc.resolveMany}
              </Button>
            ) : null}
          </>
        }
      />
      <DreamCard
        headingLevel={2}
        issue={e.n}
        date={dreamDate(e.finishedAt, timeZone, locale)}
        time={formatClock(e.finishedAt, timeZone, locale)}
        duration={interpolate(l.today.dream.seconds, { n: e.seconds })}
        notes={e.notes}
        facts={e.factIds.length}
        noteIds={e.noteIds}
        factIds={e.factIds}
      />
      <div className={styles.grid}>
        <div className={styles.main}>
          {folds.length > 0 ? (
            <Section
              id="dream-folded"
              title={dc.folded.title}
              meta={count(
                dc.notesOne,
                dc.notes,
                folds.reduce((n, a) => n + a.notes.length, 0),
              )}
              action={
                <Button
                  variant="quiet"
                  size="sm"
                  disabled={commands.pending !== null}
                  onClick={() => void undoAll("fold")}
                >
                  {folds.length === 1
                    ? dc.folded.undo
                    : folds.length === 2
                      ? dc.folded.undoBoth
                      : dc.folded.undoAll}
                </Button>
              }
            >
              {folds.map((a) => (
                <DreamRow
                  key={a.id}
                  view={view}
                  item={a.memory!}
                  note={foldNote(w, a)}
                  actions={undoButton(a)}
                />
              ))}
            </Section>
          ) : null}

          {facts.length > 0 ? (
            <Section
              id="dream-facts"
              title={dc.facts.title}
              meta={interpolate(dc.facts.meta, {
                n: facts.length,
                notes: count(
                  dc.notesOne,
                  dc.notes,
                  facts.reduce((n, a) => n + a.notes.length, 0),
                ),
              })}
              action={
                waitingFacts.length > 0 ? (
                  <Button
                    variant="quiet"
                    size="sm"
                    icon="arrow-right"
                    href={placeHref(space.slug, "review")}
                  >
                    {interpolate(dc.facts.review, { n: waitingFacts.length })}
                  </Button>
                ) : undefined
              }
            >
              {facts.map((a) => (
                <DreamRow
                  key={a.id}
                  view={view}
                  item={a.memory!}
                  note={factNote(w, a)}
                  space={a.space}
                  actions={
                    a.memory?.state === "proposed"
                      ? undoButton(a, dc.facts.withdraw)
                      : undefined
                  }
                />
              ))}
            </Section>
          ) : null}

          {needsYou.length > 0 ? (
            <Section
              id="dream-needs-you"
              title={dc.needsYou.title}
              count={needsYou.length}
            >
              {needsYou.map((m) => {
                const flag = flagFor.get(m.ref);
                return (
                  <DreamRow
                    key={m.ref}
                    view={view}
                    item={m}
                    note={
                      m.state === "conflict"
                        ? conflictNote(w, withFor(m.ref))
                        : staleNote(w, m)
                    }
                    href={
                      m.state === "conflict" ? compareHref(m.ref) : undefined
                    }
                    actions={
                      flag ? undoButton(flag, dc.needsYou.unflag) : undefined
                    }
                  />
                );
              })}
            </Section>
          ) : null}

          {repeats.length > 0 ? (
            <Section id="dream-repeats" title={dc.repeats.title}>
              {repeats.map((a) => (
                <DreamRow
                  key={a.id}
                  view={view}
                  item={a.memory!}
                  note={
                    a.related
                      ? interpolate(dc.repeats.note, { ref: a.related.ref })
                      : dc.repeats.noteBare
                  }
                  actions={undoButton(a)}
                />
              ))}
            </Section>
          ) : null}

          {faded.length > 0 ? (
            <FadedSection
              view={view}
              actions={faded}
              busy={commands.pending !== null}
              onRestore={(a) => void restore(a)}
              onRestoreAll={() => void undoAll("fade")}
            />
          ) : null}

          {briefs.length > 0 ? (
            <Section id="dream-brief" title={dc.brief.title} list={false}>
              {briefs.map((a) => (
                <div key={a.id} className={styles.briefLine}>
                  <p className={styles.briefText}>
                    {count(dc.brief.lineOne, dc.brief.line, a.brief?.ops ?? 1, {
                      ref: a.brief?.ref ?? "",
                    })}
                  </p>
                  {a.undoable ? (
                    <Button
                      variant="quiet"
                      size="sm"
                      disabled={commands.pending !== null}
                      onClick={() => void undo(a)}
                    >
                      {dc.brief.undo}
                    </Button>
                  ) : null}
                </div>
              ))}
            </Section>
          ) : null}
        </div>

        <aside className={styles.side}>
          <LastNight view={view} edition={e} />
          <Earlier view={view} edition={e} page={page} />
          <p className={`mx-meta ${styles.foot}`}>
            {[
              page?.schedule ? nextText(w, page.schedule.nextAt) : null,
              dc.side.never,
            ]
              .filter(Boolean)
              .join(" ")}
          </p>
          {space.role === "owner" ? (
            <div>
              <Button
                variant="quiet"
                size="sm"
                disabled={commands.pending !== null}
                onClick={() => void runNow()}
              >
                {dc.side.runNow}
              </Button>
            </div>
          ) : null}
        </aside>
      </div>
    </div>
  );
}

/** A panel of the edition: a title, its meta or count, and one action. */
function Section({
  id,
  title,
  meta,
  count: n,
  action,
  list = true,
  children,
}: {
  id: string;
  title: string;
  meta?: string;
  count?: number;
  action?: ReactNode;
  /** Rows of memories (the default), or prose. */
  list?: boolean;
  children: ReactNode;
}) {
  return (
    <section className="mx-panel" aria-labelledby={id}>
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id={id}>
          {title}
          {meta ? (
            <span className={`mx-meta ${styles.titleMeta}`}>{meta}</span>
          ) : null}
          {n !== undefined ? (
            <span className="mx-count is-pending">{n}</span>
          ) : null}
        </h2>
        {action}
      </header>
      {list ? <MemoryList>{children}</MemoryList> : children}
    </section>
  );
}

/** One memory of the edition, as Memories draws it, with Dream's line under it. */
function DreamRow({
  view,
  item,
  note,
  href,
  space,
  compact,
  actions,
}: {
  view: RecordsView;
  item: MemoryListItem;
  note: string;
  href?: string;
  space?: string;
  compact?: boolean;
  actions?: ReactNode;
}) {
  const { l, rc, space: current } = view;
  if (item.forgotten) return <MemoryListRow view={view} item={item} />;
  const stamp = item.receipt ? view.stamp(item.receipt.by) : null;
  return (
    <MemoryRow
      state={item.state === "forgotten" ? "kept" : item.state}
      unconfirmed={item.state === "proposed" || item.state === "conflict"}
      {...(stamp ?? {})}
      action={
        item.receipt ? rc.verbs[item.receipt.action] : l.records.verbs.kept
      }
      time={item.receipt ? view.time(item.receipt.at) : undefined}
      id={item.ref}
      space={space}
      compact={compact}
      note={compact || !note ? undefined : note}
      actions={actions}
      href={href ?? memoryHref(current.slug, item.ref)}
    >
      <StatementText text={item.statement} />
    </MemoryRow>
  );
}

function FadedSection({
  view,
  actions,
  busy,
  onRestore,
  onRestoreAll,
}: {
  view: RecordsView;
  actions: DreamActionView[];
  busy: boolean;
  onRestore: (a: DreamActionView) => void;
  onRestoreAll: () => void;
}) {
  const dc = view.l.dream;
  const [open, setOpen] = useState(false);
  const shown = open ? actions : actions.slice(0, FADED_SHOWN);
  const more = actions.length - shown.length;
  return (
    <Section
      id="dream-faded"
      title={dc.faded.title}
      meta={interpolate(dc.faded.meta, { n: actions.length })}
      action={
        <Button
          variant="quiet"
          size="sm"
          disabled={busy}
          onClick={onRestoreAll}
        >
          {dc.faded.restoreAll}
        </Button>
      }
    >
      {shown.map((a) => (
        <DreamRow
          key={a.id}
          view={view}
          item={a.memory!}
          note=""
          compact
          actions={
            <Button
              variant="quiet"
              size="sm"
              disabled={busy}
              onClick={() => onRestore(a)}
            >
              {dc.faded.restore}
            </Button>
          }
        />
      ))}
      {more > 0 || open ? (
        <li className={styles.more}>
          <Button
            variant="quiet"
            size="sm"
            icon={open ? undefined : "chevron-down"}
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            {open ? dc.faded.fewer : interpolate(dc.faded.more, { n: more })}
          </Button>
        </li>
      ) : null}
    </Section>
  );
}

/** "Last night": notes read, by whom, the facts, and the run. */
function LastNight({
  view,
  edition: e,
}: {
  view: RecordsView;
  edition: DreamEditionView;
}) {
  const { l, timeZone, locale, agentName, copy } = view;
  const s = l.dream.side;
  const label = (a: NoteAuthors) => {
    if (a.kind === "agent" && a.agent) return agentName(a.agent);
    if (a.kind === "chat") {
      if (!a.sessions) return s.chats;
      const text = count(l.dream.who.chatsOne, l.dream.who.chats, a.sessions, {
        n: copy.numbers[a.sessions] ?? String(a.sessions),
      });
      return text.charAt(0).toUpperCase() + text.slice(1);
    }
    return s.you;
  };
  return (
    <section className="mx-panel" aria-labelledby="dream-last-night">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="dream-last-night">
          {e.trigger === "manual" ? s.thisRun : s.lastNight}
        </h2>
      </header>
      <dl className={styles.stats}>
        <dt className={styles.statLabel}>{s.notesRead}</dt>
        <dd className={styles.statValue}>{e.notes}</dd>
        {e.notesBy.map((a, i) => (
          <Stat key={i} sub label={label(a)} value={a.count} />
        ))}
        <dt className={styles.statLabel}>{s.facts}</dt>
        <dd className={styles.statValue}>{e.factIds.length}</dd>
        {e.undone > 0 ? (
          <>
            <dt className={styles.statLabel}>{s.undone}</dt>
            <dd className={styles.statValue}>{e.undone}</dd>
          </>
        ) : null}
        <dt className={styles.statLabel}>{s.run}</dt>
        <dd className={styles.statValue}>
          {interpolate(s.runValue, {
            time: formatClock(e.finishedAt, timeZone, locale),
            n: e.seconds,
          })}
        </dd>
      </dl>
    </section>
  );
}

function Stat({
  label,
  value,
  sub = false,
}: {
  label: string;
  value: number;
  sub?: boolean;
}) {
  return (
    <>
      <dt className={sub ? styles.statSub : styles.statLabel}>{label}</dt>
      <dd className={sub ? styles.statSubValue : styles.statValue}>{value}</dd>
    </>
  );
}

/** "Earlier editions": the three before this one. */
function Earlier({
  view,
  edition: e,
  page,
}: {
  view: RecordsView;
  edition: DreamEditionView;
  page: DreamEditionsPage | undefined;
}) {
  const { space, l } = view;
  const w = words(view);
  const s = l.dream.side;
  const earlier = (page?.items ?? []).filter((x) => x.n < e.n).slice(0, 3);
  const later = (page?.items ?? []).filter((x) => x.n > e.n);
  return (
    <section className="mx-panel" aria-labelledby="dream-earlier">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="dream-earlier">
          {s.earlier}
        </h2>
      </header>
      {earlier.length === 0 && later.length === 0 ? (
        <p className={`mx-meta ${styles.first}`}>{s.first}</p>
      ) : null}
      {[...later.slice(-1), ...earlier].map((x) => (
        <Link
          key={x.ref}
          className={styles.issue}
          href={editionHref(space.slug, x.n)}
        >
          <span className="mx-meta">{issueLine(w, x.n, x.slot)}</span>
          <span className={styles.issueLine}>
            {becameText(w, x.notes, x.facts)}
          </span>
        </Link>
      ))}
    </section>
  );
}
