"use client";

import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { count } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import {
  targetName,
  type DriftMode,
  type DriftResolution,
  type TargetView,
} from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { placeHref } from "@/lib/v2/places";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";

/**
 * Pull, Overwrite and Stop compiling for a drifted target (the Brief's
 * notice and DriftResolve): one idempotency key per choice, reused if
 * it's tried again; then the target the server returned goes in the
 * list and the space refreshes. A pull's toast offers Review, where the
 * proposals arrive; it waits on the person, so it wears ochre.
 */
export function useDriftActions(space: SpaceSummary, target: TargetView) {
  const source = useSource();
  const { t } = useLocale();
  const b = t.ledger.brief;
  const toast = useToast();
  const router = useRouter();
  const afterCompile = useAfterCompile(space);
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState<DriftMode | null>(null);
  const [result, setResult] = useState<DriftResolution | null>(null);

  const run = useCallback(
    async (mode: DriftMode): Promise<DriftResolution | null> => {
      if (pending) return null;
      const intent = intentOf(`drift.${mode}`, target.id, target.version);
      setPending(mode);
      try {
        const resolved = await source.targets.resolveDrift({
          space,
          target,
          mode,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        afterCompile(resolved.target);
        setResult(resolved);
        const file = targetName(target);
        if (mode === "pull") {
          const n = resolved.proposals.length;
          toast(
            n === 0
              ? { text: b.toast.pulledNone }
              : {
                  state: "proposed",
                  text: count(b.toast.pulledOne, b.toast.pulled, n),
                  action: {
                    label: b.toast.openReview,
                    onClick: () => router.push(placeHref(space.slug, "review")),
                  },
                },
          );
        } else if (mode === "overwrite") {
          toast({ text: interpolate(b.toast.overwritten, { file }) });
        } else {
          toast({
            state: "off",
            text: interpolate(b.toast.stopped, { file }),
          });
        }
        return resolved;
      } catch (err) {
        const failure = toFailure(err);
        if (failure.kind !== "unreachable" && failure.kind !== "rate-limited") {
          keys.settle(intent);
        }
        if (failure.kind === "decided") afterCompile();
        toast({
          state: "proposed",
          text: interpolate(b.toast.resolveFailed, {
            reason: commandReason(t.ledger.records, b, failure, space.name),
          }),
        });
        return null;
      } finally {
        setPending(null);
      }
    },
    [afterCompile, b, keys, pending, router, source, space, t, target, toast],
  );

  return { run, pending, result };
}
