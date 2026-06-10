"use client";

/**
 * ChatMessageRow — one turn rendered in the thread.
 *
 * User messages right-aligned, assistant messages left-aligned —
 * the convention plan 24 §"UI surface" pins. Status badges (queued,
 * running, canceling, terminal flavors) render below assistant
 * content so the user can see when memax is still working without
 * polling the row.
 *
 * For an actively-streaming assistant message the caller passes
 * `streaming` (the live aggregator from useChatStream) so the row
 * shows the in-flight content instead of the persisted (still
 * empty) row content. Once the stream lands a terminal frame the
 * persisted row catches up via React Query invalidation and the
 * `streaming` prop is no longer passed for that row.
 */

import { MemaxLogo } from "@memaxlabs/ui";
import type {
  ChatApprovalRecord,
  ChatStreamState,
  ChatToolCallRecord,
} from "@/hooks/use-chat-stream";
import type { ChatMessage } from "memax-sdk";
import { cn } from "@memaxlabs/ui";
import { useSmoothStream } from "@/hooks/use-smooth-stream";
import { useLocale } from "@/i18n";
import { Markdown } from "@/components/features/markdown";
import { ChatThinkingPill } from "./chat-thinking-pill";
import { ChatAssistantSegments } from "./chat-assistant-segments";
import { ChatSourcesRow } from "./chat-sources-row";
import { ChatApprovalCardList } from "./chat-approval-card-list";

export interface ChatMessageRowProps {
  message: ChatMessage;
  /**
   * Live stream aggregator for this message. Only passed on the
   * assistant message that's currently in-flight. When provided,
   * `streaming.text` overrides `message.content`,
   * `streaming.toolCalls` overrides the persisted tool_calls, and
   * `streaming.approvals` is rendered as inline approval cards.
   */
  streaming?: ChatStreamState;
  /**
   * Approval decision callback. Wired by the parent so the row
   * doesn't need to know about the session id.
   */
  onDecideApproval?: (
    approval: ChatApprovalRecord,
    decision: "approve" | "deny",
  ) => void;
}

export function ChatMessageRow({
  message,
  streaming,
  onDecideApproval,
}: ChatMessageRowProps) {
  const { t } = useLocale();
  const isUser = message.role === "user";
  const isAssistant = message.role === "assistant";

  // Smooth pacing for the persisted/no-segments path. Streaming
  // with segments uses per-segment smoothing inside
  // ChatAssistantSegments so only the trailing text segment paces
  // (earlier text has already been split off by a tool call and
  // is "settled" from the user's reading perspective).
  const liveText = useSmoothStream(streaming?.text ?? "", {
    terminal: Boolean(streaming?.terminal),
  });
  const segments = streaming?.segments ?? [];
  const content = streaming ? liveText : message.content;
  // SDK-internal tools (e.g. `request_approval`) are runtime
  // plumbing — they exist so the agent loop can pause for human
  // consent, but the user never directly invokes them and the
  // approval card itself is the visible UI. Filter them out so
  // they don't pollute the timeline or inflate the auto-fold
  // count with a "Done" row that means nothing to the user.
  const rawToolCalls: ChatToolCallRecord[] = streaming?.toolCalls.length
    ? streaming.toolCalls
    : extractPersistedToolCalls(message.tool_calls);
  const toolCalls = rawToolCalls.filter(
    (tc) => !INTERNAL_TOOL_NAMES.has(tc.name),
  );
  const approvals = streaming?.approvals ?? [];
  const isTerminal = Boolean(streaming?.terminal) || !streaming;
  const hasAnyToolCalls = isAssistant && toolCalls.length > 0;
  // Inline segments win whenever the stream produced them.
  // Auto-fold logic for long chains is scoped INSIDE the
  // segment renderer (ChatAssistantSegments) so contiguous tool
  // runs collapse in place, preserving the chronology of any
  // earlier/later text segments. The stacked-pill path
  // (ChatThinkingPill) still does its own whole-chain fold for
  // persisted/reload rows that lost the segment ordering.
  const useInlineSegments = isAssistant && segments.length > 0;
  const showPill = isAssistant && !approvals.length && !useInlineSegments;
  const showIdleDots =
    isAssistant && !isTerminal && !content && !hasAnyToolCalls;

  // Status caption is intentionally narrow now: only surface
  // canceled / failed / partial flavors. "Replying…" duplicated
  // the pill (the spinner steps already say what's happening) and
  // the live "thinking…" italic was replaced by the dot animation
  // above. For a clean completed turn the row stays silent.
  const status = streaming?.terminal ?? message.status;
  const statusLabel = labelForTerminalCaption(status, t.chat.status);

  return (
    <div
      className={cn(
        "flex w-full gap-3 py-3",
        isUser ? "justify-end" : "justify-start",
      )}
      data-message-id={message.id}
      data-message-role={message.role}
    >
      {!isUser && (
        // Avatar is a 24px box; the icon centers inside it via
        // items-center+justify-center. Every "first content row"
        // below (thinking dots, step rows, assistant body line)
        // is also constrained to a 24px row with items-center,
        // so all four centerlines share the same y-axis. Without
        // this, the first content line drifts ~4px below the
        // avatar's icon and the row reads as misaligned.
        <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-background shadow-[0_8px_24px_oklch(from_var(--foreground)_l_c_h_/_0.06)]">
          <MemaxLogo size={14} aria-hidden />
        </div>
      )}
      <div
        className={cn(
          "flex max-w-[min(680px,100%)] flex-col gap-1.5",
          isUser ? "items-end" : "items-start",
        )}
      >
        {showPill && (
          <ChatThinkingPill
            toolCalls={toolCalls}
            terminal={isTerminal}
            showIdle={showIdleDots}
          />
        )}
        {useInlineSegments ? (
          // Streaming with segments: chronological inline render.
          // The thinking pill is suppressed (showPill is false in
          // this branch) so step rows appear at their actual
          // arrival positions instead of being stacked above.
          <div className="w-full px-1">
            <ChatAssistantSegments
              segments={segments}
              toolCalls={toolCalls}
              terminal={isTerminal}
            />
          </div>
        ) : (
          <div
            className={cn(
              "text-[15px] leading-relaxed break-words",
              isUser
                ? "max-w-[min(560px,82vw)] rounded-[22px] bg-surface-2 px-4 py-2.5 text-fg-1"
                : // py-0 + leading-[24px] keeps the first line's
                  // visual center at 12px from the body top, which
                  // matches the avatar icon's center to the pixel.
                  // px-1 stays so the answer doesn't bleed to the
                  // very edge of the message column.
                  "w-full px-1 text-fg-1 leading-[24px]",
            )}
          >
            {content ? (
              isAssistant ? (
                <Markdown
                  content={content}
                  className="chat-markdown text-[15px] leading-relaxed"
                />
              ) : (
                <span className="whitespace-pre-wrap">{content}</span>
              )
            ) : (
              <span aria-hidden />
            )}
          </div>
        )}
        {isAssistant && approvals.length > 0 && onDecideApproval && (
          <ChatApprovalCardList
            approvals={approvals}
            onDecide={onDecideApproval}
          />
        )}
        {isAssistant && isTerminal && hasAnyToolCalls && (
          <ChatSourcesRow toolCalls={toolCalls} />
        )}
        {isAssistant && statusLabel && (
          <span className="text-[11px] text-fg-3" aria-live="polite">
            {statusLabel}
          </span>
        )}
      </div>
    </div>
  );
}

// Tool names the agent runtime emits that are NOT user-facing
// work — the SDK uses them as flow-control plumbing and the
// chat surface has dedicated UI for them (approval cards,
// cancel buttons, etc.). Don't render them as step rows.
const INTERNAL_TOOL_NAMES: ReadonlySet<string> = new Set(["request_approval"]);

function extractPersistedToolCalls(raw: unknown): ChatToolCallRecord[] {
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((entry): ChatToolCallRecord[] => {
    if (!entry || typeof entry !== "object") return [];
    const e = entry as Record<string, unknown>;
    return [
      {
        id: typeof e.id === "string" ? e.id : undefined,
        name: typeof e.name === "string" ? e.name : "(unknown)",
        args: e.args,
        result: typeof e.result === "string" ? e.result : e.result,
        is_error: Boolean(e.is_error),
      },
    ];
  });
}

function labelForTerminalCaption(
  status: string | undefined,
  labels: { [k: string]: string },
): string | undefined {
  // Only surface terminal-failure flavors; the pill conveys
  // queued/running/canceling already, so re-saying "Replying…"
  // beneath an active spinner reads as duplicate noise.
  switch (status) {
    case "canceling":
      return labels.canceling;
    case "canceled":
      return labels.canceled;
    case "failed":
      return labels.failed;
    case "partial_failed":
      return labels.partial;
    default:
      return undefined;
  }
}
