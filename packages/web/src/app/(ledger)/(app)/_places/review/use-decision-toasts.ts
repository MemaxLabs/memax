"use client";

import { useCallback, useMemo } from "react";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { isRetryable, type CommandFailure } from "@/lib/v2/data/command-error";
import type { DecisionResult } from "@/lib/v2/data/records";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { failureText, type FailedCommand } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";

/**
 * What a decision says when it lands (States2 toasts, bottom left, one
 * at a time): "Kept M-0430 · 3 files recompiled" when the file count is
 * known, "Kept M-0430" when it isn't, with Undo (⌘Z) when the decision
 * can be undone; and why it didn't go through, with Try again (the same
 * command, the same key) when a retry is safe.
 */
export function useDecisionToasts(space: SpaceSummary) {
  const { t, locale } = useLocale();
  const toast = useToast();
  const copy = t.ledger;

  const kept = useCallback(
    (result: DecisionResult, undo?: () => void) => {
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
      toast({ state: "kept", text, ...(undo ? { undo } : {}) });
    },
    [copy, toast],
  );

  /** A conflict settled from the card: the proposal replaces the decision in force. */
  const keptOver = useCallback(
    (ref: string, other: string, undo?: () => void) =>
      toast({
        state: "kept",
        text: interpolate(copy.review.keptOver, { ref, other }),
        ...(undo ? { undo } : {}),
      }),
    [copy, toast],
  );

  const rejected = useCallback(
    (ref: string, undo?: () => void) =>
      toast({
        state: "off",
        text: interpolate(copy.records.toast.rejected, { ref }),
        ...(undo ? { undo } : {}),
      }),
    [copy, toast],
  );

  /** Keep found the judge had flagged it: it waits as a conflict now. */
  const nowConflict = useCallback(
    (ref: string, other: string) =>
      toast({
        state: "proposed",
        text: interpolate(copy.review.nowConflict, { ref, other }),
      }),
    [copy, toast],
  );

  const failed = useCallback(
    (
      failure: CommandFailure,
      command: FailedCommand,
      ref: string,
      retry: () => void,
    ) =>
      toast({
        state: "proposed",
        text: failureText(copy.records, failure, {
          command,
          ref,
          space: space.name,
          locale,
        }),
        ...(isRetryable(failure)
          ? { action: { label: copy.records.failure.retry, onClick: retry } }
          : {}),
      }),
    [copy, locale, space.name, toast],
  );

  return useMemo(
    () => ({ kept, keptOver, rejected, nowConflict, failed }),
    [kept, keptOver, rejected, nowConflict, failed],
  );
}
