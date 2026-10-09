"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Button, Field, MemoryList, Segmented } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { MemoryFilter, MemoryListItem } from "@/lib/v2/data/memories";
import type { Section } from "@/lib/v2/data/types";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useMemoriesList } from "../../_lib/records";
import { NotYetButton } from "../place";
import type { RecordsView } from "../records-view";
import { MemoryListRow, onRowKeys } from "./memory-rows";
import styles from "./memories.module.css";

export const FILTERS: readonly MemoryFilter[] = [
  "all",
  "waiting",
  "stale",
  "merged",
  "forgotten",
];
const SECTIONS: readonly Section[] = [
  "decisions",
  "conventions",
  "preferences",
  "open_question",
];
/** The SDK's page size, so "Show 50 more" says what a click loads. */
const PAGE = 50;

/** Search covers what's loaded: /v2 has no search yet (plan §5.11 brings it). */
function matches(item: MemoryListItem, query: string): boolean {
  const q = query.normalize("NFKC").toLowerCase().trim();
  if (!q) return true;
  return [item.statement, item.ref, item.source ?? ""].some((field) =>
    field.normalize("NFKC").toLowerCase().includes(q),
  );
}

/**
 * The record under Memories' header: the toolbar, then one panel with
 * a header row per section, the rows and the paging foot.
 */
export function Record({
  view,
  filter,
  filterLabel,
  onFilter,
}: {
  view: RecordsView;
  filter: MemoryFilter;
  filterLabel: (filter: MemoryFilter) => string;
  onFilter: (filter: MemoryFilter) => void;
}) {
  const { l, rc, space } = view;
  const m = l.memory.list;
  const [query, setQuery] = useState("");
  const list = useMemoriesList(space, filter);
  const pages = list.data?.pages;
  const items = useMemo(() => (pages ?? []).flatMap((p) => p.items), [pages]);
  const shown = items.filter((item) => matches(item, query));
  const first = pages?.[0];
  const total = first?.total ?? null;
  const groups = SECTIONS.map((section) => ({
    section,
    items: shown.filter((item) => item.section === section),
  })).filter((g) => g.items.length > 0);

  // After "Show more", focus lands on the first row that arrived.
  const rows = useRef(new Map<string, HTMLElement>());
  const before = useRef<Set<string> | null>(null);
  useEffect(() => {
    const seen = before.current;
    if (!seen || list.isFetchingNextPage) return;
    before.current = null;
    const fresh = items.find((item) => !seen.has(item.ref));
    const row = fresh ? rows.current.get(fresh.ref) : undefined;
    row?.querySelector<HTMLElement>(".mx-row-link")?.focus();
  }, [items, list.isFetchingNextPage]);

  const loadMore = () => {
    before.current = new Set(items.map((item) => item.ref));
    void list.fetchNextPage();
  };
  const remaining =
    total === null ? PAGE : Math.min(PAGE, Math.max(total - items.length, 0));

  return (
    <>
      <div className={styles.toolbar}>
        <Field
          className={styles.search}
          icon="search"
          type="search"
          placeholder={m.search}
          aria-label={m.searchLabel}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <Segmented<MemoryFilter>
          size="sm"
          label={m.filterLabel}
          value={filter}
          onChange={onFilter}
          options={FILTERS.map((value) => ({
            value,
            label: filterLabel(value),
          }))}
        />
        <span className={styles.spacer} />
        <NotYetButton variant="quiet" size="sm" icon="chevron-down">
          {m.sort}
        </NotYetButton>
      </div>
      {list.data === undefined ? (
        list.isError ? (
          <PlaceError space={space} onRetry={() => void list.refetch()} />
        ) : (
          <PlaceSkeleton
            title={view.copy.memories.title}
            status={view.copy.empty.loadingMeta}
            label={m.loading}
          />
        )
      ) : (
        <section
          className="mx-panel"
          onKeyDown={onRowKeys}
          aria-label={view.copy.memories.title}
        >
          {groups.map(({ section, items: sectionItems }) => {
            const count = first?.sectionCounts?.[section];
            const name = rc.sections[section];
            return (
              <div key={section}>
                <div className={styles.sectionHead}>
                  <h2 className={styles.sectionTitle}>{name}</h2>
                  {count !== undefined ? (
                    <span className="mx-meta">{count}</span>
                  ) : null}
                </div>
                <MemoryList aria-label={name}>
                  {sectionItems.map((item) => (
                    <MemoryListRow
                      key={item.ref}
                      view={view}
                      item={item}
                      rowRef={(el) => {
                        if (el) rows.current.set(item.ref, el);
                        else rows.current.delete(item.ref);
                      }}
                    />
                  ))}
                </MemoryList>
              </div>
            );
          })}
          {shown.length === 0 ? (
            <div className={styles.none}>
              <span>
                {query
                  ? interpolate(list.hasNextPage ? m.noMatchMore : m.noMatch, {
                      query,
                    })
                  : m.emptyFilter}
              </span>
              {query ? (
                <Button variant="quiet" size="sm" onClick={() => setQuery("")}>
                  {m.clear}
                </Button>
              ) : null}
            </div>
          ) : null}
          <div className={styles.foot}>
            <span className="mx-meta">
              {total === null
                ? interpolate(m.showingLoaded, { n: shown.length })
                : interpolate(m.showing, { n: shown.length, total })}
            </span>
            {list.hasNextPage ? (
              <Button
                variant="quiet"
                size="sm"
                icon="chevron-down"
                pending={list.isFetchingNextPage}
                onClick={loadMore}
              >
                {interpolate(m.more, { n: remaining })}
              </Button>
            ) : null}
          </div>
        </section>
      )}
    </>
  );
}
