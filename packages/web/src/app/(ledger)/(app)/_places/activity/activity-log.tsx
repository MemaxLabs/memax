"use client";

import { Fragment, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { Button, Segmented } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import {
  ACTIVITY_FILTERS,
  matchesFilter,
  type ActivityEntry,
  type ActivityFilter,
} from "@/lib/v2/data/activity";
import {
  dayKey,
  dayLabel,
  foldUndoable,
  zoneLabel,
  type Names,
} from "@/lib/v2/activity/sentence";
import { isComposing } from "@/lib/v2/keymap/keymap";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { FOLD_UNDO_WINDOW_MS } from "@/lib/v2/data/undo";
import { useUndo } from "../../_lib/undo";
import { ActivityRow } from "./activity-row";
import styles from "./activity.module.css";

interface Group {
  key: string;
  entries: ActivityEntry[];
}

function groupByDay(entries: ActivityEntry[], timeZone: string): Group[] {
  const groups: Group[] = [];
  for (const entry of entries) {
    const key = dayKey(entry.at, timeZone);
    const last = groups[groups.length - 1];
    if (last?.key === key) last.entries.push(entry);
    else groups.push({ key, entries: [entry] });
  }
  return groups;
}

/**
 * The log: the board's filters and zone line, then every receipt by
 * day, newest first, and older pages on request. One tab stop: ↓ and ↑
 * (Home, End) move between rows, and Enter opens what a row points at.
 * The keys are the list's own (a composite widget, like Segmented's
 * arrows), not app shortcuts, so they aren't in the keymap registry.
 */
export function ActivityLog({
  space,
  entries,
  names,
  now,
  timeZone,
  hasMore,
  loadingMore,
  moreFailed,
  onMore,
  filter,
  onFilter,
}: {
  space: SpaceSummary;
  entries: ActivityEntry[];
  names: Names;
  now: Date;
  timeZone: string;
  hasMore: boolean;
  loadingMore: boolean;
  moreFailed: boolean;
  onMore: () => void;
  filter: ActivityFilter;
  onFilter: (filter: ActivityFilter) => void;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.activity;
  const router = useRouter();
  const undo = useUndo();
  // Anyone who may keep can undo one of the judge's folds, for 14 days.
  const canUnfold = space.role !== "viewer";
  const shown = entries.filter((e) => matchesFilter(e, filter));
  const groups = groupByDay(shown, timeZone);
  const [active, setActive] = useState(0);
  const rows = useRef<(HTMLLIElement | null)[]>([]);
  const focusRow = (i: number) => {
    const to = Math.max(0, Math.min(shown.length - 1, i));
    rows.current[to]?.focus();
  };
  const onKeyDown = (
    index: number,
    event: KeyboardEvent<HTMLLIElement>,
    href: string | null,
  ) => {
    if (event.target !== event.currentTarget) return;
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    if (isComposing(event.nativeEvent)) return;
    const moves: Partial<Record<string, number>> = {
      ArrowDown: index + 1,
      ArrowUp: index - 1,
      Home: 0,
      End: shown.length - 1,
    };
    if (event.key in moves) {
      event.preventDefault();
      focusRow(moves[event.key]!);
    } else if (event.key === "Enter" && href) {
      event.preventDefault();
      router.push(href);
    }
  };

  let index = -1;
  const tabStop = Math.min(active, Math.max(0, shown.length - 1));
  const filterName = copy.filters[filter];
  return (
    <div className={styles.log}>
      <div className={styles.bar}>
        <Segmented<ActivityFilter>
          size="sm"
          label={copy.filterLabel}
          value={filter}
          onChange={(next) => {
            setActive(0);
            onFilter(next);
          }}
          options={ACTIVITY_FILTERS.map((value) => ({
            value,
            label: copy.filters[value],
          }))}
        />
        <span className={styles.grow} />
        <span className="mx-meta">{zoneLabel(copy, timeZone, locale)}</span>
      </div>

      <section
        className="mx-panel"
        aria-label={interpolate(copy.listLabel, { space: space.name })}
      >
        {groups.length === 0 ? (
          <div className={styles.none}>
            <p className={styles.noneTitle}>
              {interpolate(copy.emptyFilter.title, {
                filter: locale === "zh" ? filterName : filterName.toLowerCase(),
              })}
            </p>
            {filter === "reads" ? (
              <p className={styles.noneDetail}>{copy.emptyFilter.reads}</p>
            ) : null}
            {hasMore ? (
              <p className={styles.noneDetail}>{copy.emptyFilter.more}</p>
            ) : null}
          </div>
        ) : (
          groups.map((group) => {
            const label = dayLabel(copy, group.key, now, timeZone, locale);
            return (
              <Fragment key={group.key}>
                <h2 className={styles.day} id={`day-${group.key}`}>
                  {label}
                </h2>
                <ol
                  className={styles.rows}
                  aria-labelledby={`day-${group.key}`}
                >
                  {group.entries.map((entry) => {
                    index += 1;
                    const i = index;
                    return (
                      <ActivityRow
                        key={entry.id}
                        ref={(el) => {
                          rows.current[i] = el;
                        }}
                        entry={entry}
                        space={space.slug}
                        names={names}
                        timeZone={timeZone}
                        focusable={i === tabStop}
                        onFocus={() => setActive(i)}
                        onKeyDown={(event, href) => onKeyDown(i, event, href)}
                        onUnfold={
                          canUnfold &&
                          foldUndoable(entry, entries, now, FOLD_UNDO_WINDOW_MS)
                            ? () =>
                                void undo.unfold(
                                  space,
                                  entry.object.ref,
                                  entry.id,
                                )
                            : undefined
                        }
                      />
                    );
                  })}
                </ol>
              </Fragment>
            );
          })
        )}
      </section>

      {hasMore ? (
        <div className={styles.more}>
          <Button
            variant="quiet"
            size="sm"
            icon="chevron-down"
            pending={loadingMore}
            onClick={onMore}
          >
            {loadingMore ? copy.loadingMore : copy.more}
          </Button>
          {moreFailed ? (
            <span className={styles.moreFailed} role="alert">
              {copy.moreFailed}
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
