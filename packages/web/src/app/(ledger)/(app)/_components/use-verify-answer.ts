"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { interpolate } from "@/i18n";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type {
  ForgetPreview,
  StaleWhy,
  VerifyAnswer,
} from "@/lib/v2/data/memories";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { failureText } from "@/lib/v2/records-copy";
import { useSource } from "../_lib/data";
import { useOverlays } from "../_lib/overlays";
import { recordKeys, useAfterDecision } from "../_lib/records";
import { useUndo } from "../_lib/undo";
import type { RecordsView } from "../_places/records-view";
import { useToast } from "./toasts";

/**
 * What VerifyStale holds while the person answers: the answer chosen
 * (1, 2 or 3), the new words for "Update it" (the suggestion, else the
 * words it has), and the submit. One idempotency key per answer across
 * retries; still true and an update go on the undo stack, Forget never.
 */
export function useVerifyAnswer(view: RecordsView, why: StaleWhy) {
  const { space, l, rc, locale } = view;
  const v = l.verify;
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const undo = useUndo();
  const afterDecision = useAfterDecision(space);
  const { closeVerify } = useOverlays();
  const [answer, setAnswer] = useState<VerifyAnswer | null>(
    why.suggestion ? "update" : null,
  );
  const [editing, setEditing] = useState(!why.suggestion);
  const [draft, setDraft] = useState(
    why.suggestion?.statement ?? why.statement,
  );
  const [problem, setProblem] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [keys] = useState(() => new IntentKeys());

  /** The words "Update it" keeps: the suggestion, or what the person wrote. */
  const words =
    !editing && why.suggestion ? why.suggestion.statement : draft.trim();

  const choose = useCallback((next: VerifyAnswer) => {
    setAnswer(next);
    setProblem(null);
  }, []);

  const submit = useCallback(
    async (preview: ForgetPreview | undefined) => {
      if (pending || !answer) return;
      if (answer === "update") {
        if (!words) return setProblem(v.empty);
        if (words === why.statement.trim()) return setProblem(v.unchanged);
      }
      if (answer === "forget" && (!preview || preview.refusal)) return;
      const carries =
        answer === "forget" ? preview!.carries.map((c) => c.ref) : [];
      const intent = intentOf(
        "verify",
        why.ref,
        why.version,
        answer,
        answer === "update" ? words : carries.join(","),
      );
      setPending(true);
      try {
        const result = await source.memories.verify({
          space,
          ref: why.ref,
          version: why.version,
          answer,
          ...(answer === "update" ? { statement: words } : {}),
          ...(answer === "forget" ? { carries } : {}),
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        closeVerify();
        if (answer === "forget") {
          toast({
            state: "forgotten",
            text: interpolate(v.done.forget, { ref: result.ref }),
          });
        } else {
          const entry = result.receipt
            ? undo.record({
                space,
                command: "verify",
                ref: result.ref,
                receipt: result.receipt,
                kept: answer === "update",
                restore: null,
              })
            : null;
          toast({
            state: "kept",
            text: interpolate(
              answer === "update" ? v.done.update : v.done.stillTrue,
              { ref: result.ref },
            ),
            ...(entry ? { undo: () => void undo.run(entry) } : {}),
          });
        }
        afterDecision({ leftQueue: true });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        if (failure.kind === "clash" || failure.kind === "decided") {
          // It changed, or someone answered it first: read it again.
          void queryClient.invalidateQueries({
            queryKey: recordKeys.stale(source.kind, space.slug, why.ref),
          });
        }
        toast({
          state: "proposed",
          text: failureText(rc, failure, {
            command: answer === "forget" ? "forget" : "verify",
            ref: why.ref,
            space: space.name,
            locale,
          }),
          ...(isRetryable(failure)
            ? {
                action: {
                  label: rc.failure.retry,
                  onClick: () => void submit(preview),
                },
              }
            : {}),
        });
      } finally {
        setPending(false);
      }
    },
    [
      afterDecision,
      answer,
      closeVerify,
      keys,
      locale,
      pending,
      queryClient,
      rc,
      source,
      space,
      toast,
      undo,
      v,
      why,
      words,
    ],
  );

  return {
    answer,
    choose,
    editing,
    edit: () => {
      setEditing(true);
      setAnswer("update");
    },
    draft,
    setDraft: (next: string) => {
      setDraft(next);
      setProblem(null);
    },
    words,
    problem,
    pending,
    submit,
  };
}

export type VerifyAnswerState = ReturnType<typeof useVerifyAnswer>;
