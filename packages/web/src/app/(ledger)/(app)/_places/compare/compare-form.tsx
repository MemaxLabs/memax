"use client";

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { Button, Kbd, PageHeader, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate, joinList, joinSentences } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import type { DecisionResult } from "@/lib/v2/data/records";
import type { ConflictData, ConflictOption } from "@/lib/v2/data/review";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { failureText } from "@/lib/v2/records-copy";
import { placeHref } from "@/lib/v2/places";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { useAfterDecision, useQueueSnapshot } from "../../_lib/records";
import { useUndo } from "../../_lib/undo";
import { NotYetButton } from "../place";
import type { RecordsView } from "../records-view";
import { JUDGE_WAIT_MS, judgeRetryMs, pause } from "../review/use-review";
import { ConflictSides } from "./conflict-sides";
import styles from "./compare.module.css";

function optionLabel(
  view: RecordsView,
  data: ConflictData,
  option: ConflictOption,
) {
  const c = view.l.review.compare;
  if (option.label) return option.label;
  switch (option.kind) {
    case "proposal":
      return interpolate(c.labels.proposal, {
        agent: view.name(data.proposal.by),
      });
    case "kept":
      return interpolate(c.labels.kept, { ref: data.kept.ref });
    case "both":
      return c.labels.both;
    case "open":
      return c.open;
  }
}

function optionDetail(
  view: RecordsView,
  data: ConflictData,
  option: ConflictOption,
) {
  const c = view.l.review.compare;
  const agent = view.name(data.proposal.by);
  switch (option.kind) {
    case "proposal":
      return (
        option.detail ??
        interpolate(c.proposalDetail, { agent, ref: data.kept.ref })
      );
    case "kept":
      return option.detail ?? interpolate(c.keptDetail, { agent });
    case "both":
      return [option.detail, c.bothDetail]
        .filter(Boolean)
        .join(view.locale === "zh" ? "" : " ");
    case "open":
      return option.detail
        ? interpolate(c.openDetailWith, { detail: option.detail })
        : c.openDetail;
  }
}

/** Why an answer isn't open to this person, by the server's policy code. */
function refusalText(
  view: RecordsView,
  option: ConflictOption,
  ref: string,
): string {
  const f = view.rc.failure;
  const code = option.refusal?.code ?? null;
  const known = code ? (f.refused as Record<string, string>)[code] : null;
  if (known && code !== "other" && code !== "otherBare") {
    return interpolate(known, { space: view.space.name, ref });
  }
  return option.refusal?.message
    ? interpolate(f.refused.other, { message: option.refusal.message })
    : f.refused.otherBare;
}

/**
 * Both sides and the answer: the options (1–4, ↑↓), the decision as it
 * will read, and Keep the decision (↵) or Back to queue (Esc). The
 * answer goes through the ledger (resolve-conflict) and can be undone
 * from its toast for 10 minutes.
 *
 * Where the server differs from the drawn board, this renders what the
 * server does: "both" narrows each side (two fields) rather than writing
 * a third memory, "open" makes both open questions, and the footer says
 * what each answer does to each side (spec ConflictEffect).
 *
 * Rule 11 for "both": narrower words that touch another decision in force
 * are saved, not kept, until the judge has seen them. The answer then
 * wears the working mark and the checking line, and goes again (its own
 * idempotency key, reused across the waits) until it is kept, or the
 * words turn out to contradict a decision in force (said here, the fields
 * as written). Esc stops checking; the saved words stay saved. The other
 * answers keep a proposal's words as they stand, and wait the same way
 * when the judge hasn't seen them yet (503 judge_pending, same key).
 */
export function Compare({
  view,
  conflict,
  memoryRef,
}: {
  view: RecordsView;
  conflict: ConflictData;
  memoryRef: string;
}) {
  const { l, rc, space, locale, timeZone } = view;
  const c = l.review.compare;
  const router = useRouter();
  const source = useSource();
  const toast = useToast();
  const undo = useUndo();
  const snapshot = useQueueSnapshot(space);
  const afterDecision = useAfterDecision(space);
  const [keys] = useState(() => new IntentKeys());
  const [choice, setChoice] = useState<number | null>(conflict.suggested);
  const [focus, setFocus] = useState(conflict.suggested ?? 0);
  const both = conflict.options.find((o) => o.kind === "both")?.narrowed;
  const [narrowed, setNarrowed] = useState({
    proposal: both?.proposal ?? conflict.proposal.statement,
    kept: both?.kept ?? conflict.kept.statement,
  });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  // An answer waiting for the judge: checking, and what to say after it.
  const [checking, setChecking] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const waitFor = useRef<AbortController | null>(null);
  // The flagged side's version after a held "both" saved its words: the
  // If-Match of the next answer, until the conflict is read again.
  const saved = useRef(0);
  const optionRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const id = useId();
  const queueHref = placeHref(space.slug, "review");
  const option = choice === null ? undefined : conflict.options[choice];
  const agent = view.name(conflict.proposal.by);
  const keptBy = view.name(conflict.kept.by);
  const { proposal, kept } = conflict;

  useEffect(() => {
    // Keyboard first: start on the suggested answer (or the first), so
    // 1–4, ↑↓ and ↵ work at once.
    optionRefs.current[conflict.suggested ?? 0]?.focus();
  }, [conflict.suggested]);

  const choose = (i: number) => {
    const next = conflict.options[i];
    if (!next) return;
    setFocus(i);
    optionRefs.current[i]?.focus();
    if (!next.allowed) return;
    setChoice(i);
    setError(false);
  };

  const keep = async () => {
    if (!option || !option.allowed || pending) return;
    const words = {
      proposal: narrowed.proposal.trim(),
      kept: narrowed.kept.trim(),
    };
    if (option.kind === "both" && (!words.proposal || !words.kept)) {
      setError(true);
      return;
    }
    const both = option.kind === "both";
    const restore = snapshot(proposal.ref);
    const controller = new AbortController();
    waitFor.current = controller;
    let version = Math.max(proposal.version, saved.current);
    // A held "both", sent again, is its own intent (a new key), reused
    // across its waits for the judge.
    let phase = "";
    const intentNow = () =>
      intentOf(
        "resolve",
        proposal.ref,
        version,
        option.kind,
        both ? words.proposal : "",
        both ? words.kept : "",
        phase,
      );
    let intent = intentNow();
    setPending(true);
    setNotice(null);
    try {
      const deadline = Date.now() + JUDGE_WAIT_MS;
      let result: DecisionResult;
      for (;;) {
        intent = intentNow();
        try {
          result = await source.review.resolveConflict({
            space,
            ref: proposal.ref,
            other: kept.ref,
            version,
            option: option.kind,
            ...(both
              ? { statement: words.proposal, otherStatement: words.kept }
              : {}),
            idempotencyKey: keys.keyFor(intent),
          });
        } catch (err) {
          const failure = toFailure(err);
          // The judge hasn't looked at the words yet (saved just now, before
          // an Esc, or an edit's): wait for it, with the same key. Every
          // answer that keeps a proposal's words waits this way.
          if (
            failure.kind !== "busy" ||
            !failure.judge ||
            Date.now() > deadline
          ) {
            throw err;
          }
          setChecking(true);
          if (
            !(await pause(judgeRetryMs(failure.retryAfter), controller.signal))
          )
            return;
          continue;
        }
        keys.settle(intent);
        if (!result.judgePending) break;
        if (Date.now() > deadline) throw new Error("still checking");
        // Saved for the judge: the flagged side's words are its new
        // version, and the same answer goes again in a moment.
        version = result.version ?? version;
        saved.current = version;
        phase = "checked";
        setChecking(true);
        afterDecision({ leftQueue: false });
        if (!(await pause(judgeRetryMs(1), controller.signal))) return;
      }
      // Settled (even if Esc came while the last answer was on its way).
      saved.current = 0;
      const entry = result.receipt
        ? undo.record({
            space,
            command: "resolve",
            ref: proposal.ref,
            receipt: result.receipt,
            restore,
          })
        : null;
      const refs = { proposal: proposal.ref, kept: kept.ref };
      let text: string;
      switch (option.kind) {
        case "proposal":
          text =
            result.recompiled === null
              ? interpolate(c.kept, { ref: proposal.ref })
              : count(
                  c.keptRecompiledOne,
                  c.keptRecompiled,
                  result.recompiled,
                  {
                    ref: proposal.ref,
                  },
                );
          break;
        case "kept":
          text = interpolate(c.stays, refs);
          break;
        case "both":
          text = interpolate(c.keptBoth, refs);
          break;
        case "open":
          text = interpolate(c.leftOpen, refs);
          break;
      }
      toast({
        // A rejection wears `off`, as in Review; an open question no mark.
        ...(option.kind === "open"
          ? {}
          : { state: option.kind === "kept" ? "off" : "kept" }),
        text,
        ...(entry ? { undo: () => void undo.run(entry) } : {}),
      });
      afterDecision({ leftQueue: true });
      router.push(queueHref);
    } catch (err) {
      const failure = toFailure(err);
      keys.settle(intent);
      if (failure.kind === "in-conflict") {
        // The words contradict another decision in force: the conflict
        // stays open, and narrower words stay as written to change.
        const other = failure.with ?? kept.ref;
        setNotice(
          both
            ? interpolate(c.inConflictBoth, { with: other })
            : interpolate(c.inConflictWords, {
                ref: proposal.ref,
                with: other,
              }),
        );
        afterDecision({ leftQueue: false });
        return;
      }
      toast({
        state: "proposed",
        text: failureText(rc, failure, {
          command: "resolve",
          ref: proposal.ref,
          space: space.name,
          locale,
        }),
      });
    } finally {
      setPending(false);
      setChecking(false);
      if (waitFor.current === controller) waitFor.current = null;
    }
  };

  /** Esc while an answer waits for the judge: stop waiting, keep the words. */
  const stopChecking = () => {
    const controller = waitFor.current;
    if (!controller || !checking) return false;
    controller.abort();
    waitFor.current = null;
    setChecking(false);
    setNotice(
      option?.kind === "both" ? c.stoppedChecking : c.stoppedCheckingWords,
    );
    return true;
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (isComposing(event.nativeEvent)) return;
    const inField = (event.target as HTMLElement).tagName === "TEXTAREA";
    if (event.key === "Escape") {
      event.preventDefault();
      if (!stopChecking()) router.push(queueHref);
    } else if (
      event.key === "Enter" &&
      (!inField || event.metaKey || event.ctrlKey)
    ) {
      event.preventDefault();
      void keep();
    } else if (!inField && /^[1-9]$/.test(event.key)) {
      event.preventDefault();
      choose(Number(event.key) - 1);
    } else if (
      !inField &&
      (event.key === "ArrowDown" || event.key === "ArrowUp")
    ) {
      event.preventDefault();
      const n = conflict.options.length;
      choose((focus + (event.key === "ArrowDown" ? 1 : n - 1)) % n);
    }
  };

  // What the chosen answer does to each side, then what keeping reaches.
  const effects = option
    ? option.effects.map((e) => {
        const side = e.ref === proposal.ref ? "proposal" : "kept";
        const original = side === "proposal" ? proposal : kept;
        const changedWords =
          option.kind === "both" &&
          narrowed[side].trim() !== original.statement.trim();
        return interpolate(
          changedWords ? c.effects.narrowed : c.effects[e.change],
          { ref: e.ref },
        );
      })
    : [];
  const files =
    conflict.recompiles === null
      ? null
      : count(c.filesOne, c.files, conflict.recompiles);
  const tells = joinList(conflict.tells.map(view.agentName), locale);
  const reach =
    option && option.kind !== "kept" && files
      ? conflict.tells.length > 0
        ? interpolate(c.reach, { files, agents: tells })
        : interpolate(c.reachFiles, { files })
      : null;
  const footer = joinSentences([...effects, reach], locale);
  const date = formatShortDate(new Date(kept.at), timeZone, locale);
  const title =
    conflict.question ??
    (conflict.area
      ? interpolate(c.titleArea, { area: conflict.area })
      : c.titleNone);

  let words;
  if (option?.kind === "both") {
    words = (
      <fieldset className={styles.decision}>
        <legend className="mx-sr">{c.decisionBoth}</legend>
        {(["proposal", "kept"] as const).map((side) => (
          <div key={side} className={styles.decision}>
            <label className={styles.label} htmlFor={`${id}-${side}`}>
              {interpolate(c.side, { ref: conflict[side].ref })}
            </label>
            <textarea
              id={`${id}-${side}`}
              className={styles.textarea}
              rows={2}
              value={narrowed[side]}
              onChange={(event) => {
                const value = event.target.value;
                setNarrowed((n) => ({ ...n, [side]: value }));
                setError(false);
              }}
              aria-invalid={(error && !narrowed[side].trim()) || undefined}
              readOnly={pending}
            />
          </div>
        ))}
        {error ? (
          <p className={styles.error} role="alert">
            {c.needsWords}
          </p>
        ) : null}
      </fieldset>
    );
  } else if (option?.kind === "open") {
    words = <p className={`mx-meta ${styles.footText}`}>{c.openNote}</p>;
  } else if (option) {
    words = (
      <div className={styles.decision}>
        <label className={styles.label} htmlFor={`${id}-d`}>
          {c.decision}
        </label>
        {/* The side that stands, as it is: the server keeps it, words and all. */}
        <textarea
          id={`${id}-d`}
          className={styles.textarea}
          rows={2}
          value={option.decision}
          readOnly
        />
      </div>
    );
  }

  return (
    <div className={`mx-page ${styles.page}`} onKeyDown={onKeyDown}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(c.eyebrow, {
          proposal: proposal.ref,
          kept: kept.ref,
        })}
        title={title}
        lede={interpolate(c.lede, { agent, name: keptBy, date })}
        actions={
          <NotYetButton variant="secondary" icon="handoff">
            {interpolate(c.ask, { name: keptBy })}
          </NotYetButton>
        }
      />
      <ConflictSides view={view} conflict={conflict} />
      <section className={styles.answer} aria-labelledby={`${id}-q`}>
        <h2 className={styles.question} id={`${id}-q`}>
          {c.question}
        </h2>
        <div
          className={styles.options}
          role="radiogroup"
          aria-label={c.optionsLabel}
        >
          {conflict.options.map((o, i) => {
            const refusal = o.allowed
              ? null
              : refusalText(view, o, proposal.ref);
            return (
              <button
                key={o.kind}
                ref={(el) => {
                  optionRefs.current[i] = el;
                }}
                type="button"
                role="radio"
                aria-checked={i === choice}
                aria-disabled={o.allowed ? undefined : true}
                title={refusal ?? undefined}
                tabIndex={i === focus ? 0 : -1}
                className={styles.option}
                onClick={() => choose(i)}
              >
                <Kbd aria-hidden="true">{i + 1}</Kbd>
                <span className={styles.optionTitle}>
                  {optionLabel(view, conflict, o)}
                  <span className={styles.optionDetail}>
                    {refusal ?? optionDetail(view, conflict, o)}
                  </span>
                </span>
              </button>
            );
          })}
        </div>
        {words}
        {/* Waiting for the judge: the neutral working mark, never a spinner. */}
        <p className={styles.checking} role="status" aria-live="polite">
          {checking ? (
            <>
              <StateMark state="working" label={l.review.judge.checking} />
              <span className="mx-meta">
                {option?.kind === "both" ? c.checkingBoth : c.checkingWords}
              </span>
            </>
          ) : notice ? (
            <span className="mx-meta">{notice}</span>
          ) : null}
        </p>
        <footer className={styles.foot}>
          <span className={`mx-meta ${styles.footText}`}>{footer}</span>
          <span className={styles.spacer} />
          {checking ? (
            <Button
              variant="quiet"
              size="sm"
              kbd="Esc"
              onClick={() => void stopChecking()}
            >
              {c.stopChecking}
            </Button>
          ) : (
            <Button variant="quiet" size="sm" kbd="Esc" href={queueHref}>
              {c.back}
            </Button>
          )}
          <Button
            variant="keep"
            size="sm"
            kbd="↵"
            onClick={() => void keep()}
            pending={pending}
            disabled={!option}
            disabledReason={option ? undefined : c.choose}
          >
            {c.keep}
          </Button>
        </footer>
      </section>
    </div>
  );
}
