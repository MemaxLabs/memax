"use client";

import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type {
  BoardSlotVersion,
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

/**
 * Query options for the hub's system board + slots. Exported so the
 * hub-switch warm path (`lib/hub-route-readiness.ts`) prefetches into
 * the same cache entry the pulse surface reads.
 */
export function getHubBoardQueryOptions(hubId: string) {
  return {
    queryKey: boardQueryKey(hubId),
    queryFn: () => getMemaxClient().boards.getForHub(hubId),
    staleTime: 60 * 1000,
    gcTime: 30 * 60 * 1000,
  } as const;
}

/** The hub's pulse board + occupied slots. Empty slots = dreams haven't produced cards yet. */
export function useHubBoard(hubId: string | undefined) {
  return useQuery<BoardWithSlots>({
    ...getHubBoardQueryOptions(hubId ?? ""),
    enabled: Boolean(hubId),
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

export interface CustomBoardWithSlots {
  board: Board;
  slots: BoardSlot[];
}

/**
 * Slots for every CUSTOM board on the hub, aggregated client-side
 * (2026-08 unified stream — custom-board cards merge into the main
 * flow, no tabs). Hubs cap out at a handful of boards, so N parallel
 * getBoard calls are fine; each query shares its cache entry with
 * useBoardSlots (same key), so nothing double-fetches.
 */
export function useCustomBoardsWithSlots(
  hubId: string | undefined,
  boards: readonly Board[],
): CustomBoardWithSlots[] {
  const customBoards = boards.filter((b) => b.kind !== "system");
  return useQueries({
    queries: customBoards.map((board) => ({
      queryKey: [...boardsQueryKey(hubId ?? ""), board.id] as const,
      queryFn: () => getMemaxClient().boards.getBoard(hubId!, board.id),
      enabled: Boolean(hubId),
      staleTime: 60 * 1000,
      gcTime: 30 * 60 * 1000,
    })),
    combine: (results) =>
      customBoards.map((board, index) => ({
        board,
        slots: results[index]?.data?.slots ?? [],
      })),
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
      /**
       * The board owning this slot. Callers resolving a custom-board
       * card must pass it; omitted means the system board.
       */
      boardId?: string;
    }) =>
      getMemaxClient().boards.resolveSlot(
        hubId!,
        slotKey,
        action,
        verdict,
        choice,
      ),
    onMutate: async ({ slotKey, action, verdict, boardId }) => {
      if (!hubId) return;
      // Slot keys are only unique WITHIN a board: every board has its
      // own "1-wow". Matching on the key alone stamped every custom
      // board's card terminal whenever a system-board card resolved,
      // so the whole shelf blinked out and back on each dismiss.
      const stampTerminal = (data: BoardWithSlots): BoardWithSlots => ({
        ...data,
        slots: data.slots.map((slot): BoardSlot => {
          if (slot.slot_key !== slotKey) return slot;
          if (targetBoardId && slot.board_id !== targetBoardId) return slot;
          // Reopen is the inverse stamp: back to the live board with
          // the receipt cleared (the server lands it in "seen").
          if (action === "reopen") {
            return { ...slot, state: "seen", resolution: undefined };
          }
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
      });
      const key = boardQueryKey(hubId);
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<BoardWithSlots>(key);
      // Which board does this slot belong to? The caller knows when it
      // resolves a custom-board card; otherwise it's the system board
      // in the primary cache.
      const targetBoardId =
        boardId ??
        previous?.slots.find((slot) => slot.slot_key === slotKey)?.board_id;
      qc.setQueryData<BoardWithSlots>(key, (data) =>
        data ? stampTerminal(data) : data,
      );
      // Custom-board caches share the slot lifecycle (the shelf's ×
      // dismisses their slots too) — stamp any per-board cache holding
      // this slot so the tile leaves immediately. The prefix also
      // matches the boards LIST cache ({boards}), hence the shape
      // guard.
      const previousCustom = qc.getQueriesData<BoardWithSlots>({
        queryKey: boardsQueryKey(hubId),
      });
      for (const [queryKey, data] of previousCustom) {
        if (data && Array.isArray(data.slots)) {
          qc.setQueryData(queryKey, stampTerminal(data));
        }
      }
      return { previous, previousCustom };
    },
    onError: (_err, _vars, context) => {
      if (!hubId || !context) return;
      if (context.previous) {
        qc.setQueryData(boardQueryKey(hubId), context.previous);
      }
      for (const [queryKey, data] of context.previousCustom ?? []) {
        if (data && Array.isArray(data.slots)) {
          qc.setQueryData(queryKey, data);
        }
      }
    },
    onSettled: () => {
      if (hubId) {
        void qc.invalidateQueries({ queryKey: boardQueryKey(hubId) });
        void qc.invalidateQueries({ queryKey: boardsQueryKey(hubId) });
      }
    },
  });
}

/**
 * A slot's archived content versions (工单 8 — stateful cards keep a
 * readable timeline). Fetched lazily: `enabled` only when the 历史
 * disclosure is open, so the board GET stays one request.
 */
export function useSlotHistory(
  hubId: string | undefined,
  slotKey: string,
  enabled: boolean,
) {
  return useQuery<{ versions: BoardSlotVersion[] }>({
    queryKey: ["board-slot-history", hubId, slotKey],
    queryFn: () => getMemaxClient().boards.slotHistory(hubId!, slotKey),
    enabled: enabled && !!hubId,
    staleTime: 5 * 60 * 1000,
  });
}
