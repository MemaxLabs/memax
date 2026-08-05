"use client";

/**
 * ChatThread — message list for one session. Owns:
 *   - Listing the persisted messages via `useChatMessages`.
 *   - Identifying the in-flight assistant (status in
 *     queued/running/canceling) and wiring its row to the live
 *     `useChatStream` aggregator.
 *   - Auto-scroll to the bottom on new content (with a "user
 *     scrolled up" gate so we don't yank the viewport while the
 *     user is reading prior turns).
 *   - Citations + approval-decision dispatching.
 *
 * Stays narrow on purpose — the route page composes thread +
 * sidebar + composer + status. Per memory `no monolith components`,
 * each of those lives in its own file.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { motion } from "framer-motion";
import { MemaxLogo } from "@memaxlabs/ui";
import type { ChatMessage } from "memax-sdk";
import { cn } from "@memaxlabs/ui";
import { useChatMessages, useDecideChatApproval } from "@/hooks/use-chat";
import { queryClient } from "@/lib/query-client";
import {
  useChatStream,
  type ChatApprovalRecord,
} from "@/hooks/use-chat-stream";
import { useLocale } from "@/i18n";
import { ChatMessageRow } from "./chat-message-row";

const inFlightStatuses: ReadonlySet<ChatMessage["status"]> = new Set([
  "queued",
  "running",
  "canceling",
]);

export interface ChatThreadProps {
  sessionId: string;
  /**
   * Lift the in-flight assistant id so the route page can render
   * the cancel-aware composer without duplicating the lookup.
   */
  onInflightAssistantChange?: (assistantId: string | null) => void;
}

export function ChatThread({
  sessionId,
  onInflightAssistantChange,
}: ChatThreadProps) {
  const { t } = useLocale();
  const { data, isLoading, error } = useChatMessages(sessionId);
  const decideApproval = useDecideChatApproval();
  const [locallyDecidedApprovalIds, setLocallyDecidedApprovalIds] = useState(
    () => new Set<string>(),
  );

  const messages = useMemo<ChatMessage[]>(
    () => data?.messages ?? [],
    [data?.messages],
  );

  const inflightAssistant = useMemo(
    () =>
      messages.find(
        (m) => m.role === "assistant" && inFlightStatuses.has(m.status),
      ) ?? null,
    [messages],
  );

  useEffect(() => {
    onInflightAssistantChange?.(inflightAssistant?.id ?? null);
  }, [inflightAssistant?.id, onInflightAssistantChange]);

  const stream = useChatStream({
    sessionId,
    messageId: inflightAssistant?.id,
    paused: !inflightAssistant,
  });

  useEffect(() => {
    setLocallyDecidedApprovalIds(new Set());
  }, [sessionId, inflightAssistant?.id]);

  const visibleStream = useMemo(() => {
    if (!inflightAssistant || locallyDecidedApprovalIds.size === 0) {
      return stream;
    }
    return {
      ...stream,
      approvals: stream.approvals.filter(
        (approval) => !locallyDecidedApprovalIds.has(approval.approval_id),
      ),
    };
  }, [inflightAssistant, locallyDecidedApprovalIds, stream]);

  // When the live stream lands a terminal frame, the cached message
  // list still has the row's prior status (queued / running) — the
  // SSE event doesn't go through React Query. Invalidate once so the
  // persisted status, finalized content, and tool_calls catch up.
  // Keyed on the message id so we only fire once per turn even
  // through multiple `terminal` re-renders.
  const lastTerminalForMessageId = useRef<string | null>(null);
  useEffect(() => {
    if (!stream.terminal) return;
    if (!inflightAssistant) return;
    if (lastTerminalForMessageId.current === inflightAssistant.id) return;
    lastTerminalForMessageId.current = inflightAssistant.id;
    void queryClient.invalidateQueries({
      queryKey: ["chat", "messages", "list", sessionId],
    });
    void queryClient.invalidateQueries({
      queryKey: ["chat", "sessions", "list"],
    });
  }, [inflightAssistant, sessionId, stream.terminal]);

  // Auto-scroll on new content unless the user has scrolled up.
  // Pinned to bottom is the default state — we only release the
  // pin when the user actively scrolls more than a small threshold
  // away. Once they scroll back near the bottom, the pin re-engages.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [pinnedToBottom, setPinnedToBottom] = useState(true);

  // Auto-scroll-to-bottom fires when ANY part of the in-flight
  // surface grows: text deltas, tool calls, approval cards, and
  // the chronological segment list (text/tool interleave during
  // streaming). Without `approvals.length` in the deps, an
  // approval card landing mid-turn pushes the column down but
  // the scroll stays put — the user sees the card clipped at
  // the bottom of the viewport. `segments.length` covers the
  // inline-mode layout where row heights don't track text or
  // toolCalls counts directly. Two-phase rAF (then settle)
  // ensures the scroll reflects the post-paint scrollHeight,
  // not the pre-layout one — important when motion entries add
  // height after the initial commit.
  useEffect(() => {
    if (!pinnedToBottom) return;
    const el = scrollRef.current;
    if (!el) return;
    const settle = () => {
      el.scrollTop = el.scrollHeight;
    };
    settle();
    const handle = requestAnimationFrame(settle);
    return () => cancelAnimationFrame(handle);
  }, [
    messages.length,
    visibleStream.text,
    visibleStream.toolCalls.length,
    visibleStream.approvals.length,
    visibleStream.segments.length,
    pinnedToBottom,
  ]);

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    setPinnedToBottom(distanceFromBottom < 80);
  };

  const onDecideApproval = (
    approval: ChatApprovalRecord,
    decision: "approve" | "deny",
  ) => {
    setLocallyDecidedApprovalIds((current) => {
      const next = new Set(current);
      next.add(approval.approval_id);
      return next;
    });
    decideApproval.mutate(
      {
        sessionId,
        approvalId: approval.approval_id,
        decision,
      },
      {
        onError: () => {
          setLocallyDecidedApprovalIds((current) => {
            const next = new Set(current);
            next.delete(approval.approval_id);
            return next;
          });
        },
      },
    );
  };

  if (isLoading) {
    return (
      <div
        className="flex min-h-0 flex-1 items-center justify-center text-fg-3"
        aria-hidden
      >
        <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center px-6 text-center text-fg-3">
        {error.message || t.chat.errors.sendMessage}
      </div>
    );
  }

  // "Pending thinking" — last message is user + no inflight assistant
  // yet. This covers the gap between the optimistic user message
  // landing in the cache (seeded by ChatBrainView's ensureSessionAndSend
  // before navigation) and the real assistant row arriving via
  // invalidateQueries. Industry pattern: user message renders
  // instantly, thinking dots appear underneath until the model starts
  // streaming. Without this, a fresh /brain send showed the user's
  // text immediately but then a blank gap until the assistant row
  // landed.
  const lastMessage = messages[messages.length - 1];
  const showPendingThinking =
    !inflightAssistant && lastMessage?.role === "user";

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className={cn(
        // overflow-x-hidden is the hard stop for the "page pans
        // sideways on mobile" class of bugs: wide content (tables,
        // code, tool JSON) must scroll inside its own container,
        // never widen the thread. Pairs with min-w-0 on the message
        // row's content column.
        "min-h-0 flex-1 overflow-y-auto overflow-x-hidden",
        "[scrollbar-gutter:stable]",
      )}
    >
      <div className="mx-auto flex max-w-3xl flex-col gap-1 px-3 py-8 sm:px-6">
        {messages.map((m) => {
          const isInflight = inflightAssistant?.id === m.id;
          return (
            <ChatMessageRow
              key={m.id}
              message={m}
              streaming={isInflight ? visibleStream : undefined}
              onDecideApproval={isInflight ? onDecideApproval : undefined}
            />
          );
        })}
        {showPendingThinking && <PendingThinkingRow />}
      </div>
    </div>
  );
}

/**
 * Assistant-shaped row with breathing dots — surfaces while the user's
 * message is in-flight but the real assistant row hasn't materialized
 * yet. Mirrors the avatar + alignment grammar of `<ChatMessageRow>` so
 * the optimistic state flows visually into the real one without a
 * layout jump.
 */
// Exported so the chat-brain-view's pending-send bridge can render
// the IDENTICAL thinking indicator while createSession resolves —
// keeps the bridge → thread transition visually seamless instead of
// a different-looking placeholder swapping to a different-looking
// real row.
export function PendingThinkingRow() {
  const { t } = useLocale();
  return (
    <div
      className="flex w-full justify-start gap-3 py-3"
      role="status"
      aria-live="polite"
      aria-label={t.chat.status.thinking}
    >
      <div className="mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-surface-2">
        <MemaxLogo size={12} />
      </div>
      <div className="flex min-h-6 items-center gap-1 pt-1">
        {[0, 1, 2].map((i) => (
          <motion.span
            key={i}
            className="h-1.5 w-1.5 rounded-full bg-fg-3"
            animate={{ opacity: [0.3, 1, 0.3] }}
            transition={{
              duration: 1.0,
              repeat: Infinity,
              delay: i * 0.15,
              ease: [0.16, 1, 0.3, 1],
            }}
          />
        ))}
      </div>
    </div>
  );
}
