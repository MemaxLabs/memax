"use client";

import { useCallback } from "react";
import { useMutation } from "@tanstack/react-query";
import type { BatchAttributeResult } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale, useInterpolate } from "@/i18n";
import { useBarToast } from "@/hooks/use-bar-toast";
import { invalidateMemoryWriteCaches } from "@/hooks/memory-cache";

/**
 * useMemoryAttribute — re-credit memories you own to one of your
 * connected agents (the repair path for rows written before a key had
 * an identity). Not optimistic: the server rewrites provenance and the
 * lists refetch, so the row's chip flips once the truth is stored.
 */
export function useMemoryAttribute() {
  const toast = useBarToast();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const mutation = useMutation<
    BatchAttributeResult,
    Error,
    { ids: string[]; agentName: string }
  >({
    mutationFn: ({ ids, agentName }) =>
      getMemaxClient().memories.batchAttribute(ids, agentName),
    onSettled: (_data, _error, variables) => {
      invalidateMemoryWriteCaches(variables.ids);
    },
  });

  const { mutateAsync, isPending } = mutation;

  const attributeWithToast = useCallback(
    async (
      ids: string[],
      agent: { slug: string; displayName: string },
    ): Promise<boolean> => {
      if (isPending || ids.length === 0) return false;
      try {
        const result = await mutateAsync({ ids, agentName: agent.slug });
        if (result.attributed === 0) {
          toast.error(t.batch.attributeFailed);
          return false;
        }
        const base =
          result.attributed === 1
            ? interpolate(t.batch.attributedOne, { agent: agent.displayName })
            : interpolate(t.batch.attributed, {
                n: result.attributed,
                agent: agent.displayName,
              });
        toast.success(
          result.skipped.length > 0
            ? interpolate(t.batch.partialAttribute, {
                success: base,
                skipped: result.skipped.length,
              })
            : base,
        );
        return true;
      } catch {
        toast.error(t.batch.attributeFailed);
        return false;
      }
    },
    [isPending, mutateAsync, toast, t, interpolate],
  );

  return { isPending, attributeWithToast };
}
