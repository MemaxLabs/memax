"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { useRouter } from "next/navigation";
import {
  isRetryable,
  toFailure,
  type CommandFailure,
} from "@/lib/v2/data/command-error";
import type { DecisionResult } from "@/lib/v2/data/records";
import type { ReviewItem } from "@/lib/v2/data/review";
import type { SpaceSummary } from "@/lib/v2/data/types";
import type { UndoCommand } from "@/lib/v2/data/undo";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import type { FailedCommand } from "@/lib/v2/records-copy";
import { useSource } from "../../_lib/data";
import {
  useAfterDecision,
  usePatchQueueItem,
  usePrefetchCards,
  useReviewCard,
  useReviewQueue,
} from "../../_lib/records";
import { useUndo } from "../../_lib/undo";
import { compareHref } from "./hrefs";
import { INITIAL_REVIEW, reviewReducer, type ReviewMode } from "./review-state";
import { useDecisionToasts } from "./use-decision-toasts";

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

/**
 * How long Keep waits for the judge at most. The server answers `busy`
 * for at most 30 s after a version is written, then keeps anyway (so a
 * judge that's down never blocks Review); past this, say what happened.
 */
export const JUDGE_WAIT_MS = 45_000;

/** Retry-After, within reason: at least a second, at most five. */
export function judgeRetryMs(retryAfter: number | null): number {
  return Math.min(5, Math.max(1, retryAfter ?? 1)) * 1000;
}

const wait = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, Math.max(0, ms)));

/** Resolves true after `ms`, false as soon as `signal` aborts. */
function pause(ms: number, signal: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    if (signal.aborted) return resolve(false);
    const timer = setTimeout(() => resolve(true), ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve(false);
      },
      { once: true },
    );
  });
}

type EditingMode = Extract<ReviewMode, { kind: "editing" }>;

/**
 * Review's controller: the queue with this visit's decisions taken out,
 * the selected card, and Keep, Edit then keep, and Reject. Keep is
 * optimistic (the seal stamps on press and rolls back on error), each
 * action has one idempotency key reused across its retries, and every
 * failure says what happened and what to do.
 *
 * With the judge (plan §5.8): a Keep the server answers `busy` (a
 * proposal that touches a decision in force, before the judge has run)
 * neither rolls back nor errors. The card wears the working mark and
 * retries after Retry-After, until it's kept or the judge flags it, and
 * moving away or Esc stops waiting. Keep on a flagged proposal settles
 * the conflict in its favour ("Keep, replace old"); E on one compares.
 * Every decision goes on the tab's undo stack with its receipt.
 */
export function useReview(space: SpaceSummary, filter: ReviewFilter) {
  const source = useSource();
  const router = useRouter();
  const toasts = useDecisionToasts(space);
  const undo = useUndo();
  const afterDecision = useAfterDecision(space);
  const patchItem = usePatchQueueItem(space);
  const prefetch = usePrefetchCards(space);
  const [state, dispatch] = useReducer(reviewReducer, INITIAL_REVIEW);
  const [keys] = useState(() => new IntentKeys());
  const busy = useRef<string | null>(null);
  const waitFor = useRef<AbortController | null>(null);
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
  const order = useRef<readonly string[]>([]);
  order.current = visible.map((i) => i.ref);

  useEffect(() => {
    if (index >= 0) prefetch(visible.slice(index + 1, index + 4));
  }, [index, visible, prefetch]);

  // Undo puts a card back (and takes it out again if the undo fails).
  const { stack } = undo;
  useEffect(
    () =>
      stack.watch((event) => {
        const ref = event.entry.restore?.item.ref;
        if (!ref || event.entry.space.slug !== space.slug) return;
        if (event.entry.source !== source.kind) return;
        if (event.type === "restore") {
          // Undoing an edit saved for the judge: its Keep stops waiting.
          waitFor.current?.abort();
          waitFor.current = null;
          dispatch({ type: "restore", ref });
        } else {
          dispatch({
            type: "decided",
            ref,
            outcome: "gone",
            order: order.current,
          });
        }
      }),
    [source.kind, space.slug, stack],
  );

  // A wait for the judge never outlives the screen.
  useEffect(() => () => waitFor.current?.abort(), []);

  /** Where the card sat in the cached queue, for Undo's restore. */
  const restoreOf = useCallback(
    (item: ReviewItem) => {
      const items = queue.data?.pages[0]?.items ?? [];
      const at = items.findIndex((i) => i.ref === item.ref);
      return { item, index: at < 0 ? 0 : at };
    },
    [queue.data],
  );

  /** Puts a decision on the undo stack, when it has a receipt to undo by. */
  const remember = useCallback(
    (
      result: DecisionResult,
      command: UndoCommand,
      item: ReviewItem,
      kept = false,
    ) =>
      result.receipt
        ? undo.record({
            space,
            command,
            ref: item.ref,
            kept,
            receipt: result.receipt,
            restore: restoreOf(item),
          })
        : null,
    [restoreOf, space, undo],
  );

  /** A failure: drop what was decided elsewhere, refetch a clash, then say why. */
  const failed = useCallback(
    (
      failure: CommandFailure,
      command: FailedCommand,
      item: ReviewItem,
      decidedOrder: readonly string[],
      retry: () => void,
    ) => {
      if (failure.kind === "decided" || failure.kind === "not-found") {
        dispatch({
          type: "decided",
          ref: item.ref,
          outcome: "gone",
          order: decidedOrder,
        });
        afterDecision({ leftQueue: true });
      } else if (failure.kind === "clash") {
        afterDecision({ leftQueue: false });
      }
      toasts.failed(failure, command, item.ref, retry);
    },
    [afterDecision, toasts],
  );

  /**
   * Keep refused as in conflict (409 `in_conflict`): the judge flagged it,
   * so it's still waiting, as a conflict with the decision the server
   * names. Only without that name does Review ask for the memory again.
   */
  const settledAsConflict = useCallback(
    async (item: ReviewItem, other: string | null) => {
      const now = other
        ? { ...item, state: "conflict" as const, conflictsWith: other }
        : await source.review.item({ space, ref: item.ref }).catch(() => null);
      if (now) patchItem({ ...now, judge: null });
      afterDecision({ leftQueue: false });
      if (now?.conflictsWith) {
        toasts.nowConflict(now.ref, now.conflictsWith);
      }
    },
    [afterDecision, patchItem, source, space, toasts],
  );

  /** Esc, or moving away: a Keep waiting for the judge stops waiting. */
  const stopWaiting = useCallback(() => {
    const controller = waitFor.current;
    if (!controller) return false;
    controller.abort();
    waitFor.current = null;
    if (state.waiting) dispatch({ type: "stopWaiting", ref: state.waiting });
    return true;
  }, [state.waiting]);

  /** Keep on a flagged proposal: it replaces the decision in force. */
  const keepOver = useCallback(
    async (item: ReviewItem, other: string) => {
      busy.current = item.ref;
      const intent = intentOf("resolve", item.ref, item.version, "proposal");
      const decidedOrder = order.current;
      const started = Date.now();
      dispatch({ type: "seal", ref: item.ref });
      try {
        const result = await source.review.resolveConflict({
          space,
          ref: item.ref,
          other,
          version: item.version,
          option: "proposal",
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        const entry = remember(result, "resolve", item);
        toasts.keptOver(
          item.ref,
          other,
          entry ? () => void undo.run(entry) : undefined,
        );
        await wait(ADVANCE_MS - (Date.now() - started));
        if (entry && stack.get(entry.id)?.status !== "done") return;
        dispatch({
          type: "decided",
          ref: item.ref,
          outcome: "kept",
          order: decidedOrder,
        });
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "unseal", ref: item.ref });
        failed(
          failure,
          "resolve",
          item,
          decidedOrder,
          () => void keepOver(item, other),
        );
      } finally {
        busy.current = null;
      }
    },
    [afterDecision, failed, keys, remember, source, space, stack, toasts, undo],
  );

  /**
   * Keep, waiting for the judge when the server says it hasn't looked yet.
   * `afterEdit`: an edit, then keep, whose words were saved for the judge;
   * the card starts in the working state rather than under the seal.
   */
  const keep = useCallback(
    async (item: ReviewItem, { afterEdit = false } = {}) => {
      if (!canDecide || item.lifecycle !== "proposed" || busy.current) return;
      if (item.state === "conflict" && item.conflictsWith) {
        return keepOver(item, item.conflictsWith);
      }
      busy.current = item.ref;
      const intent = intentOf("keep", item.ref, item.version);
      const decidedOrder = order.current;
      const controller = new AbortController();
      waitFor.current = controller;
      let started = Date.now();
      let waited = afterEdit;
      dispatch({ type: afterEdit ? "waiting" : "seal", ref: item.ref });
      try {
        const deadline = Date.now() + JUDGE_WAIT_MS;
        let result: DecisionResult;
        for (;;) {
          try {
            result = await source.review.keep({
              space,
              item,
              idempotencyKey: keys.keyFor(intent),
            });
            break;
          } catch (err) {
            const failure = toFailure(err);
            if (failure.kind !== "busy" || Date.now() > deadline) throw err;
            if (controller.signal.aborted) return;
            // The judge hasn't looked yet (judge_pending): wait for it, with
            // the working mark, and ask again with the same key. Another
            // change holding the memory (busy) is a moment's wait under the
            // seal.
            if (failure.judge && !waited) {
              waited = true;
              dispatch({ type: "waiting", ref: item.ref });
            }
            const go = await pause(
              judgeRetryMs(failure.retryAfter),
              controller.signal,
            );
            if (!go) return;
          }
        }
        keys.settle(intent);
        const entry = remember(result, "keep", item);
        toasts.kept(result, entry ? () => void undo.run(entry) : undefined);
        if (controller.signal.aborted) {
          // Kept while the person had moved on: it leaves the queue quietly.
          dispatch({
            type: "decided",
            ref: item.ref,
            outcome: "kept",
            order: decidedOrder,
          });
          afterDecision({ leftQueue: true });
          return;
        }
        if (waited) {
          // The check is done: now the seal stamps.
          started = Date.now();
          dispatch({ type: "seal", ref: item.ref });
        }
        await wait(ADVANCE_MS - (Date.now() - started));
        if (entry && stack.get(entry.id)?.status !== "done") return;
        dispatch({
          type: "decided",
          ref: item.ref,
          outcome: "kept",
          order: decidedOrder,
        });
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "unseal", ref: item.ref });
        dispatch({ type: "stopWaiting", ref: item.ref });
        if (failure.kind === "in-conflict") {
          await settledAsConflict(item, failure.with);
          return;
        }
        failed(failure, "keep", item, decidedOrder, () => void keep(item));
      } finally {
        busy.current = null;
        if (waitFor.current === controller) waitFor.current = null;
      }
    },
    [
      afterDecision,
      canDecide,
      failed,
      keepOver,
      keys,
      remember,
      settledAsConflict,
      source,
      space,
      stack,
      toasts,
      undo,
    ],
  );

  const reject = useCallback(
    async (item: ReviewItem, reason: string) => {
      if (!canDecide || item.lifecycle !== "proposed" || busy.current) return;
      busy.current = item.ref;
      const why = reason.trim() || undefined;
      const intent = intentOf("reject", item.ref, item.version, why);
      const decidedOrder = order.current;
      dispatch({ type: "busy", ref: item.ref });
      try {
        const result = await source.review.reject({
          space,
          item,
          reason: why,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        const entry = remember(result, "reject", item);
        dispatch({
          type: "decided",
          ref: item.ref,
          outcome: "rejected",
          order: decidedOrder,
        });
        toasts.rejected(
          result.ref,
          entry ? () => void undo.run(entry) : undefined,
        );
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        dispatch({ type: "settled", ref: item.ref });
        failed(
          failure,
          "reject",
          item,
          decidedOrder,
          () => void reject(item, reason),
        );
      } finally {
        busy.current = null;
      }
    },
    [
      afterDecision,
      canDecide,
      failed,
      keys,
      remember,
      source,
      space,
      toasts,
      undo,
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
      const decidedOrder = order.current;
      const started = Date.now();
      // An edit saved for the judge: Review keeps the new version next.
      let saved: ReviewItem | null = null;
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
        if (result.judgePending) {
          // Rule 11: the new words touch a decision in force, so the edit is
          // saved (undoable as an edit) and the Keep waits for the judge.
          remember(result, "edit", item);
          saved = {
            ...item,
            statement: draft,
            version: result.version ?? item.version + 1,
            judge: "working",
          };
          patchItem(saved);
          dispatch({ type: "waiting", ref: item.ref });
        } else {
          const entry = remember(
            result,
            "edit",
            item,
            result.outcome === "kept",
          );
          toasts.kept(result, entry ? () => void undo.run(entry) : undefined);
          await wait(ADVANCE_MS - (Date.now() - started));
          if (!entry || stack.get(entry.id)?.status === "done") {
            dispatch({
              type: "decided",
              ref: item.ref,
              outcome: "kept",
              order: decidedOrder,
            });
            afterDecision({ leftQueue: true });
          }
        }
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
        failed(
          failure,
          "edit",
          item,
          decidedOrder,
          () => void submitEdit(item, mode),
        );
      } finally {
        busy.current = null;
      }
      // Then a plain Keep of the saved version, with its own key, waiting
      // for the judge (Esc stops it; the edit stays saved).
      if (saved) await keep(saved, { afterEdit: true });
    },
    [
      afterDecision,
      failed,
      keep,
      keys,
      patchItem,
      remember,
      source,
      space,
      stack,
      toasts,
      undo,
    ],
  );

  /** E: edit, then keep. A conflict's words are settled where both sides show. */
  const edit = useCallback(
    (item: ReviewItem) => {
      if (!canDecide || item.lifecycle !== "proposed") return;
      const href =
        item.state === "conflict" ? compareHref(space.slug, item) : null;
      if (href) {
        router.push(href);
        return;
      }
      dispatch({
        type: "edit",
        ref: item.ref,
        base: { version: item.version, statement: item.statement },
      });
    },
    [canDecide, router, space.slug],
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

  /** Selects a card; a Keep waiting for the judge stops waiting. */
  const select = useCallback(
    (ref: string) => {
      stopWaiting();
      dispatch({ type: "select", ref });
    },
    [stopWaiting],
  );

  const move = useCallback(
    (delta: number): boolean => {
      const waitingHere = Boolean(state.waiting);
      if (
        (!waitingHere && (state.busy || state.sealed)) ||
        state.mode.kind !== "browse"
      ) {
        return false;
      }
      const next = visible[index + delta];
      if (!next) return true;
      select(next.ref);
      return true;
    },
    [
      index,
      select,
      state.busy,
      state.mode.kind,
      state.sealed,
      state.waiting,
      visible,
    ],
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
    edit,
    submitEdit,
    keepMine,
    keepTheirs,
    select,
    move,
    stopWaiting,
    undo,
  };
}

export type ReviewController = ReturnType<typeof useReview>;
