"use client";

import { useEffect, useRef, type ReactNode } from "react";
import Link from "next/link";
import { Button, DecisionGate, formatNodes } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatShortDate } from "@/lib/v2/copy";
import {
  answerNeedsWebHere,
  gateStatusAt,
  type GateView,
} from "@/lib/v2/data/gates";
import { useSource } from "../../_lib/data";
import { useSignInAgain } from "../../_lib/sign-in";
import type { RecordsView } from "../records-view";
import { memoryHref } from "./hrefs";
import type { ReviewController } from "./use-review";
import styles from "./review.module.css";

/**
 * A decision gate in Review, through Ledger's DecisionGate (Handoff.png
 * draws it): the agent and its question, the context, two to four
 * options. Choosing one (1–4, a click, the arrows inside the options)
 * then Answer (↵) asks first, naming what happens; ↵ again answers. The
 * seal then stamps with the decision it became. Where decisions need a
 * person on the web (D15) and this session isn't one, the card says so
 * before anyone tries. Withdraw is quiet, and confirmed inline. A gate
 * that ended says how.
 */
export function GateCard({
  view,
  review,
  gate,
}: {
  view: RecordsView;
  review: ReviewController;
  gate: GateView;
}) {
  const { l, rc, space, viewer, now, timeZone, locale, agentName } = view;
  const g = l.review.gate;
  const source = useSource();
  const signInAgain = useSignInAgain();
  const gates = review.gateCards;
  const card = gates.cardOf(gate.ref);
  const status = gateStatusAt(gate, now);
  const asker = agentName(gate.agent);
  const needsWeb =
    status === "waiting" &&
    (card.needsWeb || answerNeedsWebHere(gate, source.gates.webSession()));
  const answerRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLElement>(null);

  // The confirmation takes the focus where ↵ answers; withdrawing's
  // goes to Cancel, the safe default.
  useEffect(() => {
    if (card.step === "confirm") answerRef.current?.focus();
    else if (card.step === "withdraw") cancelRef.current?.focus();
  }, [card.step]);

  const label = gate.options[card.choice]?.label ?? "";
  const pending = card.pending !== null;
  let footer: ReactNode;
  if (card.step === "confirm" && status === "waiting") {
    footer = (
      <>
        <p className={styles.gateConfirm} role="status">
          <strong>{interpolate(g.confirm, { label })}</strong> {g.confirmDetail}
        </p>
        <div className="mx-gate-actions">
          <Button
            variant="quiet"
            size="sm"
            kbd="Esc"
            disabled={pending}
            onClick={() => gates.back(gate)}
          >
            {g.cancel}
          </Button>
          <Button
            ref={answerRef}
            variant="keep"
            size="sm"
            kbd="↵"
            pending={card.pending === "answer"}
            onClick={() => void gates.answer(gate)}
          >
            {g.answer}
          </Button>
        </div>
      </>
    );
  } else if (card.step === "withdraw" && status === "waiting") {
    footer = (
      <>
        <p className={styles.gateConfirm} role="status">
          <strong>{interpolate(g.withdrawTitle, { ref: gate.ref })}</strong>{" "}
          {interpolate(g.withdrawDetail, { agent: asker })}
        </p>
        <div className="mx-gate-actions">
          <Button
            ref={cancelRef}
            variant="quiet"
            size="sm"
            kbd="Esc"
            disabled={pending}
            onClick={() => gates.back(gate)}
          >
            {g.cancel}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            pending={card.pending === "withdraw"}
            onClick={() => void gates.withdraw(gate)}
          >
            {g.withdraw}
          </Button>
        </div>
      </>
    );
  }

  const answered = card.answered
    ? {
        id: card.answered.memory.ref,
        date: formatShortDate(now, timeZone, locale),
        by: viewer?.initials,
        time: rc.time.justNow,
      }
    : undefined;
  const ended =
    !answered && status !== "waiting" ? endedLine(view, gate, status) : null;

  return (
    <DecisionGate
      // A new card for a new gate: a choice or the seal never carries over.
      key={gate.ref}
      agent={gate.agent}
      question={gate.question}
      context={gate.context ?? undefined}
      options={gate.options.map((o) => ({
        label: o.label,
        detail: o.detail ?? undefined,
      }))}
      selected={card.choice}
      onSelect={(i) => void gates.choose(gate, i)}
      onAnswer={() => void gates.answer(gate)}
      pending={card.pending === "answer"}
      space={space.name}
      time={view.time(gate.askedAt)}
      answerDisabledReason={
        !review.canDecide ? g.viewer : needsWeb ? g.needsWebReason : undefined
      }
      notice={
        needsWeb ? (
          <>
            <span>{interpolate(g.needsWeb, { space: space.name })}</span>
            <Button variant="secondary" size="sm" onClick={signInAgain}>
              {g.signInAgain}
            </Button>
          </>
        ) : undefined
      }
      actions={
        review.canDecide ? (
          <Button
            variant="quiet"
            size="sm"
            disabled={pending}
            onClick={() => void gates.withdraw(gate)}
          >
            {g.withdraw}
          </Button>
        ) : undefined
      }
      footer={footer}
      answered={answered}
      ended={ended ?? undefined}
      status={
        answered
          ? g.answered
          : status !== "waiting"
            ? g.status[status]
            : undefined
      }
    />
  );
}

/** How a gate ended, for the line in place of its footer. */
function endedLine(
  view: RecordsView,
  gate: GateView,
  status: "answered" | "withdrawn" | "expired",
): ReactNode {
  const e = view.l.review.gate.ended;
  const asker = view.agentName(gate.agent);
  switch (status) {
    case "answered": {
      const answer = gate.answer;
      if (!answer) return e.answeredBare;
      const self = answer.by?.kind === "person" && answer.by.self;
      return formatNodes(self ? e.answeredByYou : e.answeredBy, {
        label: answer.label,
        memory: (
          <Link
            className="mx-receipt-id"
            href={memoryHref(view.space.slug, answer.memory)}
          >
            {answer.memory}
          </Link>
        ),
      });
    }
    case "withdrawn": {
      const by = gate.withdrawn?.by;
      if (by?.kind === "person") {
        return by.self ? e.withdrawnByYou : e.withdrawnBy;
      }
      return interpolate(e.withdrawnByAgent, { agent: asker });
    }
    case "expired":
      return interpolate(e.expired, { agent: asker });
  }
}
