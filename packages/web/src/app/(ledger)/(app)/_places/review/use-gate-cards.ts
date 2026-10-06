"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type RefObject,
} from "react";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import {
  answerNeedsWebHere,
  byExpiry,
  gateStatusAt,
  waitingGates,
  type GateAnswerResult,
  type GateEnd,
  type GateView,
} from "@/lib/v2/data/gates";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { useSource } from "../../_lib/data";
import { useAfterGate, useGate, useWaitingGates } from "../../_lib/gates";
import type { ReviewAction } from "./review-state";
import type { useDecisionToasts } from "./use-decision-toasts";

/**
 * What one gate's card shows beyond the gate: the option chosen, which
 * step the footer is at (the choice, the answer's confirmation, or
 * withdrawing's), the command in flight, and the decision an answer
 * became (the seal).
 */
export interface GateCardState {
  /** The chosen option, counting from 0, or -1. */
  choice: number;
  step: "choose" | "confirm" | "withdraw";
  pending: "answer" | "withdraw" | null;
  answered: GateAnswerResult | null;
  /** Answering was refused because it needs the web (D15): the card says so from then on. */
  needsWeb: boolean;
}

export const INITIAL_GATE_CARD: GateCardState = {
  choice: -1,
  step: "choose",
  pending: null,
  answered: null,
  needsWeb: false,
};

const wait = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, Math.max(0, ms)));

/**
 * Review's decision gates (epic 1.11): the waiting ones, listed before
 * the memories, each answered as a choice of one option, then a
 * confirmation that says what happens, through `gates.answer` with one
 * idempotency key per answer (reused across retries) and the gate's
 * version as If-Match. A gate that ended before the command reached it
 * (409 with `details.status`) stays on screen saying how, and leaves the
 * queue when the person moves on; so does one opened from a link
 * (`?gate=`) that isn't waiting any more. Answers can't be undone yet,
 * so nothing goes on the undo stack.
 */
export function useGateCards({
  space,
  dispatch,
  order,
  busy,
  done,
  canDecide,
  toasts,
  advanceMs,
  focus,
}: {
  space: SpaceSummary;
  dispatch: Dispatch<ReviewAction>;
  /** The queue's order as the person sees it, for where the selection goes next. */
  order: RefObject<readonly string[]>;
  /** The ref with a command in flight, shared with Review's memory commands. */
  busy: RefObject<string | null>;
  /** Decided this visit, from Review's state: out of the queue at once. */
  done: Readonly<Record<string, unknown>>;
  canDecide: boolean;
  toasts: ReturnType<typeof useDecisionToasts>;
  /** How long the seal shows before the next card. */
  advanceMs: number;
  /** A gate to open (`?gate=` from Activity or Today), or null. */
  focus: string | null;
}) {
  const source = useSource();
  const query = useWaitingGates(space);
  const afterGate = useAfterGate(space);
  const [keys] = useState(() => new IntentKeys());
  const [cards, setCards] = useState<Record<string, GateCardState>>({});
  // Gates on screen that aren't waiting: ended under a command, or opened
  // from a link. Each leaves when the person moves away from it.
  const [lingering, setLingering] = useState<GateView[]>([]);
  const [left, setLeft] = useState<ReadonlySet<string>>(() => new Set());
  const now = source.now();
  const minute = Math.floor(now.getTime() / 60_000);

  const patch = useCallback(
    (ref: string, change: Partial<GateCardState>) =>
      setCards((all) => ({
        ...all,
        [ref]: { ...(all[ref] ?? INITIAL_GATE_CARD), ...change },
      })),
    [],
  );
  const cardOf = useCallback(
    (ref: string): GateCardState => cards[ref] ?? INITIAL_GATE_CARD,
    [cards],
  );

  const linger = useCallback((gate: GateView) => {
    setLingering((list) => [...list.filter((g) => g.ref !== gate.ref), gate]);
    setLeft((set) => {
      if (!set.has(gate.ref)) return set;
      const next = new Set(set);
      next.delete(gate.ref);
      return next;
    });
  }, []);

  // The queue's gates: the waiting ones, and those lingering, in order.
  const gates = useMemo(() => {
    const at = new Date(minute * 60_000);
    const waiting = waitingGates(query.data ?? [], at);
    const fresh = new Set(waiting.map((g) => g.ref));
    return [...waiting, ...lingering.filter((g) => !fresh.has(g.ref))]
      .filter((g) => !done[g.ref] && !left.has(g.ref))
      .sort(byExpiry);
  }, [done, left, lingering, minute, query.data]);

  /** The person moved away from a gate: a lingering one leaves the queue, a confirmation goes back. */
  const leave = useCallback(
    (ref: string) => {
      if (lingering.some((g) => g.ref === ref)) {
        setLeft((set) => new Set(set).add(ref));
        setLingering((list) => list.filter((g) => g.ref !== ref));
      }
      if (cards[ref] && cards[ref].step !== "choose") {
        patch(ref, { step: "choose" });
      }
    },
    [cards, lingering, patch],
  );

  // A link to a gate: select it, and find it if it isn't waiting here.
  const listed = focus ? gates.some((g) => g.ref === focus) : false;
  const loaded = query.data !== undefined;
  const lookup = useGate(space, focus && loaded && !listed ? focus : null);
  useEffect(() => {
    if (focus) dispatch({ type: "select", ref: focus });
  }, [dispatch, focus]);
  const missing = useRef<string | null>(null);
  useEffect(() => {
    if (!focus || lookup.data === undefined) return;
    if (lookup.data) {
      linger(lookup.data);
      dispatch({ type: "select", ref: lookup.data.ref });
    } else if (missing.current !== focus) {
      missing.current = focus;
      toasts.gateMissing(focus);
    }
  }, [dispatch, focus, linger, lookup.data, toasts]);

  /**
   * A command met a gate that isn't waiting: read how it ended, and keep
   * it on screen saying so. The list and the rail drop it at once.
   */
  const ended = useCallback(
    async (gate: GateView, status: GateEnd | undefined) => {
      const fresh = await source.gates
        .get({ space, ref: gate.ref })
        .catch(() => null);
      const known =
        fresh && gateStatusAt(fresh, source.now()) !== "waiting"
          ? fresh
          : { ...gate, status: status ?? "answered" };
      linger(known);
      afterGate(gate.ref);
    },
    [afterGate, linger, source, space],
  );

  /** ↵ or Answer: choose → confirm, then confirm → answer. */
  const answer = useCallback(
    async (gate: GateView) => {
      const card = cardOf(gate.ref);
      if (!canDecide || busy.current || card.choice < 0) return;
      if (gateStatusAt(gate, source.now()) !== "waiting") return;
      if (
        card.needsWeb ||
        answerNeedsWebHere(gate, source.gates.webSession())
      ) {
        return;
      }
      if (card.step !== "confirm") {
        patch(gate.ref, { step: "confirm" });
        return;
      }
      busy.current = gate.ref;
      const option = card.choice;
      const intent = intentOf("answer", gate.ref, gate.version, String(option));
      const decidedOrder = order.current ?? [];
      const started = Date.now();
      dispatch({ type: "busy", ref: gate.ref });
      patch(gate.ref, { pending: "answer" });
      try {
        const result = await source.gates.answer({
          space,
          gate,
          option,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        patch(gate.ref, { pending: null, answered: result });
        toasts.answered(gate.ref, result);
        // The seal stamps, then the next card (as a Keep does).
        await wait(advanceMs - (Date.now() - started));
        dispatch({
          type: "decided",
          ref: gate.ref,
          outcome: "kept",
          order: decidedOrder,
        });
        afterGate(gate.ref);
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "settled", ref: gate.ref });
        const needsWeb =
          failure.kind === "refused" && failure.code === "decision_needs_web";
        patch(gate.ref, {
          pending: null,
          step: isRetryable(failure) ? "confirm" : "choose",
          ...(needsWeb ? { needsWeb: true } : {}),
        });
        if (failure.kind === "decided" || failure.kind === "not-found") {
          await ended(
            gate,
            failure.kind === "decided" ? failure.status : undefined,
          );
        } else if (failure.kind === "clash") {
          // Its version moved: it ended, or the list was behind.
          await ended(gate, undefined);
        }
        toasts.gateFailed(failure, "answer", gate, () => void answer(gate));
      } finally {
        busy.current = null;
      }
    },
    [
      advanceMs,
      afterGate,
      busy,
      canDecide,
      cardOf,
      dispatch,
      ended,
      keys,
      order,
      patch,
      source,
      space,
      toasts,
    ],
  );

  /** Withdraw, confirmed inline: the first press asks, the second takes the question back. */
  const withdraw = useCallback(
    async (gate: GateView) => {
      const card = cardOf(gate.ref);
      if (!canDecide || busy.current) return;
      if (gateStatusAt(gate, source.now()) !== "waiting") return;
      if (card.step !== "withdraw") {
        patch(gate.ref, { step: "withdraw" });
        return;
      }
      busy.current = gate.ref;
      const intent = intentOf("withdraw", gate.ref, gate.version);
      const decidedOrder = order.current ?? [];
      dispatch({ type: "busy", ref: gate.ref });
      patch(gate.ref, { pending: "withdraw" });
      try {
        await source.gates.withdraw({
          space,
          gate,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        patch(gate.ref, { pending: null, step: "choose" });
        dispatch({
          type: "decided",
          ref: gate.ref,
          outcome: "gone",
          order: decidedOrder,
        });
        toasts.withdrew(gate);
        afterGate(gate.ref);
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "settled", ref: gate.ref });
        patch(gate.ref, {
          pending: null,
          step: isRetryable(failure) ? "withdraw" : "choose",
        });
        if (failure.kind === "decided" || failure.kind === "not-found") {
          await ended(
            gate,
            failure.kind === "decided" ? failure.status : undefined,
          );
        } else if (failure.kind === "clash") {
          await ended(gate, undefined);
        }
        toasts.gateFailed(failure, "withdraw", gate, () => void withdraw(gate));
      } finally {
        busy.current = null;
      }
    },
    [
      afterGate,
      busy,
      canDecide,
      cardOf,
      dispatch,
      ended,
      keys,
      order,
      patch,
      source,
      space,
      toasts,
    ],
  );

  /** 1–4, a click, or the arrows inside the options: choose one. Back to the choice if it was confirming. */
  const choose = useCallback(
    (gate: GateView, index: number): boolean => {
      const card = cardOf(gate.ref);
      if (card.pending || card.answered || index >= gate.options.length) {
        return false;
      }
      if (gateStatusAt(gate, source.now()) !== "waiting") return false;
      if (card.choice !== index || card.step !== "choose") {
        patch(gate.ref, { choice: index, step: "choose" });
      }
      return true;
    },
    [cardOf, patch, source],
  );

  /** Esc, or Cancel: back from a confirmation. */
  const back = useCallback(
    (gate: GateView): boolean => {
      const card = cardOf(gate.ref);
      if (card.step === "choose" || card.pending) return false;
      patch(gate.ref, { step: "choose" });
      return true;
    },
    [cardOf, patch],
  );

  return {
    query,
    gates,
    /** A linked gate is still being looked up. */
    finding:
      Boolean(focus) &&
      !listed &&
      (!loaded || (lookup.data === undefined && !lookup.isError)),
    cardOf,
    choose,
    answer,
    withdraw,
    back,
    leave,
  };
}

export type GateCards = ReturnType<typeof useGateCards>;
