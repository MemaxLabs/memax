"use client";

import { useEffect, useRef, type DragEvent, type KeyboardEvent } from "react";
import { Button, Cite, Icon, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { BriefMemory } from "@/lib/v2/data/brief";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import { CitePicker } from "./cite-picker";
import type { DraftError, DraftItem, EditorAction } from "./editor-state";
import styles from "./brief-edit.module.css";

/** The six-dot grip of BriefEdit.png. */
function Grip() {
  return (
    <svg
      width={10}
      height={14}
      viewBox="0 0 10 14"
      aria-hidden="true"
      fill="currentColor"
    >
      <circle cx="2" cy="2" r="1.4" />
      <circle cx="8" cy="2" r="1.4" />
      <circle cx="2" cy="7" r="1.4" />
      <circle cx="8" cy="7" r="1.4" />
      <circle cx="2" cy="12" r="1.4" />
      <circle cx="8" cy="12" r="1.4" />
    </svg>
  );
}

export interface RowProps {
  view: RecordsView;
  item: DraftItem;
  section: string;
  editing: boolean;
  /** "Moved here from Conventions", for a fact regrouped while editing. */
  movedFrom: string | null;
  error: DraftError | null;
  /** The memories prose may cite, for the picker. */
  memories: Readonly<Record<string, BriefMemory>>;
  dispatch: (action: EditorAction) => void;
  /** Drag and drop: where a fact dragged over this row would land. */
  dropBefore: boolean;
  onDragStart: (key: string) => void;
  onDragOverRow: (key: string, before: boolean) => void;
  onDrop: () => void;
  keepKey: string;
  moveKeys: string;
  /** The Brief's [n] for each cited memory, as the page numbers them. */
  numbers: ReadonlyMap<string, number>;
  /** A new empty fact asks for focus once. */
  autoFocus?: boolean;
}

/**
 * One fact while editing the Brief (BriefEdit.png): focusable, with its
 * grip (drag, or ⌥↑ ⌥↓), and its tools on hover and focus: Edit, Cite a
 * source (prose) and Remove from the Brief. Editing opens the words in
 * place with what they cite; Esc cancels, ⌘↵ keeps. A new fact is an
 * input that's kept as the person's on Done.
 */
export function EditorRow(props: RowProps) {
  return props.item.kind === "new" ? (
    <NewFact {...props} />
  ) : (
    <Fact {...props} />
  );
}

function Fact({
  view,
  item,
  editing,
  movedFrom,
  error,
  memories,
  dispatch,
  dropBefore,
  onDragStart,
  onDragOverRow,
  onDrop,
  keepKey,
  moveKeys,
  numbers,
}: RowProps) {
  const { l } = view;
  const e = l.brief.edit;
  const area = useRef<HTMLTextAreaElement>(null);
  const label = item.ref ?? "";
  const prose = item.kind === "prose";

  useEffect(() => {
    if (editing) {
      const el = area.current;
      el?.focus();
      el?.setSelectionRange(el.value.length, el.value.length);
    }
  }, [editing]);

  const onRowKey = (event: KeyboardEvent<HTMLLIElement>) => {
    if (event.target !== event.currentTarget || editing) return;
    if (event.key === "Enter") {
      event.preventDefault();
      dispatch({ type: "edit", key: item.key });
    }
  };
  const onAreaKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      dispatch({ type: "cancel" });
      focusRow(item.key);
    } else if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      // ⌘↵ keeps this fact's words; Done is the next ⌘↵.
      event.preventDefault();
      dispatch({ type: "keep" });
      focusRow(item.key);
    }
  };
  const drag = (event: DragEvent<HTMLLIElement>) => {
    event.preventDefault();
    const box = event.currentTarget.getBoundingClientRect();
    onDragOverRow(item.key, event.clientY < box.top + box.height / 2);
  };

  return (
    <>
      <li
        className={`${styles.fact} ${editing ? styles.editing : ""} ${
          dropBefore ? styles.dropBefore : ""
        } ${item.state === "stale" ? styles.stale : ""} ${
          item.state === "conflict" ? styles.conflict : ""
        }`}
        tabIndex={editing ? -1 : 0}
        data-fact={item.key}
        aria-keyshortcuts={moveKeys}
        onKeyDown={onRowKey}
        onDragOver={drag}
        onDrop={(event) => {
          event.preventDefault();
          onDrop();
        }}
      >
        <span
          className={styles.grip}
          draggable
          aria-hidden="true"
          title={interpolate(e.moveHint, { keys: moveKeys })}
          onDragStart={(event) => {
            event.dataTransfer.effectAllowed = "move";
            event.dataTransfer.setData("text/plain", item.key);
            onDragStart(item.key);
          }}
        >
          <Grip />
        </span>
        <span className={styles.mark}>
          {item.state !== "kept" ? (
            <StateMark state={item.state} label={false} />
          ) : null}
        </span>
        {editing ? (
          <div
            className={styles.editor}
            onBlur={(event) => {
              // Leaving the fact keeps its words; the cite picker is inside it.
              if (!event.currentTarget.contains(event.relatedTarget)) {
                dispatch({ type: "keep" });
              }
            }}
          >
            <textarea
              ref={area}
              className={styles.textarea}
              aria-label={
                prose ? e.proseLabel : interpolate(e.factLabel, { ref: label })
              }
              value={item.text}
              rows={2}
              onChange={(event) =>
                dispatch({
                  type: "text",
                  key: item.key,
                  text: event.currentTarget.value,
                })
              }
              onKeyDown={onAreaKey}
            />
            <Cites
              view={view}
              item={item}
              memories={memories}
              dispatch={dispatch}
              keepKey={keepKey}
            />
          </div>
        ) : (
          <p className={styles.text}>
            <StatementText text={item.text} />
            {(prose ? item.cites : item.ref ? [item.ref] : []).map((ref) => {
              const n = numbers.get(ref);
              return n ? <Cite key={ref} n={n} title={ref} /> : null;
            })}
          </p>
        )}
        {editing ? null : (
          <span
            className={styles.tools}
            role="group"
            aria-label={
              prose ? e.toolsProse : interpolate(e.tools, { ref: label })
            }
          >
            <Button
              variant="quiet"
              size="sm"
              icon="pencil"
              aria-label={e.editFact}
              onClick={() => dispatch({ type: "edit", key: item.key })}
            />
            {prose ? (
              <Button
                variant="quiet"
                size="sm"
                icon="link"
                aria-label={e.cite}
                onClick={() => dispatch({ type: "edit", key: item.key })}
              />
            ) : null}
            <Button
              variant="quiet"
              size="sm"
              icon="x"
              aria-label={e.remove}
              onClick={() => {
                const next = nextFocus(item.key);
                dispatch({ type: "remove", key: item.key });
                next?.focus();
              }}
            />
          </span>
        )}
      </li>
      {movedFrom ? (
        <p className={`mx-meta ${styles.note}`}>
          {interpolate(e.movedFrom, { section: movedFrom })}
        </p>
      ) : null}
      {error ? <p className={styles.error}>{e[error]}</p> : null}
    </>
  );
}

/** The words a fact rests on while it's edited: its sources and itself, or prose's cites with a picker. */
function Cites({
  view,
  item,
  memories,
  dispatch,
  keepKey,
}: {
  view: RecordsView;
  item: DraftItem;
  memories: Readonly<Record<string, BriefMemory>>;
  dispatch: (action: EditorAction) => void;
  keepKey: string;
}) {
  const e = view.l.brief.edit;
  const prose = item.kind === "prose";
  const chips = prose ? item.cites : item.ref ? [item.ref] : [];
  return (
    <div className={styles.cites}>
      <span className="mx-meta">{e.cites}</span>
      {chips.map((ref) => (
        <span key={ref} className={styles.chip}>
          <span className="mx-code">{ref}</span>
          {prose ? (
            <button
              type="button"
              className={styles.chipX}
              aria-label={interpolate(e.removeCite, { ref })}
              onClick={() => dispatch({ type: "uncite", key: item.key, ref })}
            >
              <Icon name="x" size={12} />
            </button>
          ) : null}
        </span>
      ))}
      {prose ? (
        <CitePicker
          view={view}
          memories={memories}
          cited={item.cites}
          onCite={(ref) => dispatch({ type: "cite", key: item.key, ref })}
        />
      ) : null}
      <span className={styles.spacer} />
      <span className="mx-meta">
        {interpolate(e.keepHint, { key: keepKey })}
      </span>
    </div>
  );
}

function NewFact({ view, item, section, dispatch, autoFocus }: RowProps) {
  const e = view.l.brief.edit;
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (autoFocus) input.current?.focus();
  }, [autoFocus]);
  return (
    <li className={`${styles.fact} ${styles.newFact}`} data-fact={item.key}>
      <span className={styles.grip} aria-hidden="true">
        <Icon name="plus" size={14} />
      </span>
      <span className={styles.mark} />
      <div className={styles.newWrap}>
        <input
          ref={input}
          className={styles.newInput}
          aria-label={interpolate(e.newFact, { section })}
          placeholder={e.newFactPlaceholder}
          value={item.text}
          onChange={(event) =>
            dispatch({
              type: "text",
              key: item.key,
              text: event.currentTarget.value,
            })
          }
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.metaKey && !event.ctrlKey) {
              event.preventDefault();
              if (item.text.trim()) {
                dispatch({
                  type: "add",
                  section: sectionKeyOf(event.currentTarget),
                  after: item.key,
                });
              }
            } else if (
              (event.key === "Escape" || event.key === "Backspace") &&
              !item.text
            ) {
              event.preventDefault();
              dispatch({ type: "remove", key: item.key });
            }
          }}
        />
        <span className="mx-meta">{e.newFactHint}</span>
      </div>
    </li>
  );
}

/** The section a row's element sits in (its list's data-section). */
function sectionKeyOf(el: HTMLElement): string {
  return el.closest<HTMLElement>("[data-section]")?.dataset.section ?? "";
}

function focusRow(key: string) {
  requestAnimationFrame(() => {
    [...document.querySelectorAll<HTMLElement>("[data-fact]")]
      .find((row) => row.dataset.fact === key)
      ?.focus();
  });
}

/** The fact to focus after one is taken out: the next, else the one before. */
function nextFocus(key: string): HTMLElement | null {
  const rows = [...document.querySelectorAll<HTMLElement>("[data-fact]")];
  const i = rows.findIndex((row) => row.dataset.fact === key);
  return rows[i + 1] ?? rows[i - 1] ?? null;
}

export { focusRow };
