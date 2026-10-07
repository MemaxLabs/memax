"use client";

import { useCallback, useMemo } from "react";
import { useRouter } from "next/navigation";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { isRetryable, type CommandFailure } from "@/lib/v2/data/command-error";
import type { GateAnswerResult, GateView } from "@/lib/v2/data/gates";
import type { DecisionResult } from "@/lib/v2/data/records";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { failureText, type FailedCommand } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";
import { useAgentName } from "../../_lib/frame-copy";
import { useSignInAgain } from "../../_lib/sign-in";
import { memoryHref } from "./hrefs";

const DEV = process.env.NODE_ENV !== "production";

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
  const router = useRouter();
  const agentName = useAgentName();
  const signInAgain = useSignInAgain();
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

  /** A gate's answer, kept: the decision it became, a link to it, and no Undo (answers can't be undone yet). */
  const answered = useCallback(
    (gate: string, result: GateAnswerResult) => {
      const g = copy.review.gate;
      const memory = result.memory.ref;
      const text =
        result.recompiled === null
          ? interpolate(g.kept, { memory, ref: gate })
          : count(g.keptRecompiledOne, g.keptRecompiled, result.recompiled, {
              memory,
              ref: gate,
            });
      toast({
        state: "kept",
        text,
        action: {
          label: interpolate(g.openDecision, { memory }),
          onClick: () => router.push(memoryHref(space.slug, memory)),
        },
      });
    },
    [copy, router, space.slug, toast],
  );

  const withdrew = useCallback(
    (gate: GateView) =>
      toast({
        state: "off",
        text: interpolate(copy.review.gate.withdrew, {
          ref: gate.ref,
          agent: agentName(gate.agent),
        }),
      }),
    [agentName, copy, toast],
  );

  /**
   * Why a gate's answer or withdrawal didn't go through. D15's refusal
   * offers Sign in again (with the operator's hint in development).
   */
  const gateFailed = useCallback(
    (
      failure: CommandFailure,
      command: "answer" | "withdraw",
      gate: GateView,
      retry: () => void,
    ) => {
      const needsWeb =
        failure.kind === "refused" && failure.code === "decision_needs_web";
      const text = failureText(copy.records, failure, {
        command,
        ref: gate.ref,
        space: space.name,
        agent: agentName(gate.agent),
        locale,
      });
      toast({
        state: "proposed",
        text:
          needsWeb && DEV
            ? `${text} ${copy.records.failure.needsWebDev}`
            : text,
        ...(needsWeb
          ? {
              action: {
                label: copy.records.failure.signInAgain,
                onClick: signInAgain,
              },
            }
          : isRetryable(failure)
            ? { action: { label: copy.records.failure.retry, onClick: retry } }
            : {}),
      });
    },
    [agentName, copy, locale, signInAgain, space.name, toast],
  );

  /** A link to a gate this space doesn't have. */
  const gateMissing = useCallback(
    (ref: string) =>
      toast({
        text: interpolate(copy.review.gate.notFound, {
          ref,
          space: space.name,
        }),
      }),
    [copy, space.name, toast],
  );

  return useMemo(
    () => ({
      kept,
      keptOver,
      rejected,
      nowConflict,
      failed,
      answered,
      withdrew,
      gateFailed,
      gateMissing,
    }),
    [
      kept,
      keptOver,
      rejected,
      nowConflict,
      failed,
      answered,
      withdrew,
      gateFailed,
      gateMissing,
    ],
  );
}
