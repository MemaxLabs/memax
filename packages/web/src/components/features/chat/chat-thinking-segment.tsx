"use client";

/**
 * ChatThinkingSegment — one readable reasoning block, rendered at the
 * chronological position the model thought it (Claude.ai pattern).
 *
 * Two presentations:
 *   - LIVE (this is the last segment and the turn is in flight):
 *     breathing ✦ + "Thinking…" label + a two-line preview of the
 *     reasoning, so the wait between tool steps reads as visible work.
 *   - SETTLED (activity moved past it, or the turn is terminal):
 *     a quiet one-line "Thought" row, chevron-expandable to the full
 *     reasoning in muted type. Collapsed by default — reasoning is
 *     context, not the answer.
 */

import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { Markdown } from "@/components/features/markdown";

export function ChatThinkingSegment({
  text,
  live,
  durationMs,
}: {
  text: string;
  /** True while this block is streaming as the newest in-flight activity. */
  live: boolean;
  /** Set once the block completes; drives the "Thought for Xs" label. */
  durationMs?: number;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const [expanded, setExpanded] = useState(false);

  if (live) {
    return (
      <div className="flex items-start gap-1.5 text-[13px] text-fg-3">
        <span
          aria-hidden
          className="state-slow-breathe mt-px shrink-0 leading-none"
          style={{ color: "var(--signature)" }}
        >
          {"✦"}
        </span>
        <div className="min-w-0">
          <span className="font-medium">{t.chat.thinking.reasoningLive}</span>
          <p className="mt-0.5 line-clamp-2 text-[12px] leading-relaxed text-fg-4">
            {text}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex w-full flex-col">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex items-center gap-1.5 self-start rounded-lg px-1 py-0.5 text-[12px] text-fg-4 transition-colors hover:text-fg-2 cursor-pointer"
      >
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 transition-transform",
            expanded && "rotate-90",
          )}
        />
        {durationMs !== undefined
          ? interpolate(t.chat.thinking.reasoningFor, {
              s: Math.max(1, Math.round(durationMs / 1000)),
            })
          : t.chat.thinking.reasoningLabel}
      </button>
      {expanded && (
        <div className="ml-4 mt-1 border-l border-border/30 pl-3 animate-content-ready">
          <Markdown
            content={text}
            className="chat-markdown text-[13px] leading-[20px] text-fg-3"
          />
        </div>
      )}
    </div>
  );
}
