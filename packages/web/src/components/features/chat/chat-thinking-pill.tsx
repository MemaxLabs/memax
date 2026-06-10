"use client";

/**
 * ChatThinkingPill — vertical list of step rows for an assistant
 * turn's tool activity. Used on the persisted/no-segments path
 * where the row stacks all tool steps ABOVE the answer body. The
 * inline-during-streaming path uses ChatAssistantSegments instead,
 * which interleaves text + tool steps in chronological order.
 *
 * **Auto-fold (per group).** The persisted/stacked chain is
 * effectively ONE group (no segment ordering to split it into
 * multiple groups), so when the turn is terminal with ≥
 * AUTO_FOLD_THRESHOLD tools the whole chain folds into a single
 * `FoldedToolSummary` pill — matching the per-group fold rule
 * applied inline by ChatAssistantSegments. Click expands. Short
 * chains (<3) stay open as a vertical list.
 *
 * Idle: when the turn is mid-flight with no tool calls yet, render
 * the 3-dot bouncing animation so the user sees memax is working.
 *
 * Per-row args/result expansion lives on each ChatStepRow — both
 * paths share the same primitive so the chevron + animations stay
 * consistent across stacked and inline views.
 */

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
// AnimatePresence is still used by FoldedToolSummary for the
// expanded inner detail panel. The stacked stepper above no
// longer wraps in AnimatePresence — see commit message for the
// "restraint > choreography" reasoning.
import { ChevronRight, Loader2 } from "lucide-react";
import type { ChatToolCallRecord } from "@/hooks/use-chat-stream";
import { cn } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import { ChatStepRow } from "./chat-step-row";
import { doneLabel, runningLabel } from "./chat-tool-labels";

/** Tool-chain length at which the post-terminal auto-fold kicks in. */
export const AUTO_FOLD_THRESHOLD = 3;

export interface ChatThinkingPillProps {
  toolCalls: ChatToolCallRecord[];
  /** True once the assistant turn is finalized. */
  terminal: boolean;
  /** True while the message is in flight with NO tool calls yet. */
  showIdle: boolean;
}

export function ChatThinkingPill({
  toolCalls,
  terminal,
  showIdle,
}: ChatThinkingPillProps) {
  if (toolCalls.length === 0) {
    return showIdle ? <ThinkingDots /> : null;
  }

  // Auto-fold fires the moment the threshold trips — mid-stream
  // OR after terminal. Waiting for terminal made the user watch
  // 5 rows pile up before they collapsed; folding immediately
  // keeps the column compact while the chain is happening. The
  // FoldedToolSummary itself indicates running vs done state via
  // its label/icon so the user still knows work is in flight.
  const shouldAutoFold = toolCalls.length >= AUTO_FOLD_THRESHOLD;
  if (shouldAutoFold) {
    return <FoldedToolSummary toolCalls={toolCalls} terminal={terminal} />;
  }

  return (
    <div className="flex w-full flex-col gap-1">
      {toolCalls.map((tc, idx) => (
        <motion.div
          key={tc.id ?? `${tc.name}-${idx}`}
          // Opacity-only entry. Earlier iterations had y-translate,
          // height-collapse, AnimatePresence, and `layout` FLIP
          // all firing together — the compounding motion read as
          // buggy distortion. Restraint > choreography (industry
          // chat: ChatGPT, Claude.ai). Stagger turns batch-mounted
          // rows (replay path) into a readable sequence.
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.18, delay: idx * 0.04 }}
        >
          <ChatStepRow tc={tc} terminal={terminal} />
        </motion.div>
      ))}
    </div>
  );
}

// ── Auto-fold summary ──────────────────────────────────────────

/**
 * FoldedToolSummary — single "Used N tools" summary pill that
 * collapses a long tool-step chain. Exported so the inline
 * segments path (ChatAssistantSegments) can use it for in-place
 * runs of ≥ AUTO_FOLD_THRESHOLD tools, AND the stacked pill path
 * uses it for the whole chain. Same primitive, two consumers.
 */
export function FoldedToolSummary({
  toolCalls,
  terminal,
}: {
  toolCalls: ChatToolCallRecord[];
  terminal: boolean;
}) {
  const { t } = useLocale();
  const [expanded, setExpanded] = useState(false);
  // Live status indicator (industry pattern: ChatGPT Reasoning,
  // Cursor): while any tool inside is still in flight, the pill
  // shows the LATEST step's current label (e.g. "Searching
  // memories…" with a spinner) — the user reads the live
  // frontier without expanding. Once every step has settled, the
  // pill switches to a count summary ("Used 4 tools") so the
  // turn reads as a clean receipt.
  const anyRunning = toolCalls.some(
    (tc) => !tc.is_error && tc.result === undefined && !terminal,
  );
  const latest = toolCalls[toolCalls.length - 1];
  const latestRunning =
    !!latest && !latest.is_error && latest.result === undefined && !terminal;
  const summaryLabel = anyRunning
    ? latest && latestRunning
      ? runningLabel(latest.name, t.chat.thinking)
      : latest
        ? doneLabel(latest, t.chat.thinking)
        : interpolate(t.chat.thinking.autoFoldUsing, {
            count: toolCalls.length,
          })
    : interpolate(t.chat.thinking.autoFoldUsed, {
        count: toolCalls.length,
      });

  return (
    <div className="flex w-full flex-col">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className={cn(
          "group inline-flex min-h-6 items-center gap-2 self-start rounded-full px-2.5",
          "border border-border/50 bg-surface-1/70 text-[12px] text-fg-2",
          "cursor-pointer transition-colors hover:bg-surface-2",
        )}
        aria-expanded={expanded}
        aria-label={
          expanded
            ? t.chat.thinking.autoFoldCollapse
            : t.chat.thinking.autoFoldExpand
        }
      >
        {/* Leading spinner crossfades when the chain transitions
            from running to done (any-running flips false). */}
        <AnimatePresence mode="wait" initial={false}>
          {anyRunning ? (
            <motion.span
              key="spinner"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.14 }}
              className="inline-flex"
            >
              <Loader2
                className="h-3 w-3 shrink-0 animate-spin text-fg-3"
                aria-hidden
              />
            </motion.span>
          ) : null}
        </AnimatePresence>
        {/* Live label crossfades when the latest tool changes
            stage (e.g. "Searching memories…" → "Searched 3
            memories" → "Reading memory…"). The key on the label
            string drives AnimatePresence even when only the
            count interpolation changed. */}
        <span className="relative inline-flex min-w-0">
          <AnimatePresence mode="wait" initial={false}>
            <motion.span
              key={summaryLabel}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.14 }}
              className="block truncate"
            >
              {summaryLabel}
            </motion.span>
          </AnimatePresence>
        </span>
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 text-fg-3 transition-transform duration-200",
            expanded ? "rotate-90" : "rotate-0",
          )}
          aria-hidden
        />
      </button>
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
            className="overflow-hidden"
          >
            <div className="mt-1 flex w-full flex-col gap-1">
              {toolCalls.map((tc, idx) => (
                <motion.div
                  key={tc.id ?? `${tc.name}-${idx}`}
                  initial={{ opacity: 0, y: 4 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{
                    duration: 0.18,
                    ease: [0.16, 1, 0.3, 1],
                    delay: 0.03 * idx,
                  }}
                >
                  <ChatStepRow tc={tc} terminal={terminal} />
                </motion.div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ── Idle dots ──────────────────────────────────────────────────

function ThinkingDots() {
  const { t } = useLocale();
  return (
    <div
      // 24px row with items-center so the dots' visual midline
      // sits at the same y as the avatar's icon center.
      className="flex min-h-6 items-center gap-1"
      aria-label={t.chat.status.thinking}
      role="status"
      aria-live="polite"
    >
      {[0, 1, 2].map((i) => (
        <motion.span
          key={i}
          className="h-1.5 w-1.5 rounded-full bg-fg-3"
          animate={{ opacity: [0.3, 1, 0.3] }}
          transition={{
            duration: 1.0,
            repeat: Infinity,
            delay: i * 0.15,
            // Same ease-out cubic the rest of the chat surface
            // uses (step rows, sources row, expand panels). Keeps
            // the dot pulse rhythm in the same motion vocabulary
            // instead of mixing easeInOut with bezier curves.
            ease: [0.16, 1, 0.3, 1],
          }}
        />
      ))}
    </div>
  );
}
