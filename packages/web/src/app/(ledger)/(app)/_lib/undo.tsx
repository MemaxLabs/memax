"use client";

import { createContext, use, useCallback, useMemo, useState } from "react";
import { useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type { ReviewQueue } from "@/lib/v2/data/review";
import type { SpaceOverview, SpaceSummary } from "@/lib/v2/data/types";
import { undoFailureText, undoneText } from "@/lib/v2/records-copy";
import {
  undoStack,
  type UndoEntry,
  type UndoInput,
  type UndoStack,
} from "@/lib/v2/undo-stack";
import { useToast } from "../_components/toasts";
import { ledgerQueryKeys, useSource } from "./data";
import { recordKeys } from "./records";

/**
 * Undo in the frame: the per-tab stack (lib/v2/undo-stack.ts) and what
 * running one means on screen. An undo of a Review decision puts the
 * card back in the queue at once (optimistic, with the overview's count),
 * calls `source.undo` with the decision's receipt, and says what was
 * undone; a refusal or a dropped connection takes the card out again and
 * says why. Its toasts carry no state mark: nothing was kept, and
 * nothing waits on the person.
 */

const UndoStackContext = createContext<UndoStack>(undoStack);

/** Tests give each render its own stack. */
export const UndoStackProvider = UndoStackContext;

export function useUndoStack(): UndoStack {
  return use(UndoStackContext);
}

type QueueData = InfiniteData<ReviewQueue, string | undefined>;

/** The queue with a decided card put back where it sat. */
export function withRestored(
  data: QueueData | undefined,
  restore: NonNullable<UndoEntry["restore"]>,
): QueueData | undefined {
  const first = data?.pages[0];
  if (!data || !first) return data;
  if (data.pages.some((p) => p.items.some((i) => i.ref === restore.item.ref))) {
    return data;
  }
  const items = [...first.items];
  items.splice(Math.min(restore.index, items.length), 0, restore.item);
  return {
    ...data,
    pages: [
      { ...first, items, total: first.total + 1 },
      ...data.pages.slice(1),
    ],
  };
}

export function useUndo() {
  const stack = useUndoStack();
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const { t } = useLocale();
  const rc = t.ledger.records;

  /** Puts a decision on the stack, so ⌘Z and its toast can undo it. */
  const record = useCallback(
    (input: Omit<UndoInput, "source">) =>
      stack.push({ ...input, source: source.kind }),
    [stack, source.kind],
  );

  const refresh = useCallback(
    (space: SpaceSummary) => {
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
      void queryClient.invalidateQueries({
        queryKey: ["v2", source.kind, "activity", space.slug],
      });
    },
    [queryClient, source.kind],
  );

  // A fold's Undo isn't on the stack, so its status can't guard a second
  // press; this does, for both.
  const [running] = useState(() => new Set<string>());

  const run = useCallback(
    async (entry: UndoEntry): Promise<void> => {
      const current = stack.get(entry.id) ?? entry;
      if (current.status !== "done" || running.has(entry.id)) return;
      running.add(entry.id);
      stack.update(entry.id, { status: "undoing" });
      const { space, restore } = entry;
      const queueKey = recordKeys.queue(source.kind, space.slug);
      const overviewKey = ledgerQueryKeys.overview(source.kind, space.slug);
      const waiting = (delta: number) =>
        queryClient.setQueryData<SpaceOverview>(
          overviewKey,
          (o) => o && { ...o, waiting: Math.max(0, o.waiting + delta) },
        );
      let previous: QueueData | undefined;
      if (restore) {
        await queryClient.cancelQueries({ queryKey: queueKey, exact: true });
        previous = queryClient.getQueryData<QueueData>(queueKey);
        queryClient.setQueryData<QueueData>(queueKey, (data) =>
          withRestored(data, restore),
        );
        waiting(1);
        stack.emit({ type: "restore", entry });
      }
      try {
        await source.undo({
          space,
          receipt: entry.receipt,
          idempotencyKey: entry.key,
        });
        stack.update(entry.id, { status: "undone" });
        toast({
          text: undoneText(rc, entry.command, entry.ref, { kept: entry.kept }),
        });
      } catch (err) {
        const failure = toFailure(err);
        if (restore) {
          if (previous) queryClient.setQueryData(queueKey, previous);
          waiting(-1);
          stack.emit({ type: "rollback", entry });
        }
        const retry = isRetryable(failure);
        // A refusal is final: another ⌘Z moves on to the decision before it.
        if (retry) stack.update(entry.id, { status: "done" });
        else stack.drop(entry.id);
        toast({
          text: undoFailureText(rc, failure, {
            command: entry.command,
            ref: entry.ref,
          }),
          ...(retry
            ? {
                action: {
                  label: rc.failure.retry,
                  onClick: () => void run({ ...entry, status: "done" }),
                },
              }
            : {}),
        });
      } finally {
        running.delete(entry.id);
        refresh(space);
      }
    },
    [queryClient, rc, refresh, running, source, stack, toast],
  );

  /** ⌘Z: the newest decision in the space still inside its window. False when there's none. */
  const undoLatest = useCallback(
    (space: SpaceSummary): boolean => {
      const entry = stack.latest(source.kind, space.slug);
      if (!entry) return false;
      void run(entry);
      return true;
    },
    [run, source.kind, stack],
  );

  /**
   * Undo one of the judge's folds (not the person's own, so never on the
   * stack): any person who may keep, for 14 days. The key is the fold's,
   * so a retry is the same command.
   */
  const unfold = useCallback(
    (space: SpaceSummary, ref: string, receipt: string) =>
      run({
        id: `fold:${receipt}`,
        source: source.kind,
        space,
        command: "fold",
        ref,
        receipt,
        at: Date.now(),
        restore: null,
        status: "done",
        key: `unfold:${receipt}`,
      }),
    [run, source.kind],
  );

  return useMemo(
    () => ({ stack, record, run, undoLatest, unfold }),
    [stack, record, run, undoLatest, unfold],
  );
}

export type UndoController = ReturnType<typeof useUndo>;
