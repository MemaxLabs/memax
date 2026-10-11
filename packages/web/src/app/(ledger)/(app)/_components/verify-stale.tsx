"use client";

import { useRef, useState, type RefObject } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { AgentStamp, Button, Receipt, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate } from "@/lib/v2/copy";
import type { StaleWhy } from "@/lib/v2/data/memories";
import { KeyScopeBoundary, useHotkey } from "@/lib/v2/keymap/react";
import { useOverlays } from "../_lib/overlays";
import { useForgetPreview, useStaleWhy } from "../_lib/records";
import { useRecordsView, type RecordsView } from "../_places/records-view";
import { StatementText } from "./statement-text";
import { useVerifyAnswer } from "./use-verify-answer";
import { Answers, forgetRefusalText, WhatChanged } from "./verify-parts";
import styles from "./verify-stale.module.css";

/**
 * VerifyStale (VerifyStale.png), a layer over the page: V on a stale
 * memory in Memories, the Brief, a memory's page or Review's flag card.
 * It says why the memory is stale (GET /v2/memories/{ref}/stale), what
 * Forget would remove (the preview), and takes one of three answers on
 * 1, 2 and 3: still true, update it, or forget it. ↵ answers.
 *
 * Base UI's Dialog brings the focus trap, Escape, outside press, scroll
 * lock and focus return; a modal key scope silences the page's keys.
 */
export function VerifyStale() {
  const { verify, closeVerify } = useOverlays();
  // The last memory opened, kept while the layer closes so it leaves with
  // its scrim (the portal unmounts both once the exit ends). Rendering it
  // only while open would leave the scrim behind, taking every click.
  const [shown, setShown] = useState(verify);
  if (verify && verify !== shown) setShown(verify);
  return (
    <Dialog.Root
      open={verify !== null}
      onOpenChange={(open) => {
        if (!open) closeVerify();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {shown ? (
          <VerifyLayer key={shown.session} memoryRef={shown.ref} />
        ) : null}
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function VerifyLayer({ memoryRef: ref }: { memoryRef: string }) {
  const view = useRecordsView();
  const v = view.l.verify;
  const why = useStaleWhy(view.space, ref);
  const first = useRef<HTMLButtonElement>(null);
  const data = why.data;
  return (
    <Dialog.Popup
      className={styles.layer}
      initialFocus={first}
      aria-labelledby="verify-title"
    >
      <KeyScopeBoundary name="verify-stale" modal>
        <section className={styles.dialog}>
          <header className={styles.head}>
            <Dialog.Title id="verify-title" className={styles.title}>
              {interpolate(v.title, { ref })}
            </Dialog.Title>
            {data?.stale ? (
              <StateMark
                state="stale"
                label={
                  data.since
                    ? interpolate(v.since, {
                        date: formatShortDate(
                          new Date(data.since),
                          view.timeZone,
                          view.locale,
                        ),
                      })
                    : v.sinceBare
                }
              />
            ) : null}
            <span className={styles.spacer} />
            <Dialog.Close
              render={
                <Button
                  variant="quiet"
                  size="sm"
                  icon="x"
                  aria-label={v.close}
                />
              }
            />
          </header>
          {data && data.stale ? (
            <VerifyBody view={view} why={data} first={first} />
          ) : (
            <div className={styles.body}>
              <p className={styles.plain} role="status">
                {data === undefined
                  ? why.isError
                    ? interpolate(v.failed, { ref })
                    : interpolate(v.loading, { ref })
                  : interpolate(data === null ? v.notFound : v.notStale, {
                      ref,
                    })}
              </p>
              {why.isError ? (
                <div>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => void why.refetch()}
                  >
                    {v.retry}
                  </Button>
                </div>
              ) : null}
            </div>
          )}
        </section>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}

function VerifyBody({
  view,
  why,
  first,
}: {
  view: RecordsView;
  why: StaleWhy;
  first: RefObject<HTMLButtonElement | null>;
}) {
  const { l } = view;
  const v = l.verify;
  const state = useVerifyAnswer(view, why);
  const preview = useForgetPreview(view.space, why.ref, why.version, true);
  const viewer = view.space.role === "viewer";
  const answerKeys = ["still_true", "update", "forget"] as const;

  const submit = () => {
    if (viewer) return;
    void state.submit(preview.data);
  };

  useHotkey("verify.choose", (_, { index }) => {
    const next = answerKeys[index];
    if (!next || state.pending) return false;
    state.choose(next);
    // The option takes focus, so ↵ answers with it and ↓↑ move on.
    const el = document.querySelector<HTMLElement>(
      `.${styles.dialog} [data-answer="${next}"]`,
    );
    if (next !== "update" || !state.editing) el?.focus();
  });

  useHotkey("verify.answer", (event) => {
    if (!answersHere(event.target)) return false;
    submit();
  });

  const kept = why.kept;
  const keptStamp = kept ? view.stamp(kept.by) : null;
  const reads =
    why.readsSince === null
      ? undefined
      : why.readsSince === 0
        ? v.readsSinceNone
        : count(v.readsSinceOne, v.readsSince, why.readsSince);
  const suggestionStamp = why.suggestion ? view.stamp(why.suggestion.by) : null;
  // Forget goes only with what the preview says it does.
  const forgetBlocked = preview.isError
    ? v.forgetFailed
    : !preview.data
      ? v.forgetChecking
      : forgetRefusalText(view, why, preview.data);
  const primary =
    state.answer === "forget"
      ? { variant: "danger" as const, label: v.forgetSubmit }
      : {
          variant: "keep" as const,
          label: state.answer === "update" ? v.keepUpdate : v.keep,
        };
  const disabledReason = viewer
    ? v.viewer
    : !state.answer
      ? v.choose
      : state.answer === "forget" && forgetBlocked
        ? forgetBlocked
        : undefined;

  return (
    <>
      <div className={styles.body}>
        <div className={styles.statementBlock}>
          <p className={styles.statement}>
            <StatementText text={why.statement} />
          </p>
          {kept ? (
            <Receipt
              {...(keptStamp ?? {})}
              action={view.rc.verbs[kept.action]}
              time={formatShortDate(
                new Date(kept.at),
                view.timeZone,
                view.locale,
              )}
              source={reads}
              id={why.ref}
            />
          ) : (
            <Receipt id={why.ref} source={reads} />
          )}
        </div>
        <WhatChanged view={view} why={why} />
        <Answers
          view={view}
          why={why}
          state={state}
          preview={preview.data}
          previewFailed={preview.isError}
          firstRef={first}
          onSubmit={submit}
        />
      </div>
      <footer className={styles.foot}>
        <span className={`mx-meta ${styles.suggested}`}>
          {why.suggestion ? (
            <>
              {suggestionStamp ? (
                <AgentStamp {...suggestionStamp} size="sm" />
              ) : null}
              {interpolate(v.suggested, {
                name: view.name(why.suggestion.by),
                source: why.suggestion.from,
              })}
            </>
          ) : null}
        </span>
        <Dialog.Close render={<Button variant="quiet">{v.cancel}</Button>} />
        <Button
          variant={primary.variant}
          kbd="↵"
          pending={state.pending}
          disabled={Boolean(disabledReason)}
          disabledReason={disabledReason}
          aria-keyshortcuts="Enter"
          onClick={submit}
        >
          {primary.label}
        </Button>
      </footer>
    </>
  );
}

/**
 * Whether ↵ here is the dialog's answer: an option or the page. Any other
 * button (Cancel, close, Change the words) and the field keep their own.
 */
function answersHere(target: unknown): boolean {
  if (!(target instanceof HTMLElement)) return true;
  if (target.closest("[role=radio]")) return true;
  return !target.closest("button, a, input, textarea, select");
}
