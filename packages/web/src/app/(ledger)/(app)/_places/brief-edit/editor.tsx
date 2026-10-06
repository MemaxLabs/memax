"use client";

import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Icon } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { numberSources, type BriefView } from "@/lib/v2/data/brief";
import type { TargetView } from "@/lib/v2/data/targets";
import { useHotkey, useKeycap, useKeycaps } from "@/lib/v2/keymap/react";
import { briefHref } from "../brief";
import type { RecordsView } from "../records-view";
import { ChangesPanel } from "./changes-panel";
import {
  changesOf,
  draftFacts,
  draftOf,
  editorReducer,
  errorsOf,
  type EditorAction,
} from "./editor-state";
import { EditorRow, focusRow } from "./editor-row";
import { useBriefDone } from "./use-brief-done";
import styles from "./brief-edit.module.css";

/**
 * Editing the Brief (BriefEdit.png): the sticky bar with Discard and
 * Done (⌘↵), the sections with their facts to reorder (drag, or ⌥↑ ⌥↓
 * on the focused fact, across headings to regroup), edit, cite and take
 * out, new facts and sections, and the Changes panel collecting it all.
 */
export function BriefEditor({
  view,
  brief,
  targets,
}: {
  view: RecordsView;
  brief: BriefView;
  targets: TargetView[] | undefined;
}) {
  const { l, space } = view;
  const e = l.brief.edit;
  const router = useRouter();
  const [state, dispatch] = useReducer(editorReducer, brief, draftOf);
  const [drag, setDrag] = useState<{
    key: string;
    over: string | null;
    before: boolean;
  } | null>(null);
  const [checked, setChecked] = useState(false);
  const [focusNew, setFocusNew] = useState<string | null>(null);
  const lastNew = useRef(state.nextId);
  const finish = useBriefDone(space, brief);
  const keepKey = useKeycap("command.keep");
  const moveCaps = useKeycaps("brief.move");
  const moveKeys = moveCaps.map((caps) => caps.join(" ")).join(" ");

  const changes = useMemo(() => changesOf(state), [state]);
  // The Brief's own [n], so a fact keeps its numeral while it moves.
  const numbers = useMemo(
    () => numberSources(brief.sections, brief.memories),
    [brief.sections, brief.memories],
  );
  const errors = useMemo(
    () => errorsOf(state, (ref) => brief.memories[ref]?.kept === true),
    [state, brief.memories],
  );
  const files = (targets ?? []).filter(
    (t) => t.delivery !== "copy" && t.syncState !== "off",
  ).length;

  // A clash: carry on from the newer version.
  useEffect(() => {
    if (finish.failure?.kind === "clash" && finish.failure.base !== null) {
      dispatch({ type: "rebase", base: finish.failure.base });
    }
  }, [finish.failure]);

  // Focus a fact just added with ↵ or "Add a fact".
  useEffect(() => {
    if (state.nextId !== lastNew.current) {
      setFocusNew(`new-${state.nextId - 1}`);
      lastNew.current = state.nextId;
    }
  }, [state.nextId]);

  const act = (action: EditorAction) => {
    setChecked(false);
    finish.clearFailure();
    dispatch(action);
  };

  const done = () => {
    if (state.editing) dispatch({ type: "keep" });
    if (errors.size > 0) {
      setChecked(true);
      return;
    }
    void finish.done(state);
  };

  useHotkey("command.keep", () => done(), { enabled: !finish.pending });
  useHotkey("brief.move", (event, match) => {
    const el = (event.target as HTMLElement | null)?.closest?.<HTMLElement>(
      "[data-fact]",
    );
    const key = el?.dataset.fact;
    if (!key || state.editing) return false;
    act({ type: "move", key, delta: match.index === 0 ? 1 : -1 });
    focusRow(key);
  });

  const discard = () => {
    if (changes.length === 0) {
      router.push(briefHref(space.slug));
      return;
    }
    act({ type: "reset", state: draftOf(brief) });
  };

  const failure = finish.failure;
  const failureText =
    failure?.kind === "clash"
      ? e.clash
      : failure?.kind === "partial"
        ? e.partial
        : failure?.kind === "failed"
          ? interpolate(e.failed, { reason: failure.message })
          : checked && errors.size > 0
            ? e.fix
            : null;

  return (
    <div className={styles.frame}>
      <div className={styles.bar}>
        <Icon name="pencil" />
        <h1 className={styles.barTitle}>{e.bar}</h1>
        <span className="mx-meta">{e.barMeta}</span>
        <span className={styles.spacer} />
        <Button variant="quiet" size="sm" onClick={discard}>
          {changes.length === 0
            ? e.discardNone
            : count(e.discardOne, e.discard, changes.length)}
        </Button>
        <Button
          variant="primary"
          size="sm"
          kbd={keepKey}
          pending={finish.pending}
          onClick={done}
        >
          {e.done}
        </Button>
      </div>

      <div className={styles.page}>
        <article className={styles.article}>
          <div className="mx-page-eyebrow">
            {interpolate(e.eyebrow, {
              space: space.name,
              facts: count(
                view.copy.brief.factsOne,
                view.copy.brief.facts,
                draftFacts(state),
              ),
            })}
          </div>
          <p className={styles.title}>{state.title}</p>
          {state.summary ? (
            <p className={styles.lede}>{state.summary}</p>
          ) : null}

          {state.sections.map((section) => (
            <section key={section.key} className={styles.section}>
              <div className={styles.heading}>
                <h2 className={styles.headingTitle}>
                  <input
                    className={styles.headingInput}
                    aria-label={interpolate(
                      section.originalHeading === null
                        ? e.newSectionLabel
                        : e.sectionLabel,
                      { section: section.originalHeading ?? section.heading },
                    )}
                    value={section.heading}
                    onChange={(event) =>
                      act({
                        type: "heading",
                        section: section.key,
                        heading: event.currentTarget.value,
                      })
                    }
                  />
                </h2>
                <Button
                  variant="quiet"
                  size="sm"
                  icon="plus"
                  className={styles.addFact}
                  onClick={() => act({ type: "add", section: section.key })}
                >
                  {e.newFactPlaceholder}
                </Button>
              </div>
              {checked && errors.get(`s:${section.key}`) ? (
                <p className={styles.error}>{e.emptyHeading}</p>
              ) : null}
              <ul
                className={styles.list}
                data-section={section.key}
                onDragOver={(event) => {
                  if (section.items.length === 0) {
                    event.preventDefault();
                    if (drag)
                      setDrag({
                        ...drag,
                        over: `section:${section.key}`,
                        before: true,
                      });
                  }
                }}
                onDrop={(event) => {
                  if (drag && section.items.length === 0) {
                    event.preventDefault();
                    act({
                      type: "moveTo",
                      key: drag.key,
                      section: section.key,
                      index: 0,
                    });
                    setDrag(null);
                  }
                }}
              >
                {section.items.map((item, index) => (
                  <EditorRow
                    key={item.key}
                    view={view}
                    item={item}
                    section={section.heading}
                    editing={state.editing?.key === item.key}
                    movedFrom={
                      item.kind !== "new" &&
                      item.from &&
                      item.from !== section.key
                        ? (state.sections.find((s) => s.key === item.from)
                            ?.heading ?? item.from)
                        : null
                    }
                    error={checked ? (errors.get(item.key) ?? null) : null}
                    memories={brief.memories}
                    dispatch={act}
                    dropBefore={drag?.over === item.key && drag.before}
                    onDragStart={(key) =>
                      setDrag({ key, over: null, before: true })
                    }
                    onDragOverRow={(key, before) =>
                      drag && setDrag({ ...drag, over: key, before })
                    }
                    onDrop={() => {
                      if (!drag) return;
                      const target =
                        drag.over === item.key && !drag.before
                          ? index + 1
                          : index;
                      act({
                        type: "moveTo",
                        key: drag.key,
                        section: section.key,
                        index: target,
                      });
                      setDrag(null);
                    }}
                    keepKey={keepKey}
                    moveKeys={moveKeys}
                    numbers={numbers}
                    autoFocus={focusNew === item.key}
                  />
                ))}
              </ul>
            </section>
          ))}

          <button
            type="button"
            className={styles.addSection}
            onClick={() => act({ type: "addSection", heading: e.newSection })}
          >
            <Icon name="plus" />
            <span>{e.addSection}</span>
          </button>
        </article>

        <aside className={styles.aside}>
          <ChangesPanel view={view} changes={changes} files={files} />
          {failureText ? (
            <p className={styles.failure} role="alert">
              {failureText}
            </p>
          ) : null}
        </aside>
      </div>
    </div>
  );
}
