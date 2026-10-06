"use client";

import { AgentStamp, Button, Receipt } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatAgo } from "@/lib/v2/copy";
import type { Actor } from "@/lib/v2/data/records";
import type { ReviewItem } from "@/lib/v2/data/review";
import { useKeycap } from "@/lib/v2/keymap/react";
import { isYou } from "@/lib/v2/records-copy";
import { EditClash } from "../../_components/edit-clash";
import { StatementEditor } from "../../_components/statement-editor";
import type { RecordsView } from "../records-view";
import { FlagCard } from "./flag-card";
import { ProposalCard } from "./proposal-card";
import { RejectPanel } from "./reject-panel";
import { CardSkeleton } from "./skeleton";
import { Touches } from "./touches";
import type { ReviewController } from "./use-review";
import styles from "./review.module.css";

export function compareHref(slug: string, item: ReviewItem): string | null {
  // Only a conflict the judge linked to a kept memory has two sides.
  return item.conflictsWith
    ? `/${encodeURIComponent(slug)}/review/${encodeURIComponent(item.ref)}/compare`
    : null;
}

/**
 * Review.png's right column: where you are in the queue, the card (a
 * proposal, the editor, an edit clash, or a flagged kept memory), the
 * reject reason when X was pressed, and "This touches".
 */
export function CardColumn({
  view,
  review,
}: {
  view: RecordsView;
  review: ReviewController;
}) {
  const { l, rc, space, viewer, copy, now } = view;
  const r = l.review;
  const { selected: item, index, visible, card, state } = review;
  const keepChord = useKeycap("command.keep");

  if (review.queue.data === undefined) {
    return (
      <section className={styles.cardPane} aria-busy="true">
        <div className={styles.cardColumn}>
          <CardSkeleton />
        </div>
      </section>
    );
  }
  if (!item) return <section className={styles.cardPane} />;

  const mode = state.mode;
  const editing =
    mode.kind === "editing" && mode.ref === item.ref ? mode : null;
  const clash = mode.kind === "clash" && mode.ref === item.ref ? mode : null;
  const rejecting =
    mode.kind === "rejecting" && mode.ref === item.ref ? mode : null;
  const href = compareHref(space.slug, item);
  const name = view.name(item.by);
  const position = interpolate(editing ? r.positionEditing : r.position, {
    space: space.name,
    n: index + 1,
    total: visible.length,
  });
  const stamp = view.stamp(item.by);

  let body;
  if (clash) {
    const by: Actor | null = clash.theirs.by;
    const ago = formatAgo(copy, clash.theirs.at, now);
    const title = isYou(by)
      ? interpolate(rc.clash.byYou, { ago })
      : by && (by.kind !== "person" || by.name)
        ? interpolate(rc.clash.by, { name: view.name(by), ago })
        : interpolate(rc.clash.byTeammate, { ago });
    body = (
      <EditClash
        title={title}
        stamp={view.stamp(by)}
        before={clash.base.statement}
        theirs={clash.theirs.statement}
        yours={interpolate(rc.clash.yours, { text: clash.mine })}
        labels={rc.clash}
        onKeepTheirs={review.keepTheirs}
        onCombine={() => review.dispatch({ type: "combine" })}
        onKeepMine={() => review.keepMine(item)}
        pending={state.busy === item.ref}
      />
    );
  } else if (editing) {
    body = (
      <StatementEditor
        stateLabel={r.edit.title}
        by={
          <>
            {stamp ? <AgentStamp {...stamp} size="sm" decorative /> : null}
            {name} <span className="mx-meta">· {view.time(item.at)}</span>
          </>
        }
        base={editing.base.statement}
        draft={editing.draft}
        reason={editing.reason}
        onDraft={(draft) => review.dispatch({ type: "draft", draft })}
        onReason={(reason) => review.dispatch({ type: "draft", reason })}
        onCancel={() => review.dispatch({ type: "browse" })}
        onSubmit={() => void review.submitEdit(item, editing)}
        labels={{
          statement: rc.editor.statement,
          diff: interpolate(rc.editor.yourChangeTo, { name }),
          why: rc.editor.why,
          whyHint: rc.editor.whyHint,
          cancel: rc.editor.cancel,
          submit: rc.editor.keepEdited,
        }}
        receipt={
          <Receipt
            person={viewer?.initials}
            name={rc.actor.you}
            action={rc.verbs.kept}
            source={interpolate(rc.editor.editedFrom, { name })}
            id={item.ref}
          />
        }
        pending={state.busy === item.ref}
        error={editing.error === "empty" ? rc.editor.empty : null}
        submitKey={keepChord}
      />
    );
  } else if (item.lifecycle === "proposed") {
    body = (
      <>
        <ProposalCard
          view={view}
          review={review}
          item={item}
          card={card.data}
        />
        {rejecting ? (
          <RejectPanel
            view={view}
            item={item}
            reason={rejecting.reason}
            pending={state.busy === item.ref}
            onReason={(reason) => review.dispatch({ type: "draft", reason })}
            onReject={() => void review.reject(item, rejecting.reason)}
            onCancel={() => review.dispatch({ type: "browse" })}
          />
        ) : null}
        {href && !rejecting ? (
          <Button
            className={styles.compare}
            variant="secondary"
            size="sm"
            kbd="C"
            href={href}
          >
            {r.compareBoth}
          </Button>
        ) : null}
      </>
    );
  } else {
    body = <FlagCard view={view} item={item} compareHref={href} />;
  }

  return (
    <section className={styles.cardPane} aria-label={copy.review.title}>
      <div className={styles.cardColumn}>
        <div className={styles.metaRow}>
          <span className="mx-meta">{position}</span>
          {item.session && item.action !== "flagged" ? (
            <span className="mx-meta">
              {interpolate(r.session, { session: item.session })}
            </span>
          ) : null}
        </div>
        {body}
        <div className={styles.pager}>
          <Button
            variant="quiet"
            size="sm"
            disabled={index <= 0}
            onClick={() => review.move(-1)}
          >
            {r.previous}
          </Button>
          <Button
            variant="quiet"
            size="sm"
            disabled={index >= visible.length - 1}
            onClick={() => review.move(1)}
          >
            {r.next}
          </Button>
        </div>
        <Touches
          view={view}
          item={item}
          card={card.data}
          editing={Boolean(editing)}
        />
      </div>
    </section>
  );
}
