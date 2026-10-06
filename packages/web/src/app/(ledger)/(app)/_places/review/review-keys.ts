"use client";

import { useRouter } from "next/navigation";
import { useHotkey } from "@/lib/v2/keymap/react";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { compareHref } from "./hrefs";
import type { ReviewController } from "./use-review";

/**
 * Review's keys, from the registry (review.*): ↓↑ move, K keep, E edit
 * then keep (a conflict's E compares), X reject (with an optional
 * reason), C compare a conflict, O open the source, ⌘Z undo the last
 * decision still inside its window, and Esc stop waiting for the judge.
 * The keymap already ignores single keys while typing in a field and
 * while an IME composes; a handler that returns false leaves the key
 * alone (nothing to compare, no source to open, nothing to undo).
 *
 * ⌘Z here undoes from the tab's undo stack, so it reaches a decision
 * whose toast has gone; while a decision toast shows, it's the same
 * decision. Outside Review, a toast's own ⌘Z undoes its command.
 */
export function useReviewKeys(review: ReviewController, space: SpaceSummary) {
  const router = useRouter();
  const item = review.selected;
  const browsing =
    review.state.mode.kind === "browse" &&
    !review.state.busy &&
    !review.state.sealed;
  const proposal = browsing && item?.lifecycle === "proposed";

  useHotkey("review.move", (_, { index }) => {
    // The registry lists ↓ first, then ↑.
    if (!review.move(index === 0 ? 1 : -1)) return false;
  });

  useHotkey("review.keep", () => {
    if (!proposal || !item || !review.canDecide) return false;
    void review.keep(item);
  });

  useHotkey("review.edit", () => {
    if (!proposal || !item || !review.canDecide) return false;
    review.edit(item);
  });

  useHotkey("review.reject", () => {
    if (!proposal || !item || !review.canDecide) return false;
    review.dispatch({ type: "reject", ref: item.ref });
  });

  useHotkey("review.compare", () => {
    const href = item && browsing ? compareHref(space.slug, item) : null;
    if (!href) return false;
    router.push(href);
  });

  useHotkey("review.source", () => {
    const url = review.card.data?.sourceUrl;
    if (!url || !browsing) return false;
    window.open(url, "_blank", "noopener,noreferrer");
  });

  useHotkey("undo", () => {
    if (!review.canDecide || !review.undo.undoLatest(space)) return false;
  });

  useHotkey("review.stopWaiting", () => {
    if (!review.state.waiting) return false;
    review.stopWaiting();
  });

  // A decision gate's card: 1–4 choose, ↵ asks to confirm and ↵ again
  // answers, Esc goes back. The arrows choose too, inside the options
  // (DecisionGate's radio group); outside them they move the queue.
  const gate = review.selectedGate;
  const gates = review.gateCards;
  const idle = !review.state.busy && !review.state.sealed;

  useHotkey("gate.choose", (_, { index }) => {
    if (!gate || !idle || !review.canDecide) return false;
    if (!gates.choose(gate, index)) return false;
  });

  useHotkey("gate.answer", (event) => {
    if (!gate || !idle || !review.canDecide) return false;
    if (!answersHere(event.target)) return false;
    const card = gates.cardOf(gate.ref);
    // Withdrawing is confirmed with its own button, never a stray ↵.
    if (card.choice < 0 || card.step === "withdraw") return false;
    void gates.answer(gate);
  });

  useHotkey("gate.cancel", () => {
    if (!gate || !gates.back(gate)) return false;
  });
}

/**
 * Whether ↵ here is the gate's: the page, an option, a queue row, or the
 * confirmation's Answer. Any other focused button or field keeps its own
 * ↵ (Cancel stays Cancel).
 */
function answersHere(target: unknown): boolean {
  if (!(target instanceof HTMLElement)) return true;
  if (target.closest(".mx-gate-opt, .mx-row-link, [data-gate-answer]")) {
    return true;
  }
  return !target.closest("button, a, input, textarea, select, [role=menu]");
}
