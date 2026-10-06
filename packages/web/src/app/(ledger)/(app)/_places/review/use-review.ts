"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import {
  isRetryable,
  toFailure,
  type CommandFailure,
} from "@/lib/v2/data/command-error";
import type { DecisionResult } from "@/lib/v2/data/records";
import type { ReviewItem } from "@/lib/v2/data/review";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { failureText, type FailedCommand } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import {
  useAfterDecision,
  usePrefetchCards,
  useReviewCard,
  useReviewQueue,
} from "../../_lib/records";
import { INITIAL_REVIEW, reviewReducer, type ReviewMode } from "./review-state";

export const REVIEW_FILTERS = [
  "all",
  "conflicts",
  "external",
  "stale",
] as const;
export type ReviewFilter = (typeof REVIEW_FILTERS)[number];

export function matchesFilter(item: ReviewItem, filter: ReviewFilter): boolean {
  switch (filter) {
    case "all":
      return true;
    case "conflicts":
      return item.state === "conflict";
    case "external":
      return item.external;
    case "stale":
      return item.state === "stale";
  }
}

/**
 * How long a kept card keeps its seal before the next card takes its
 * place, counted from the press: the stamp (280ms) and most of its ink
 * ring (HANDOFF §7). A slower server just advances when it answers.
 */
export const ADVANCE_MS = 600;

const wait = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, Math.max(0, ms)));

type EditingMode = Extract<ReviewMode, { kind: "editing" }>;

/**
 * Review's controller: the queue with this visit's decisions taken out,
 * the selected card, and Keep, Edit then keep, and Reject. Keep is
 * optimistic (the seal stamps on press and rolls back on error), each
 * action has one idempotency key reused across its retries, and every
 * failure says what happened and what to do.
 */
export function useReview(space: SpaceSummary, filter: ReviewFilter) {
  const source = useSource();
  const { t, locale } = useLocale();
  const toast = useToast();
  const afterDecision = useAfterDecision(space);
  const prefetch = usePrefetchCards(space);
  const [state, dispatch] = useReducer(reviewReducer, INITIAL_REVIEW);
  const [keys] = useState(() => new IntentKeys());
  const busy = useRef<string | null>(null);
  const queue = useReviewQueue(space);
  const canDecide = space.role !== "viewer";

  const all = useMemo(
    () =>
      (queue.data?.pages ?? [])
        .flatMap((page) => page.items)
        .filter((item) => !state.done[item.ref]),
    [queue.data, state.done],
  );
  const visible = useMemo(
    () => all.filter((item) => matchesFilter(item, filter)),
    [all, filter],
  );
  const selected =
    visible.find((item) => item.ref === state.selected) ?? visible[0];
  const index = selected ? visible.indexOf(selected) : -1;
  const card = useReviewCard(space, selected);

  useEffect(() => {
    if (index >= 0) prefetch(visible.slice(index + 1, index + 4));
  }, [index, visible, prefetch]);

  const copy = t.ledger;

  const showKept = useCallback(
    (result: DecisionResult) => {
      if (result.outcome === "proposed") {
        toast({
          state: "proposed",
          text: interpolate(copy.app.toast.proposed, { ref: result.ref }),
        });
        return;
      }
      const text =
        result.recompiled === null
          ? interpolate(copy.app.toast.kept, { ref: result.ref })
          : count(
              copy.app.toast.keptRecompiledOne,
              copy.app.toast.keptRecompiled,
              result.recompiled,
              { ref: result.ref },
            );
      // Undo shows only where the source returns an inverse command;
      // none does yet (no server undo), so Review offers no Undo.
      toast({
        state: "kept",
        text,
        ...(result.undo ? { undo: result.undo, undoRef: result.ref } : {}),
      });
    },
    [copy, toast],
  );

  const failed = useCallback(
    (
      failure: CommandFailure,
      command: FailedCommand,
      item: ReviewItem,
      order: readonly string[],
      retry: () => void,
    ) => {
      if (failure.kind === "decided" || failure.kind === "not-found") {
        dispatch({ type: "decided", ref: item.ref, outcome: "gone", order });
        afterDecision({ leftQueue: true });
      } else if (failure.kind === "clash") {
        afterDecision({ leftQueue: false });
      }
      toast({
        state: "proposed",
        text: failureText(copy.records, failure, {
          command,
          ref: item.ref,
          space: space.name,
          locale,
        }),
        ...(isRetryable(failure)
          ? { action: { label: copy.records.failure.retry, onClick: retry } }
          : {}),
      });
    },
    [afterDecision, copy, locale, space.name, toast],
  );

  const keep = useCallback(
    async (item: ReviewItem) => {
      if (!canDecide || item.lifecycle !== "proposed" || busy.current) return;
      busy.current = item.ref;
      const intent = intentOf("keep", item.ref, item.version);
      const order = visible.map((i) => i.ref);
      const started = Date.now();
      dispatch({ type: "seal", ref: item.ref });
      try {
        const result = await source.review.keep({
          space,
          item,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        showKept(result);
        await wait(ADVANCE_MS - (Date.now() - started));
        dispatch({ type: "decided", ref: item.ref, outcome: "kept", order });
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "unseal", ref: item.ref });
        failed(failure, "keep", item, order, () => void keep(item));
      } finally {
        busy.current = null;
      }
    },
    [afterDecision, canDecide, failed, keys, showKept, source, space, visible],
  );

  const reject = useCallback(
    async (item: ReviewItem, reason: string) => {
      if (!canDecide || item.lifecycle !== "proposed" || busy.current) return;
      busy.current = item.ref;
      const why = reason.trim() || undefined;
      const intent = intentOf("reject", item.ref, item.version, why);
      const order = visible.map((i) => i.ref);
      dispatch({ type: "busy", ref: item.ref });
      try {
        const result = await source.review.reject({
          space,
          item,
          reason: why,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        dispatch({
          type: "decided",
          ref: item.ref,
          outcome: "rejected",
          order,
        });
        toast({
          state: "off",
          text: interpolate(copy.records.toast.rejected, { ref: result.ref }),
        });
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "settled", ref: item.ref });
        failed(failure, "reject", item, order, () => void reject(item, reason));
      } finally {
        busy.current = null;
      }
    },
    [
      afterDecision,
      canDecide,
      copy,
      failed,
      keys,
      source,
      space,
      toast,
      visible,
    ],
  );

  const submitEdit = useCallback(
    async (item: ReviewItem, mode: EditingMode) => {
      if (busy.current) return;
      const draft = mode.draft.trim();
      if (!draft) {
        dispatch({ type: "editError", error: "empty" });
        return;
      }
      // The words didn't change: that's a Keep.
      if (draft === mode.base.statement.trim()) return keep(item);
      busy.current = item.ref;
      const why = mode.reason.trim() || undefined;
      const intent = intentOf("edit", item.ref, mode.base.version, draft, why);
      const order = visible.map((i) => i.ref);
      const started = Date.now();
      dispatch({ type: "seal", ref: item.ref, statement: draft });
      try {
        const result = await source.memories.edit({
          space,
          ref: item.ref,
          version: mode.base.version,
          statement: draft,
          reason: why,
          keep: true,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        showKept(result);
        await wait(ADVANCE_MS - (Date.now() - started));
        dispatch({ type: "decided", ref: item.ref, outcome: "kept", order });
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        if (failure.kind === "clash") {
          const theirs = await source.memories
            .latest({ space, ref: item.ref })
            .catch(() => null);
          if (theirs) {
            dispatch({ type: "clash", ref: item.ref, theirs });
            return;
          }
        }
        dispatch({ type: "unseal", ref: item.ref });
        failed(failure, "edit", item, order, () => void submitEdit(item, mode));
      } finally {
        busy.current = null;
      }
    },
    [afterDecision, failed, keep, keys, showKept, source, space, visible],
  );

  const keepMine = useCallback(
    (item: ReviewItem) => {
      const mode = state.mode;
      if (mode.kind !== "clash") return;
      const next: EditingMode = {
        kind: "editing",
        ref: mode.ref,
        base: {
          version: mode.theirs.version,
          statement: mode.theirs.statement,
        },
        draft: mode.mine,
        reason: mode.reason,
        error: null,
      };
      dispatch({ type: "combine" });
      void submitEdit(item, next);
    },
    [state.mode, submitEdit],
  );

  const keepTheirs = useCallback(() => {
    dispatch({ type: "keepTheirs" });
    afterDecision({ leftQueue: false });
  }, [afterDecision]);

  const move = useCallback(
    (delta: number): boolean => {
      if (state.busy || state.sealed || state.mode.kind !== "browse") {
        return false;
      }
      const next = visible[index + delta];
      if (!next) return true;
      dispatch({ type: "select", ref: next.ref });
      return true;
    },
    [index, state.busy, state.mode.kind, state.sealed, visible],
  );

  return {
    queue,
    all,
    visible,
    selected,
    index,
    card,
    state,
    dispatch,
    canDecide,
    keep,
    reject,
    submitEdit,
    keepMine,
    keepTheirs,
    move,
  };
}

export type ReviewController = ReturnType<typeof useReview>;
