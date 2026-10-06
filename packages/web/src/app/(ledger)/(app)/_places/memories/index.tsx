"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  Button,
  Field,
  MemoryList,
  PageHeader,
  Segmented,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { memoriesLede } from "@/lib/v2/copy";
import type { MemoryFilter, MemoryListItem } from "@/lib/v2/data/memories";
import type { Section } from "@/lib/v2/data/types";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { EmptyState } from "../../_components/empty-state";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useOverlays } from "../../_lib/overlays";
import { useMemoriesList } from "../../_lib/records";
import { NotYetButton, PlaceColumn } from "../place";
import { useRecordsView, type RecordsView } from "../records-view";
import { MemoryListRow, onRowKeys } from "./memory-rows";
import styles from "./memories.module.css";

const FILTERS: readonly MemoryFilter[] = [
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
 * Memories (Memories.png): the whole record by section, a state filter
 * (?filter=), search over what's loaded, "Show 50 more" through the
 * cursor, and rows a keyboard can walk (↓↑). Remember is N.
 */
export function MemoriesPlace() {
  const view = useRecordsView();
  const { copy, l, overview, locale } = view;
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const asked = params.get("filter") as MemoryFilter | null;
  const filter = asked && FILTERS.includes(asked) ? asked : "all";
  const { openCommand } = useOverlays();
  const rememberKey = useKeycap("memories.remember");
  const remember = () => openCommand("remember");
  useHotkey("memories.remember", remember);

  const counts: Record<MemoryFilter, number | null> = {
    all:
      overview?.memories.kept != null
        ? overview.memories.kept +
          overview.waiting +
          (overview.memories.forgotten ?? 0)
        : null,
    waiting: overview?.waiting ?? null,
    stale: overview?.reviewFilters?.stale ?? null,
    // Memories.png draws Merged without a count.
    merged: null,
    forgotten: overview?.memories.forgotten ?? null,
  };
  const f = l.memory.list.filters;
  const label = (key: MemoryFilter) => {
    const n = counts[key];
    switch (key) {
      case "merged":
        return f.merged;
      case "all":
        return n === null ? f.allBare : interpolate(f.all, { n });
      case "waiting":
        return n === null ? f.waitingBare : interpolate(f.waiting, { n });
      case "stale":
        return n === null ? f.staleBare : interpolate(f.stale, { n });
      case "forgotten":
        return n === null ? f.forgottenBare : interpolate(f.forgotten, { n });
    }
  };

  const empty = overview !== undefined && !overview.memories.any;
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.head}
        eyebrow={view.eyebrow}
        title={copy.memories.title}
        lede={
          overview
            ? (memoriesLede(copy, locale, overview) ?? undefined)
            : undefined
        }
        actions={
          <>
            <NotYetButton variant="secondary" icon="file">
              {copy.memories.export}
            </NotYetButton>
            <Button
              variant="primary"
              icon="plus"
              kbd={rememberKey}
              onClick={remember}
            >
              {copy.memories.remember}
            </Button>
          </>
        }
      />
      {empty ? (
        <EmptyState
          title={copy.empty.memories.title}
          detail={copy.empty.memories.detail}
          action={
            <Button variant="secondary" icon="plus" onClick={remember}>
              {copy.empty.remember}
            </Button>
          }
        />
      ) : (
        <Record
          view={view}
          filter={filter}
          filterLabel={label}
          onFilter={(next) =>
            router.replace(
              `${pathname}${next === "all" ? "" : `?filter=${next}`}`,
              {
                scroll: false,
              },
            )
          }
        />
      )}
    </PlaceColumn>
  );
}

function Record({
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
