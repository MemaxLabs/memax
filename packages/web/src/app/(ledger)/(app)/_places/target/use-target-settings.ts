"use client";

import { useCallback, useState } from "react";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import {
  targetName,
  type Delivery,
  type TargetChange,
  type TargetSettingsView,
  type TargetView,
} from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";

/** What the sidebar shows: the target's settings with any change still in flight on top. */
export interface ShownSettings extends TargetSettingsView {
  delivery: Delivery;
}

export function shownSettings(
  target: TargetView,
  optimistic: TargetChange | null,
): ShownSettings {
  return {
    ...target.settings,
    ...optimistic?.settings,
    delivery: optimistic?.delivery ?? target.delivery,
  };
}

/**
 * "How this file is written" (TargetPreview.png): each change applies at
 * once, through PATCH /v2/targets/{target} with If-Match on the version
 * on screen and one idempotency key per change (the same on a retry).
 * The control shows the new value while the request runs and goes back
 * if it fails; a clash refreshes the target and says someone changed it
 * first. The target recompiles, so the file on screen follows.
 */
export function useTargetSettings(space: SpaceSummary, target: TargetView) {
  const source = useSource();
  const { t } = useLocale();
  const b = t.ledger.brief;
  const toast = useToast();
  const afterCompile = useAfterCompile(space);
  const [keys] = useState(() => new IntentKeys());
  const [optimistic, setOptimistic] = useState<TargetChange | null>(null);

  const change = useCallback(
    async (next: TargetChange): Promise<boolean> => {
      if (optimistic) return false;
      const intent = intentOf(
        "configure",
        target.id,
        target.version,
        JSON.stringify(next),
      );
      const file = targetName(target);
      setOptimistic(next);
      try {
        const updated = await source.targets.configure({
          space,
          target,
          change: next,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        afterCompile(updated);
        toast({ text: interpolate(b.toast.changed, { file }) });
        return true;
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        afterCompile();
        toast({
          state: "proposed",
          text:
            failure.kind === "clash"
              ? interpolate(b.target.settings.clash, { file })
              : interpolate(b.target.settings.failed, {
                  file,
                  reason: commandReason(
                    t.ledger.records,
                    b,
                    failure,
                    space.name,
                  ),
                }),
        });
        return false;
      } finally {
        setOptimistic(null);
      }
    },
    [afterCompile, b, keys, optimistic, source, space, t, target, toast],
  );

  return {
    settings: shownSettings(target, optimistic),
    pending: optimistic !== null,
    change,
  };
}
