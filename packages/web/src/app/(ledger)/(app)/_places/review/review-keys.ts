"use client";

import { useRouter } from "next/navigation";
import { useHotkey } from "@/lib/v2/keymap/react";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { compareHref } from "./card-column";
import type { ReviewController } from "./use-review";

/**
 * Review's keys, from the registry (review.*): ↓↑ move, K keep, E edit
 * then keep, X reject (with an optional reason), C compare a conflict,
 * O open the source. The keymap already ignores single keys while
 * typing in a field and while an IME composes; a handler that returns
 * false leaves the key alone (nothing to compare, no source to open).
 *
 * ⌘Z (undo) isn't handled here: it undoes only where a command has an
 * inverse, and none does yet, so Review offers no Undo. The toast host
 * takes ⌘Z the day a decision returns one (DecisionResult.undo).
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
    review.dispatch({
      type: "edit",
      ref: item.ref,
      base: { version: item.version, statement: item.statement },
    });
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
}
