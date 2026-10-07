"use client";

import { useCallback, useState } from "react";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { count } from "@/lib/v2/copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import { targetName, type TargetView } from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";

/**
 * Compile now (⌘⇧S, the Brief's Compile, Today's recompile): asks each
 * target that isn't stopped for a fresh compile. The compile is queued,
 * so each comes back compiling and the list polls until it settles. One
 * key per press, kept for a retry while the request may have landed.
 * The toast is neutral: nothing is kept, it's on its way.
 */
export function useCompile(space: SpaceSummary) {
  const source = useSource();
  const { t } = useLocale();
  const b = t.ledger.brief;
  const toast = useToast();
  const afterCompile = useAfterCompile(space);
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState(false);

  const run = useCallback(
    async (targets: readonly TargetView[]) => {
      const live = targets.filter((target) => target.syncState !== "off");
      if (pending || live.length === 0) return;
      setPending(true);
      const failures = await Promise.all(
        live.map(async (target) => {
          const intent = `compile:${target.id}:${target.lastCompile?.ref ?? ""}`;
          try {
            const next = await source.targets.compile({
              space,
              target,
              idempotencyKey: keys.keyFor(intent),
            });
            keys.settle(intent);
            afterCompile(next);
            return null;
          } catch (err) {
            const failure = toFailure(err);
            if (!isRetryable(failure)) keys.settle(intent);
            return failure;
          }
        }),
      );
      setPending(false);
      const failed = failures.find((f) => f !== null);
      if (failed) {
        afterCompile();
        toast({
          state: "proposed",
          text: interpolate(b.toast.compileFailed, {
            reason: commandReason(t.ledger.records, b, failed, space.name),
          }),
        });
        return;
      }
      toast({
        text:
          live.length === 1
            ? interpolate(b.toast.compilingOne, { file: targetName(live[0]!) })
            : count(b.toast.compilingOne, b.toast.compiling, live.length, {
                file: targetName(live[0]!),
              }),
      });
    },
    [afterCompile, b, keys, pending, source, space, t, toast],
  );

  return { run, pending };
}
