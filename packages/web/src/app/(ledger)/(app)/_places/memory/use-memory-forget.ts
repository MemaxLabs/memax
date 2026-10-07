"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { interpolate, useLocale } from "@/i18n";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type { ForgetPreview, MemoryRecord } from "@/lib/v2/data/memories";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { failureText } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { recordKeys, useAfterDecision } from "../../_lib/records";

/**
 * Forget on a memory's page (the States board): the Forget button opens
 * an inline confirmation, which reads what the Forget would do (the
 * preview: what goes with it, the files, the agents) and forgets with
 * exactly that, from the version on screen (If-Match), one idempotency
 * key per confirmation across retries. Forget has no key and no Undo.
 * When an agent asked to forget it, Keep it declines the request.
 */
export function useMemoryForget(space: SpaceSummary, record: MemoryRecord) {
  const source = useSource();
  const queryClient = useQueryClient();
  const { t, locale } = useLocale();
  const rc = t.ledger.records;
  const p = t.ledger.memory.page;
  const toast = useToast();
  const afterDecision = useAfterDecision(space);
  const [confirming, setConfirming] = useState(false);
  const [note, setNote] = useState("");
  const [pending, setPending] = useState(false);
  const [keys] = useState(() => new IntentKeys());

  const open = useCallback(() => {
    if (record.lifecycle === "forgotten") return;
    setNote("");
    setConfirming(true);
  }, [record.lifecycle]);

  const cancel = useCallback(() => {
    if (!pending) setConfirming(false);
  }, [pending]);

  const submit = useCallback(
    async (preview: ForgetPreview) => {
      if (pending || preview.refusal) return;
      const carries = preview.carries.map((c) => c.ref);
      const why = note.trim() || undefined;
      const intent = intentOf(
        "forget",
        record.ref,
        preview.version,
        carries.join(","),
        why,
      );
      setPending(true);
      try {
        const result = await source.memories.forget({
          space,
          ref: record.ref,
          version: preview.version,
          carries,
          note: why,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        setConfirming(false);
        toast({
          state: "forgotten",
          text: interpolate(p.forgetConfirm.done, { ref: result.ref }),
        });
        afterDecision({ leftQueue: record.lifecycle === "proposed" });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        if (failure.kind === "carries" || failure.kind === "clash") {
          // What it takes changed: read the preview again before asking.
          void queryClient.invalidateQueries({
            queryKey: recordKeys.memory(source.kind, space.slug, record.ref),
          });
        }
        toast({
          state: "proposed",
          text: failureText(rc, failure, {
            command: "forget",
            ref: record.ref,
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
      keys,
      locale,
      note,
      p,
      pending,
      queryClient,
      rc,
      record.lifecycle,
      record.ref,
      source,
      space,
      toast,
    ],
  );

  const decline = useCallback(async () => {
    if (pending) return;
    const intent = intentOf("decline-forget", record.ref, record.version);
    setPending(true);
    try {
      await source.memories.declineForget({
        space,
        ref: record.ref,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      toast({
        state: "kept",
        text: interpolate(p.forgetRequest.kept, { ref: record.ref }),
      });
      afterDecision({ leftQueue: false });
    } catch (err) {
      const failure = toFailure(err);
      if (!isRetryable(failure)) keys.settle(intent);
      toast({
        state: "proposed",
        text: failureText(rc, failure, {
          command: "declineForget",
          ref: record.ref,
          space: space.name,
          locale,
        }),
      });
    } finally {
      setPending(false);
    }
  }, [
    afterDecision,
    keys,
    locale,
    p,
    pending,
    rc,
    record.ref,
    record.version,
    source,
    space,
    toast,
  ]);

  return { confirming, note, setNote, pending, open, cancel, submit, decline };
}

export type MemoryForget = ReturnType<typeof useMemoryForget>;
