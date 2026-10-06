"use client";

import { useId, useMemo, useRef, useState } from "react";
import { Button, Field } from "@memaxlabs/ledger";
import type { BriefMemory } from "@/lib/v2/data/brief";
import type { RecordsView } from "../records-view";
import styles from "./brief-edit.module.css";

const SHOWN = 6;

/** Kept memories that match what's typed: an ID ("219", "M-0219") or words. */
export function matchMemories(
  memories: Readonly<Record<string, BriefMemory>>,
  query: string,
  exclude: readonly string[],
): { ref: string; text: string }[] {
  const q = query.trim().toLowerCase();
  const digits = q.replace(/^m-?0*/, "");
  return Object.entries(memories)
    .filter(([ref, m]) => m.kept && !exclude.includes(ref))
    .filter(([ref, m]) => {
      if (!q) return true;
      if (/^m?-?\d+$/.test(q))
        return ref.replace(/^M-0*/, "").startsWith(digits);
      return m.text.toLowerCase().includes(q) || ref.toLowerCase().includes(q);
    })
    .slice(0, SHOWN)
    .map(([ref, m]) => ({ ref, text: m.text }));
}

/**
 * "Cite a source" (BriefEdit.png) on a line of prose: a search over the
 * space's kept memories, by ID or words. ↓↑ choose, ↵ cites, Esc closes.
 */
export function CitePicker({
  view,
  memories,
  cited,
  onCite,
}: {
  view: RecordsView;
  memories: Readonly<Record<string, BriefMemory>>;
  cited: readonly string[];
  onCite: (ref: string) => void;
}) {
  const p = view.l.brief.edit.picker;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const matches = useMemo(
    () => matchMemories(memories, query, cited),
    [memories, query, cited],
  );

  if (!open) {
    return (
      <Button
        variant="quiet"
        size="sm"
        icon="plus"
        onClick={() => {
          setOpen(true);
          requestAnimationFrame(() => input.current?.focus());
        }}
      >
        {view.l.brief.edit.cite}
      </Button>
    );
  }

  const pick = (ref: string | undefined) => {
    if (!ref) return;
    onCite(ref);
    setQuery("");
    setActive(0);
  };

  return (
    <div className={styles.picker}>
      <Field
        ref={input}
        icon="search"
        role="combobox"
        aria-label={p.label}
        aria-expanded={matches.length > 0}
        aria-controls={listId}
        aria-activedescendant={
          matches[active] ? `${listId}-${matches[active]!.ref}` : undefined
        }
        placeholder={p.placeholder}
        value={query}
        onChange={(event) => {
          setQuery(event.currentTarget.value);
          setActive(0);
        }}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setActive((i) => Math.min(i + 1, matches.length - 1));
          } else if (event.key === "ArrowUp") {
            event.preventDefault();
            setActive((i) => Math.max(i - 1, 0));
          } else if (
            event.key === "Enter" &&
            !event.metaKey &&
            !event.ctrlKey
          ) {
            event.preventDefault();
            pick(matches[active]?.ref);
          } else if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            setOpen(false);
          }
        }}
      />
      {matches.length > 0 ? (
        <ul
          id={listId}
          role="listbox"
          aria-label={p.label}
          className={styles.matches}
        >
          {matches.map((m, i) => (
            <li key={m.ref} role="presentation">
              <button
                id={`${listId}-${m.ref}`}
                type="button"
                role="option"
                aria-selected={i === active}
                className={styles.match}
                tabIndex={-1}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => pick(m.ref)}
              >
                <span className={styles.matchRef}>{m.ref}</span>
                <span className={styles.matchText}>{m.text}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="mx-meta">{p.none}</p>
      )}
    </div>
  );
}
