"use client";

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  AgentStamp,
  Button,
  Field,
  Icon,
  Kbd,
  MemoryList,
  MemoryRow,
  StateMark,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate, spell } from "@/lib/v2/copy";
import type { SearchHit, SearchNote } from "@/lib/v2/data/search";
import { MAX_SEARCH } from "@/lib/v2/data/search";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { useAriaKeys, useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { searchHref } from "@/lib/v2/places";
import {
  authorsOf,
  DEFAULT_OFF,
  KIND_FACETS,
  searchView,
  STATE_FACETS,
  toggle,
  type SearchOff,
} from "@/lib/v2/search-view";
import { EmptyState } from "../../_components/empty-state";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { StatementText } from "../../_components/statement-text";
import { useSpaces } from "../../_lib/data";
import { useOverlays } from "../../_lib/overlays";
import { useSearch } from "../../_lib/search";
import { useNarrow } from "../../_lib/use-narrow";
import { memoryHref } from "../memories/memory-rows";
import { PlaceColumn } from "../place";
import { useRecordsView, type RecordsView } from "../records-view";
import styles from "./search.module.css";

/** How long typing settles before the address (and the search) follows. */
const SETTLE_MS = 250;
/** Rows a panel shows before its "N more". */
const FIRST = 4;

/**
 * Search across spaces (Search.png) at /[space]/search?q=: one field,
 * Ask instead (⌘↵), the facets on the left (kind, state, space, who
 * wrote it) and the matches by kind on the right, each word that matched
 * under the highlighter. The space in the address is where the frame
 * stays; the search reads every space the person belongs to that is on
 * V2. Facets filter what was found without asking again, and their
 * counts are over everything found. Raw notes are off until asked for.
 */
export function SearchPlace() {
  const view = useRecordsView();
  const { space, l } = view;
  const s = l.search;
  const router = useRouter();
  const params = useSearchParams();
  const query = params?.get("q") ?? "";
  const { openCommand } = useOverlays();
  const askKey = useKeycap("search.ask");
  const askAria = useAriaKeys("search.ask");
  const results = useRef<HTMLDivElement>(null);

  // The field follows the address (back, forward, a link here); typing
  // moves the address once it settles.
  const [text, setText] = useState(query);
  const [synced, setSynced] = useState(query);
  if (query !== synced) {
    setSynced(query);
    setText(query);
  }
  useEffect(() => {
    if (text.trim() === query.trim()) return;
    const timer = setTimeout(() => {
      router.replace(searchHref(space.slug, text), { scroll: false });
    }, SETTLE_MS);
    return () => clearTimeout(timer);
  }, [text, query, router, space.slug]);

  const [off, setOff] = useState<SearchOff>(DEFAULT_OFF);
  const search = useSearch(query);
  const spaces = useSpaces().data ?? [space];

  const question = (text.trim() || query).trim();
  const ask = () => openCommand("ask", question);
  // ⌘↵ asks the words instead, from the field too (Search.png).
  useHotkey("search.ask", () => ask());

  const onFieldKeys = (event: KeyboardEvent<HTMLInputElement>) => {
    if (isComposing(event.nativeEvent)) return;
    if (event.key === "Enter" && !event.metaKey && !event.ctrlKey) {
      event.preventDefault();
      router.replace(searchHref(space.slug, text), { scroll: false });
    }
    // ↓ from the field goes to the first match.
    if (event.key === "ArrowDown") {
      const first =
        results.current?.querySelector<HTMLElement>(RESULT_SELECTOR);
      if (first) {
        event.preventDefault();
        first.focus();
      }
    }
  };

  return (
    <PlaceColumn>
      <h1 className="mx-sr">{view.copy.titles.search}</h1>
      <div className={styles.top} role="search">
        <Field
          className={styles.field}
          icon="search"
          type="search"
          aria-label={s.field}
          placeholder={s.placeholder}
          maxLength={MAX_SEARCH}
          value={text}
          // Nothing searched yet: the field is the page.
          autoFocus={query === ""}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onFieldKeys}
        />
        {question ? (
          <button
            type="button"
            className={styles.ask}
            aria-label={interpolate(s.askLabel, {
              space: space.name,
              question,
            })}
            aria-keyshortcuts={askAria}
            onClick={ask}
          >
            <span className={styles.askIcon} aria-hidden="true">
              <Icon name="chat" />
            </span>
            <span className={styles.askLabel}>{s.askInstead}</span>
            <span className={styles.askQuestion}>{question}</span>
            <Kbd aria-hidden="true" className={styles.askKbd}>
              {askKey}
            </Kbd>
          </button>
        ) : null}
      </div>
      {query.trim() === "" ? (
        <EmptyState title={s.start.title} detail={s.start.detail} />
      ) : search.data === undefined ? (
        search.isError ? (
          <PlaceError space={space} onRetry={() => void search.refetch()} />
        ) : (
          <PlaceSkeleton
            title={s.kinds.memories}
            status={s.loadingMeta}
            label={s.loading}
          />
        )
      ) : (
        <Results
          view={view}
          spaces={spaces}
          read={search.data}
          off={off}
          onOff={setOff}
          resultsRef={results}
        />
      )}
    </PlaceColumn>
  );
}

/** What ↓↑ move between: memories' links, decisions and notes. */
const RESULT_SELECTOR = ".mx-row-link, [data-result]";

/** ↓↑ (and Home, End) move between the matches, like Memories' rows. */
function onResultKeys(event: KeyboardEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (!target.matches(RESULT_SELECTOR)) return;
  const all = [
    ...event.currentTarget.querySelectorAll<HTMLElement>(RESULT_SELECTOR),
  ];
  const i = all.indexOf(target);
  let next: HTMLElement | undefined;
  if (event.key === "ArrowDown") next = all[i + 1];
  else if (event.key === "ArrowUp") next = all[i - 1];
  else if (event.key === "Home") next = all[0];
  else if (event.key === "End") next = all[all.length - 1];
  else return;
  event.preventDefault();
  next?.focus();
}

function Results({
  view,
  spaces,
  read,
  off,
  onOff,
  resultsRef,
}: {
  view: RecordsView;
  spaces: SpaceSummary[];
  read: NonNullable<ReturnType<typeof useSearch>["data"]>;
  off: SearchOff;
  onOff: (off: SearchOff) => void;
  resultsRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { l, copy } = view;
  const s = l.search;
  const { query, results } = read;
  const shown = useMemo(() => searchView(results, off), [results, off]);
  const byId = useMemo(() => new Map(spaces.map((x) => [x.id, x])), [spaces]);
  // The spaces a search reads: on V2, with something in them to find
  // (or something found), the most found first, as drawn.
  const found = (id: string) => shown.counts.spaces[id] ?? 0;
  const listed = spaces
    .filter(
      (x) =>
        x.onV2 !== false &&
        ((x.kept ?? 1) + (x.waiting ?? 0) > 0 || found(x.id) > 0),
    )
    .map((x, i) => ({ x, i }))
    .sort((a, b) => found(b.x.id) - found(a.x.id) || a.i - b.i)
    .map(({ x }) => x);
  const authors = authorsOf(results.hits);
  const on = listed.filter((x) => !off.spaces.includes(x.id));
  const where =
    on.length === 1
      ? on[0].name
      : interpolate(s.spaces, { n: spell(copy, on.length) });
  const shownCount =
    shown.memories.length + shown.decisions.length + shown.notes.length;
  const total = results.hits.length + results.notes.length;
  const metaParts = [
    results.more
      ? interpolate(s.best, { n: shownCount, query, where })
      : count(s.resultsOne, s.results, shownCount, { query, where }),
    shown.hiddenNotes > 0
      ? count(s.hiddenOne, s.hidden, shown.hiddenNotes)
      : null,
  ].filter(Boolean);

  // Nothing found at all: no facets to set, just what to try.
  if (total === 0) {
    return (
      <EmptyState
        title={interpolate(s.none.title, { query })}
        detail={s.none.detail}
      />
    );
  }

  return (
    <div className={styles.grid}>
      <aside className={styles.aside} aria-label={s.facets.label}>
        <Facet
          legend={s.facets.kind}
          items={KIND_FACETS.map((k) => ({
            key: k,
            label: s.kinds[k],
            n: shown.counts.kinds[k],
            on: !off.kinds.includes(k),
          }))}
          onToggle={(k) => onOff({ ...off, kinds: toggle(off.kinds, k) })}
        />
        <Facet
          legend={s.facets.state}
          items={STATE_FACETS.map((k) => ({
            key: k,
            label: s.states[k],
            n: shown.counts.states[k],
            on: !off.states.includes(k),
          }))}
          onToggle={(k) => onOff({ ...off, states: toggle(off.states, k) })}
        />
        {listed.length > 0 ? (
          <Facet
            legend={s.facets.space}
            items={listed.map((x) => ({
              key: x.id,
              label: x.name,
              n: found(x.id),
              on: !off.spaces.includes(x.id),
            }))}
            onToggle={(k) => onOff({ ...off, spaces: toggle(off.spaces, k) })}
          />
        ) : null}
        {authors.length > 0 ? (
          <div role="group" aria-labelledby="search-written-by">
            <p className="mx-section-label" id="search-written-by">
              {s.facets.writtenBy}
            </p>
            <div className={styles.stamps}>
              {authors.map(({ key, actor }) => {
                const stamp = view.stamp(actor);
                if (!stamp) return null;
                const pressed = !off.authors.includes(key);
                return (
                  <button
                    key={key}
                    type="button"
                    className={styles.stampToggle}
                    aria-pressed={pressed}
                    aria-label={interpolate(s.facets.author, {
                      name: view.name(actor),
                    })}
                    onClick={() =>
                      onOff({ ...off, authors: toggle(off.authors, key) })
                    }
                  >
                    <AgentStamp {...stamp} />
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}
      </aside>

      <div className={styles.results} ref={resultsRef} onKeyDown={onResultKeys}>
        <p className={`mx-meta ${styles.meta}`} role="status">
          {metaParts.join(" · ")}
        </p>
        {shownCount === 0 ? (
          <EmptyState
            title={s.filtered.title}
            detail={count(s.filtered.detailOne, s.filtered.detail, total, {
              query,
            })}
            action={
              <Button
                variant="secondary"
                size="sm"
                onClick={() => onOff(DEFAULT_OFF)}
              >
                {s.filtered.reset}
              </Button>
            }
          />
        ) : null}
        {shown.memories.length > 0 ? (
          <Panel
            title={s.kinds.memories}
            meta={String(shown.memories.length)}
            items={shown.memories}
            more={s.more}
            fewer={s.fewer}
            render={(hit) => (
              <SearchMemoryRow
                key={`${hit.spaceId}/${hit.item.ref}`}
                view={view}
                hit={hit}
                space={byId.get(hit.spaceId)}
                query={query}
              />
            )}
            list
          />
        ) : null}
        {shown.decisions.length > 0 ? (
          <Panel
            title={s.kinds.decisions}
            meta={decisionsMeta(view, shown.decisions, byId)}
            items={shown.decisions}
            more={s.more}
            fewer={s.fewer}
            render={(hit) => (
              <DecisionRow
                key={`${hit.spaceId}/${hit.item.ref}`}
                view={view}
                hit={hit}
                space={byId.get(hit.spaceId)}
                query={query}
              />
            )}
          />
        ) : null}
        {shown.notes.length > 0 ? (
          <Panel
            title={s.kinds.notes}
            meta={String(shown.notes.length)}
            items={shown.notes}
            more={s.more}
            fewer={s.fewer}
            render={(note) => (
              <NoteRow
                key={`${note.spaceId}/${note.ref ?? note.title}`}
                view={view}
                note={note}
                space={byId.get(note.spaceId)}
                query={query}
              />
            )}
          />
        ) : null}
      </div>
    </div>
  );
}

/** "2 in Memax team" when they're all in one space, else how many. */
function decisionsMeta(
  view: RecordsView,
  decisions: SearchHit[],
  byId: Map<string, SpaceSummary>,
): string {
  const ids = new Set(decisions.map((d) => d.spaceId));
  const only = ids.size === 1 ? byId.get([...ids][0]) : undefined;
  return only
    ? interpolate(view.l.search.decisionsIn, {
        n: decisions.length,
        space: only.name,
      })
    : String(decisions.length);
}

function Facet<K extends string>({
  legend,
  items,
  onToggle,
}: {
  legend: string;
  items: { key: K; label: string; n: number; on: boolean }[];
  onToggle: (key: K) => void;
}) {
  return (
    <fieldset className={styles.fieldset}>
      <legend className={`mx-section-label ${styles.legend}`}>{legend}</legend>
      {items.map((item) => (
        <label key={item.key} className={styles.check}>
          <input
            type="checkbox"
            checked={item.on}
            onChange={() => onToggle(item.key)}
          />
          <span className={styles.checkLabel}>{item.label}</span>
          <span className={`mx-meta ${styles.count}`}>{item.n}</span>
        </label>
      ))}
    </fieldset>
  );
}

function Panel<T>({
  title,
  meta,
  items,
  render,
  more,
  fewer,
  list = false,
}: {
  title: string;
  meta: string;
  items: T[];
  render: (item: T) => React.ReactNode;
  more: string;
  fewer: string;
  /** Memory rows go in a MemoryList (one rule down the receipt rail). */
  list?: boolean;
}) {
  const [all, setAll] = useState(false);
  const rest = items.length - FIRST;
  const visible = all || rest <= 0 ? items : items.slice(0, FIRST);
  const rows = visible.map(render);
  return (
    <section className="mx-panel" aria-label={title}>
      <header className="mx-panel-head">
        <h2 className="mx-panel-title">
          {title} <span className={`mx-meta ${styles.panelMeta}`}>{meta}</span>
        </h2>
      </header>
      {list ? (
        <MemoryList aria-label={title}>{rows}</MemoryList>
      ) : (
        <div className={styles.rows}>{rows}</div>
      )}
      {rest > 0 ? (
        <div className={styles.moreRow}>
          <Button
            variant="quiet"
            size="sm"
            icon="chevron-down"
            aria-expanded={all}
            onClick={() => setAll(!all)}
          >
            {all ? fewer : interpolate(more, { n: rest })}
          </Button>
        </div>
      ) : null}
    </section>
  );
}

/** A memory as Search.png draws it: its words, its receipt, and its space on the rail. */
function SearchMemoryRow({
  view,
  hit,
  space,
  query,
}: {
  view: RecordsView;
  hit: SearchHit;
  space: SpaceSummary | undefined;
  query: string;
}) {
  const { l, rc } = view;
  const narrow = useNarrow();
  const item = hit.item;
  const stamp = item.receipt ? view.stamp(item.receipt.by) : null;
  return (
    <MemoryRow
      stacked={narrow}
      state={item.state === "forgotten" ? "kept" : item.state}
      unconfirmed={item.state === "proposed" || item.state === "conflict"}
      {...(stamp ?? {})}
      action={
        item.receipt ? rc.verbs[item.receipt.action] : l.records.verbs.kept
      }
      time={item.receipt ? view.time(item.receipt.at) : undefined}
      id={item.ref}
      space={space?.name}
      href={space ? memoryHref(space.slug, item.ref) : undefined}
    >
      <StatementText text={item.statement} query={query} />
    </MemoryRow>
  );
}

/** A decision as the Decisions panel draws it: when, what, and whether it's open or in force. */
function DecisionRow({
  view,
  hit,
  space,
  query,
}: {
  view: RecordsView;
  hit: SearchHit;
  space: SpaceSummary | undefined;
  query: string;
}) {
  const s = view.l.search;
  const state = hit.item.state;
  const standing =
    state === "proposed" || state === "conflict" ? (
      <StateMark state={state} label={s.proposed} />
    ) : state === "stale" ? (
      <StateMark state="stale" label={s.stale} />
    ) : hit.decision === "open" ? (
      <StateMark state="conflict" label={s.open} />
    ) : (
      <span className="mx-meta">{s.inForce}</span>
    );
  const body = (
    <>
      <span className={styles.date}>
        {formatShortDate(new Date(hit.at), view.timeZone, view.locale)}
      </span>
      <span className={styles.decisionText}>
        <StatementText text={hit.item.statement} query={query} />
      </span>
      {standing}
    </>
  );
  return space ? (
    <Link
      className={styles.decision}
      href={memoryHref(space.slug, hit.item.ref)}
      data-result=""
      title={`${hit.item.ref} · ${space.name}`}
    >
      {body}
    </Link>
  ) : (
    <div className={styles.decision}>{body}</div>
  );
}

/** One of the person's raw notes: its N-, its words, and its space. Notes have no page. */
function NoteRow({
  view,
  note,
  space,
  query,
}: {
  view: RecordsView;
  note: SearchNote;
  space: SpaceSummary | undefined;
  query: string;
}) {
  return (
    <div className={styles.note} tabIndex={0} data-result="">
      <span className={styles.date}>{note.ref ?? ""}</span>
      <span className={styles.noteText}>
        <StatementText text={note.excerpt} query={query} />
      </span>
      <span className="mx-meta">
        {[
          space?.name,
          formatShortDate(new Date(note.at), view.timeZone, view.locale),
        ]
          .filter(Boolean)
          .join(" · ")}
      </span>
    </div>
  );
}
