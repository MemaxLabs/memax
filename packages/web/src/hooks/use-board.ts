"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  BoardFeedbackVerdict,
  BoardSlot,
  BoardSlotAction,
  BoardWithSlots,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";

export function boardQueryKey(hubId: string) {
  return ["board", hubId] as const;
}

/** The hub's pulse board + occupied slots. Empty slots = dreams haven't produced cards yet. */
export function useHubBoard(hubId: string | undefined) {
  return useQuery<BoardWithSlots>({
    queryKey: boardQueryKey(hubId ?? ""),
    queryFn: () => getMemaxClient().boards.getForHub(hubId!),
    enabled: Boolean(hubId),
    staleTime: 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

/**
 * Resolve a card (ack / dismiss / feedback). Optimistically stamps the
 * slot terminal so the receipt swap is instant, then reconciles with
 * the server row.
 */
export function useResolveBoardSlot(hubId: string | undefined) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.resolveBoardCard,
    },
    mutationFn: ({
      slotKey,
      action,
      verdict,
    }: {
      slotKey: string;
      action: BoardSlotAction;
      verdict?: BoardFeedbackVerdict;
    }) => getMemaxClient().boards.resolveSlot(hubId!, slotKey, action, verdict),
    onMutate: async ({ slotKey, action, verdict }) => {
      if (!hubId) return;
      const key = boardQueryKey(hubId);
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<BoardWithSlots>(key);
      qc.setQueryData<BoardWithSlots>(key, (data) =>
        data
          ? {
              ...data,
              slots: data.slots.map((slot): BoardSlot => {
                if (slot.slot_key !== slotKey) return slot;
                return {
                  ...slot,
                  state: action === "dismiss" ? "dismissed" : "resolved",
                  resolution: {
                    action,
                    verdict,
                    resolved_by: "",
                    resolved_at: new Date().toISOString(),
                  },
                };
              }),
            }
          : data,
      );
      return { previous };
    },
    onError: (_err, _vars, context) => {
      if (hubId && context?.previous) {
        qc.setQueryData(boardQueryKey(hubId), context.previous);
      }
    },
    onSettled: () => {
      if (hubId) {
        void qc.invalidateQueries({ queryKey: boardQueryKey(hubId) });
      }
    },
  });
}
