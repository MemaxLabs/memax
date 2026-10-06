"use client";

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { Button, Kbd, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate, joinList } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import type { ConflictData, ConflictOption } from "@/lib/v2/data/review";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { failureText } from "@/lib/v2/records-copy";
import { placeHref } from "@/lib/v2/places";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { useAfterDecision } from "../../_lib/records";
import { NotYetButton } from "../place";
import type { RecordsView } from "../records-view";
import { ConflictSides } from "./conflict-sides";
import styles from "./compare.module.css";

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

/**
 * Both sides and the answer: the judge's options (1–4, ↑↓), the decision
 * as it will read, and Keep the decision (↵) or Back to queue (Esc).
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
  const afterDecision = useAfterDecision(space);
  const [keys] = useState(() => new IntentKeys());
  const [choice, setChoice] = useState(conflict.suggested);
  const [decision, setDecision] = useState(
    conflict.options[conflict.suggested]?.decision ?? "",
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const optionRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const id = useId();
  const queueHref = placeHref(space.slug, "review");
  const option = conflict.options[choice];
  const agent = view.name(conflict.proposal.by);
  const keptBy = view.name(conflict.kept.by);

  useEffect(() => {
    // Keyboard first: start on the suggested answer, so 1–4, ↑↓ and ↵ work at once.
    optionRefs.current[conflict.suggested]?.focus();
  }, [conflict.suggested]);

  const choose = (i: number) => {
    const next = conflict.options[i];
    if (!next) return;
    setChoice(i);
    setDecision(next.decision);
    setError(false);
    optionRefs.current[i]?.focus();
  };

  const keep = async () => {
    if (!option || pending) return;
    const words = decision.trim();
    if (option.kind !== "open" && !words) {
      setError(true);
      return;
    }
    const intent = intentOf("resolve", memoryRef, 0, option.kind, words);
    setPending(true);
    try {
      const result = await source.review.resolveConflict({
        space,
        ref: memoryRef,
        option: option.kind,
        decision: words,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      toast({
        state: option.kind === "open" ? "off" : "kept",
        text:
          option.kind === "open"
            ? interpolate(c.leftOpen, { ref: memoryRef })
            : result.recompiled === null
              ? interpolate(c.kept, { ref: result.ref })
              : count(
                  c.keptRecompiledOne,
                  c.keptRecompiled,
                  result.recompiled,
                  {
                    ref: result.ref,
                  },
                ),
      });
      afterDecision({ leftQueue: option.kind !== "open" });
      router.push(queueHref);
    } catch (err) {
      const failure = toFailure(err);
      keys.settle(intent);
      toast({
        state: "proposed",
        text: failureText(rc, failure, {
          command: "keep",
          ref: memoryRef,
          space: space.name,
          locale,
        }),
      });
    } finally {
      setPending(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (isComposing(event.nativeEvent)) return;
    const inField = (event.target as HTMLElement).tagName === "TEXTAREA";
    if (event.key === "Escape") {
      event.preventDefault();
      router.push(queueHref);
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
      choose((choice + (event.key === "ArrowDown" ? 1 : n - 1)) % n);
    }
  };

  const files =
    conflict.recompiles === null
      ? ""
      : count(c.filesOne, c.files, conflict.recompiles);
  const tells = joinList(conflict.tells.map(view.agentName), locale);
  const footer = option
    ? interpolate(c.footer[option.kind], {
        kept: conflict.kept.ref,
        proposal: conflict.proposal.ref,
        files,
        agents: tells,
        agent,
      })
    : "";
  const date = formatShortDate(new Date(conflict.kept.at), timeZone, locale);

  return (
    <div className={`mx-page ${styles.page}`} onKeyDown={onKeyDown}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(c.eyebrow, {
          proposal: conflict.proposal.ref,
          kept: conflict.kept.ref,
        })}
        title={conflict.question}
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
          {conflict.options.map((o, i) => (
            <button
              key={o.kind}
              ref={(el) => {
                optionRefs.current[i] = el;
              }}
              type="button"
              role="radio"
              aria-checked={i === choice}
              tabIndex={i === choice ? 0 : -1}
              className={styles.option}
              onClick={() => choose(i)}
            >
              <Kbd aria-hidden="true">{i + 1}</Kbd>
              <span className={styles.optionTitle}>
                {o.label ?? c.open}
                <span className={styles.optionDetail}>
                  {optionDetail(view, conflict, o)}
                </span>
              </span>
            </button>
          ))}
        </div>
        <div className={styles.decision}>
          <label className={styles.label} htmlFor={`${id}-d`}>
            {c.decision}
          </label>
          <textarea
            id={`${id}-d`}
            className={styles.textarea}
            rows={2}
            value={decision}
            onChange={(event) => {
              setDecision(event.target.value);
              setError(false);
            }}
            aria-invalid={error || undefined}
            readOnly={pending}
          />
          {error ? (
            <p className={styles.error} role="alert">
              {c.needsWords}
            </p>
          ) : null}
        </div>
        <footer className={styles.foot}>
          <span className={`mx-meta ${styles.footText}`}>{footer}</span>
          <span className={styles.spacer} />
          <Button variant="quiet" size="sm" kbd="Esc" href={queueHref}>
            {c.back}
          </Button>
          <Button
            variant="keep"
            size="sm"
            kbd="↵"
            onClick={() => void keep()}
            pending={pending}
          >
            {c.keep}
          </Button>
        </footer>
      </section>
    </div>
  );
}
