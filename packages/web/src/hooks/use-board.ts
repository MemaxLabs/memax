"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  Board,
  BoardFeedbackVerdict,
  BoardSlot,
  BoardSlotAction,
  BoardStatus,
  BoardWithSlots,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";

export function boardQueryKey(hubId: string) {
  return ["board", hubId] as const;
}

/** All boards on the hub — system board first, then custom ones. */
export function boardsQueryKey(hubId: string) {
  return ["boards", hubId] as const;
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
 * Every board on the hub (plan 25 P4). The system board is always
 * present; custom boards are user-authored standing instructions that
 * start in the `cooking` state until a dream run puts a card on them.
 *
 * NOTE: only the system board has a slots endpoint today
 * (`boards.getForHub`). Custom boards therefore render their 酝酿中
 * state rather than cards — see BoardView's custom-board branch.
 */
/**
 * One board's slots. Custom boards get their cards through this
 * endpoint; the system board keeps using useHubBoard so the memories
 * section's query cache stays untouched.
 */
export function useBoardSlots(
  hubId: string | undefined,
  boardId: string | undefined,
) {
  return useQuery<BoardWithSlots>({
    queryKey: [...boardsQueryKey(hubId ?? ""), boardId ?? ""],
    queryFn: () => getMemaxClient().boards.getBoard(hubId!, boardId!),
    enabled: Boolean(hubId && boardId),
    staleTime: 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

export function useHubBoards(hubId: string | undefined) {
  return useQuery<{ boards: Board[] }>({
    queryKey: boardsQueryKey(hubId ?? ""),
    queryFn: () => getMemaxClient().boards.listForHub(hubId!),
    enabled: Boolean(hubId),
    staleTime: 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

export function useCreateBoard(hubId: string | undefined) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    { board: Board },
    Error,
    { title: string; instruction: string }
  >({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.createBoard,
    },
    mutationFn: (input) => getMemaxClient().boards.createBoard(hubId!, input),
    onSuccess: () => {
      if (hubId) {
        void qc.invalidateQueries({ queryKey: boardsQueryKey(hubId) });
      }
    },
  });
}

export function useUpdateBoard(hubId: string | undefined) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    { board: Board },
    Error,
    {
      boardId: string;
      title?: string;
      instruction?: string;
      status?: BoardStatus;
    }
  >({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.updateBoard,
    },
    mutationFn: ({ boardId, ...input }) =>
      getMemaxClient().boards.updateBoard(hubId!, boardId, input),
    onSuccess: () => {
      if (hubId) {
        void qc.invalidateQueries({ queryKey: boardsQueryKey(hubId) });
        // Editing the system board's title changes the header the
        // slots view renders under.
        void qc.invalidateQueries({ queryKey: boardQueryKey(hubId) });
      }
    },
  });
}

export function useDeleteBoard(hubId: string | undefined) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<{ deleted: boolean }, Error, string>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.deleteBoard,
    },
    mutationFn: (boardId) =>
      getMemaxClient().boards.deleteBoard(hubId!, boardId),
    onSuccess: () => {
      if (hubId) {
        void qc.invalidateQueries({ queryKey: boardsQueryKey(hubId) });
      }
    },
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
      choice,
    }: {
      slotKey: string;
      action: BoardSlotAction;
      verdict?: BoardFeedbackVerdict;
      /** For action "choose" (decision gates): the chosen option id. */
      choice?: string;
    }) =>
      getMemaxClient().boards.resolveSlot(
        hubId!,
        slotKey,
        action,
        verdict,
        choice,
      ),
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
                  // ack / feedback / choose all land the slot in
                  // "resolved"; only dismiss greys it out.
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
