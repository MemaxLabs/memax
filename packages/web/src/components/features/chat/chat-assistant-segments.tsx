"use client";

/**
 * ChatAssistantSegments — renders the assistant body as an
 * interleaved sequence of text + tool steps in chronological
 * order. Industry pattern (Claude.ai / Cursor / v0): when the
 * model emits a tool call mid-response, the step row appears at
 * THAT position in the answer, not stacked above the whole body.
 *
 * **Inline auto-fold.** Contiguous runs of tool segments of
 * length ≥ AUTO_FOLD_THRESHOLD (and only after the turn is
 * terminal) collapse into a single FoldedToolSummary pill at the
 * run's chronological position. This keeps the receipt-style
 * summary while preserving any earlier/later text segments —
 * unlike a global fold which would lose the inline structure.
 * Short runs (<3) stay as individual rows.
 *
 * Smooth streaming is applied only to the trailing text segment
 * (the one currently growing). Earlier text segments are
 * "frozen" — a tool call has split them off, so smoothing them
 * would re-typewrite already-settled prose.
 */

import { motion } from "framer-motion";
import type {
  ChatStreamSegment,
  ChatToolCallRecord,
} from "@/hooks/use-chat-stream";
import { useSmoothStream } from "@/hooks/use-smooth-stream";
import { Markdown } from "@/components/features/markdown";
import { ChatStepRow } from "./chat-step-row";
import { AUTO_FOLD_THRESHOLD, FoldedToolSummary } from "./chat-thinking-pill";

export interface ChatAssistantSegmentsProps {
  segments: ChatStreamSegment[];
  toolCalls: ChatToolCallRecord[];
  /**
   * True once the assistant turn is finalized. Drives whether
   * the trailing text segment still smooth-paces (false → keep
   * pacing) or snaps to its full text (true → snap), AND whether
   * contiguous tool runs auto-fold (true → collapse runs of ≥3
   * tools into a summary pill).
   */
  terminal: boolean;
}

// Internal grouping: consecutive tool segments coalesce into one
// "tools" group; text segments stand alone. Lets the renderer
// decide per-group whether to fold or render row-by-row.
type RenderGroup =
  | { kind: "text"; segIdx: number; text: string }
  | { kind: "tools"; segIdxStart: number; toolCalls: ChatToolCallRecord[] };

export function ChatAssistantSegments({
  segments,
  toolCalls,
  terminal,
}: ChatAssistantSegmentsProps) {
  // Identify the trailing text segment so we can pace it.
  let lastTextIndex = -1;
  for (let i = segments.length - 1; i >= 0; i--) {
    if (segments[i].kind === "text") {
      lastTextIndex = i;
      break;
    }
  }

  const groups = groupSegments(segments, toolCalls);

  return (
    <div className="flex w-full flex-col gap-2">
      {groups.map((group) => {
        if (group.kind === "text") {
          const isTrailing = group.segIdx === lastTextIndex;
          return (
            <motion.div
              key={`text-${group.segIdx}`}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.18 }}
            >
              <TextSegment
                text={group.text}
                terminal={terminal || !isTrailing}
              />
            </motion.div>
          );
        }
        // Tool group folds whenever its size hits the threshold,
        // mid-stream OR after terminal — the user asked for the
        // collapse to apply as soon as it's "applicable", not
        // wait for the chain to finish. The FoldedToolSummary
        // shows a spinner + "Using N tools…" while running, so
        // live state stays visible inside the fold.
        const shouldFold = group.toolCalls.length >= AUTO_FOLD_THRESHOLD;
        if (shouldFold) {
          return (
            <motion.div
              key={`tools-fold-${group.segIdxStart}`}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.18 }}
            >
              <FoldedToolSummary
                toolCalls={group.toolCalls}
                terminal={terminal}
              />
            </motion.div>
          );
        }
        return (
          <div
            key={`tools-${group.segIdxStart}`}
            className="flex w-full flex-col gap-1"
          >
            {group.toolCalls.map((tc, j) => (
              <motion.div
                key={tc.id ?? `${tc.name}-${group.segIdxStart}-${j}`}
                // Opacity-only fade-in, staggered so a batched
                // replay reads as a sequence instead of a flash.
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.18, delay: j * 0.04 }}
              >
                <ChatStepRow tc={tc} terminal={terminal} />
              </motion.div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

function TextSegment({ text, terminal }: { text: string; terminal: boolean }) {
  const display = useSmoothStream(text, { terminal });
  if (!display) return null;
  return (
    <Markdown
      content={display}
      className="chat-markdown text-[15px] leading-[24px] text-fg-1"
    />
  );
}

function groupSegments(
  segments: ChatStreamSegment[],
  toolCalls: ChatToolCallRecord[],
): RenderGroup[] {
  const out: RenderGroup[] = [];
  let toolBuf: ChatToolCallRecord[] = [];
  let toolStartIdx = -1;
  segments.forEach((seg, i) => {
    if (seg.kind === "tool") {
      if (toolBuf.length === 0) toolStartIdx = i;
      const tc = toolCalls[seg.toolIdx];
      if (tc) toolBuf.push(tc);
      return;
    }
    if (toolBuf.length > 0) {
      out.push({
        kind: "tools",
        segIdxStart: toolStartIdx,
        toolCalls: toolBuf,
      });
      toolBuf = [];
      toolStartIdx = -1;
    }
    out.push({ kind: "text", segIdx: i, text: seg.text });
  });
  if (toolBuf.length > 0) {
    out.push({
      kind: "tools",
      segIdxStart: toolStartIdx,
      toolCalls: toolBuf,
    });
  }
  return out;
}
