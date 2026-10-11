"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { interpolate, useLocale } from "@/i18n";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type { MemoryRecord } from "@/lib/v2/data/memories";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { failureText } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { recordKeys } from "../../_lib/records";

/**
 * Apply everywhere on a memory's page: a personal fact init tied to one
 * repository (scope.repos) is widened to every project, from the version
 * on screen (If-Match), one idempotency key per press across retries. The
 * words don't change; home files pick it up on their next compile.
 */
export function useMemoryScope(space: SpaceSummary, record: MemoryRecord) {
  const source = useSource();
  const queryClient = useQueryClient();
  const { t, locale } = useLocale();
  const rc = t.ledger.records;
  const p = t.ledger.memory.page;
  const toast = useToast();
  const [pending, setPending] = useState(false);
  const [keys] = useState(() => new IntentKeys());

  const everywhere = useCallback(async () => {
    if (pending || !record.repos?.length) return;
    const intent = intentOf("scope", record.ref, record.version, "everywhere");
    setPending(true);
    try {
      const result = await source.memories.scope({
        space,
        ref: record.ref,
        version: record.version,
        repos: [],
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      toast({
        state: "kept",
        text: interpolate(p.everywhereDone, { ref: result.ref }),
      });
      void queryClient.invalidateQueries({
        queryKey: recordKeys.memory(source.kind, space.slug, record.ref),
      });
    } catch (err) {
      const failure = toFailure(err);
      if (!isRetryable(failure)) keys.settle(intent);
      if (failure.kind === "clash") {
        // It changed since it was read: read it again.
        void queryClient.invalidateQueries({
          queryKey: recordKeys.memory(source.kind, space.slug, record.ref),
        });
      }
      toast({
        state: "proposed",
        text: failureText(rc, failure, {
          command: "scope",
          ref: record.ref,
          space: space.name,
          locale,
        }),
        ...(isRetryable(failure)
          ? {
              action: {
                label: rc.failure.retry,
                onClick: () => void everywhere(),
              },
            }
          : {}),
      });
    } finally {
      setPending(false);
    }
  }, [
    keys,
    locale,
    p,
    pending,
    queryClient,
    rc,
    record.ref,
    record.repos,
    record.version,
    source,
    space,
    toast,
  ]);

  return { pending, everywhere };
}
