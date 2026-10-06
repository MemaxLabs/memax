"use client";

import { ReviewCard } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatShortDate } from "@/lib/v2/copy";
import type { ReviewCardData, ReviewItem } from "@/lib/v2/data/review";
import type { RecordsView } from "../records-view";
import type { ReviewController } from "./use-review";

/**
 * A proposal as Review.png draws it, through Ledger's controlled
 * ReviewCard: the italic claim, the proposer, the quarantine notice for
 * external content, the diff for an update, the evidence and the
 * receipt line, and Keep · Edit · Reject. `kept` is the optimistic seal.
 */
export function ProposalCard({
  view,
  review,
  item,
  card,
}: {
  view: RecordsView;
  review: ReviewController;
  item: ReviewItem;
  card: ReviewCardData | undefined;
}) {
  const { l, rc, space, viewer, now, timeZone, locale } = view;
  const r = l.review;
  const proposer = view.stamp(item.by);
  const sealed = review.state.sealed?.ref === item.ref;
  const statement =
    (sealed ? review.state.sealed?.statement : null) ?? item.statement;
  const session = item.session
    ? interpolate(rc.sessionRef, { session: item.session })
    : undefined;
  const name = view.name(item.by);
  const external = item.external
    ? card?.readFrom
      ? interpolate(r.external, { agent: name, source: card.readFrom })
      : interpolate(r.externalUnnamed, { agent: name })
    : undefined;
  const evidenceSource = card?.evidence
    ? [
        card.evidence.source,
        item.session ? interpolate(r.quoted, { session: item.session }) : null,
      ]
        .filter(Boolean)
        .join(" · ")
    : undefined;

  return (
    <ReviewCard
      // A new card for a new memory: the seal never carries over.
      key={item.ref}
      statement={statement}
      agent={proposer?.agent ?? proposer?.person ?? ""}
      time={view.time(item.at)}
      id={item.ref}
      space={item.intoSpace ?? space.name}
      source={session}
      evidence={card?.evidence?.quote}
      evidenceSource={evidenceSource}
      // While sealed with edited words, there's nothing left to diff.
      before={
        sealed && review.state.sealed?.statement
          ? undefined
          : card?.before?.statement
      }
      beforeId={card?.before?.ref}
      external={external}
      conflictWith={card?.conflict?.statement}
      kept={sealed}
      pending={review.state.busy === item.ref}
      onKeep={() => void review.keep(item)}
      onEdit={() =>
        review.dispatch({
          type: "edit",
          ref: item.ref,
          base: { version: item.version, statement: item.statement },
        })
      }
      onReject={() => review.dispatch({ type: "reject", ref: item.ref })}
      keepDisabledReason={review.canDecide ? undefined : r.viewer}
      keptBy={viewer?.initials}
      keptDate={formatShortDate(now, timeZone, locale)}
      keptTime={rc.time.justNow}
    />
  );
}
