"use client";

import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import type { BoardSlot } from "memax-sdk";
import { useCreateChatSession } from "@/hooks/use-chat";
import { useLocale } from "@/i18n";
import { trackEvent } from "@/lib/posthog";
import { buildBoardCardContext } from "@/components/features/board/board-card-context";

/**
 * 续接 ×2 (plan 25 P3) — the two ways a board card leaves the board.
 *
 * The card is where memax noticed something; these are how the user
 * acts on it without retyping the context:
 *   - continue: opens a memax chat pre-seeded with the card's cited
 *     memories, so the agent starts already knowing what you're
 *     looking at (server renders them into the system prompt).
 *   - copy: puts the same substance on the clipboard as a
 *     <memax-context> block for any external agent.
 */
export function useBoardCardActions(hubId: string) {
  const router = useRouter();
  const { t } = useLocale();
  const createSession = useCreateChatSession();
  const [copiedSlotKey, setCopiedSlotKey] = useState<string | null>(null);

  const continueInMemax = useCallback(
    async (slot: BoardSlot) => {
      trackEvent("board_card_continue", {
        hub_id: hubId,
        kind: slot.kind,
        slot_key: slot.slot_key,
        cite_count: slot.cite_memory_ids?.length ?? 0,
      });
      try {
        const session = await createSession.mutateAsync({
          scopeType: "single_hub",
          scopeHubIds: [hubId],
          writeHubId: hubId,
          title: slot.title,
          pinnedMemoryIds: slot.cite_memory_ids ?? [],
        });
        router.push(`/brain/sessions/${session.id}`);
      } catch {
        // useCreateChatSession's mutation meta already surfaces the
        // error toast; swallow so a failed handoff doesn't bubble.
      }
    },
    [createSession, hubId, router],
  );

  const copyForAgent = useCallback(
    async (slot: BoardSlot) => {
      const block = buildBoardCardContext(slot);
      if (!block) return;
      trackEvent("board_card_copy", {
        hub_id: hubId,
        kind: slot.kind,
        slot_key: slot.slot_key,
      });
      try {
        await navigator.clipboard.writeText(block);
        setCopiedSlotKey(slot.slot_key);
        toast.success(t.board.copied);
        setTimeout(() => setCopiedSlotKey(null), 2000);
      } catch {
        toast.error(t.board.copyFailed);
      }
    },
    [hubId, t.board.copied, t.board.copyFailed],
  );

  return {
    continueInMemax,
    copyForAgent,
    copiedSlotKey,
    isContinuing: createSession.isPending,
  };
}
