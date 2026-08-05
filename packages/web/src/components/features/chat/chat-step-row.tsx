"use client";

/**
 * ChatStepRow — one tool-call line with status icon + label +
 * chevron-expandable args/result detail. Used in two places:
 *
 *   1. ChatThinkingPill — renders all step rows above the
 *      assistant body (persisted / no-segments path).
 *   2. ChatAssistantSegments — renders one step row inline at the
 *      chronological position the tool call happened (streaming
 *      with segments).
 *
 * Behavior:
 *   - running (`result === undefined && !terminal && !is_error`):
 *     spinner + running label.
 *   - errored: X icon + "{name} failed".
 *   - done (terminal or result landed): check icon + done label
 *     (with recall-count for recall_memories).
 */

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, ChevronRight, Loader2, XCircle } from "lucide-react";
import type { ChatToolCallRecord } from "@/hooks/use-chat-stream";
import { cn } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import { doneLabel, runningLabel } from "./chat-tool-labels";
import { ChatRecallResults, parseRecallHits } from "./chat-recall-results";

export interface ChatStepRowProps {
  tc: ChatToolCallRecord;
  /** True once the assistant turn is finalized. */
  terminal: boolean;
}

// Minimum time the running spinner is shown before flipping to
// the done state, but ONLY when the row mounted in the running
// state (live streaming). Persisted/reload rows that mount
// already-done skip the gate — there's no work to perceive.
// Industry pattern (Linear / Vercel / OpenAI): snappy state
// changes need a visible floor or the user reads "step appeared
// and was done" without ever perceiving the work. 350ms is short
// enough to feel responsive, long enough that the spinner is
// recognizably a spinner.
const MIN_RUNNING_MS = 350;

export function ChatStepRow({ tc, terminal }: ChatStepRowProps) {
  const { t } = useLocale();
  const [expanded, setExpanded] = useState(false);

  const errored = Boolean(tc.is_error);
  const resultLanded = tc.result !== undefined;
  // Snapshot the "did this row arrive in the running phase"
  // question on first mount so we don't gate persisted rows that
  // were never running on this client. Ref keeps the value stable
  // across the lifetime of the component instance.
  const mountedRunningRef = useRef<boolean>(
    !errored && !resultLanded && !terminal,
  );
  const [minVisibleElapsed, setMinVisibleElapsed] = useState(
    !mountedRunningRef.current,
  );
  useEffect(() => {
    if (!mountedRunningRef.current) return;
    const handle = setTimeout(() => setMinVisibleElapsed(true), MIN_RUNNING_MS);
    return () => clearTimeout(handle);
  }, []);

  const running =
    !errored &&
    ((!resultLanded && !terminal) ||
      (mountedRunningRef.current && !minVisibleElapsed));
  const label = errored
    ? interpolate(t.chat.thinking.genericError, { name: tc.name })
    : running
      ? runningLabel(tc.name, t.chat.thinking)
      : doneLabel(tc, t.chat.thinking);

  const argsText = tc.args !== undefined ? compactJSON(tc.args) : undefined;
  // memax-native result rendering: recall hits become tappable memory
  // rows instead of a JSON dump. Anything that doesn't parse as recall
  // hits falls back to the raw JSON path below.
  const recallHits =
    !errored && tc.name === "recall_memories" && tc.result !== undefined
      ? parseRecallHits(tc.result)
      : null;
  const resultText =
    tc.result !== undefined && recallHits === null
      ? formatResult(tc.result)
      : undefined;
  const expandable = Boolean(argsText || resultText || recallHits);

  return (
    <div className="flex w-full flex-col">
      <button
        type="button"
        onClick={() => expandable && setExpanded((v) => !v)}
        className={cn(
          // Errored steps stay on the neutral text scale — the X
          // icon's amber tint is the signal. Destructive red is
          // reserved for *actions* (delete confirmation, etc.); a
          // tool step that failed is an attention moment, not an
          // alarm. Matches Linear / Cursor / Notion grammar.
          "group inline-flex min-h-6 items-center gap-2 self-start rounded-md pr-1 text-left text-[12px] text-fg-2 transition-colors",
          expandable ? "cursor-pointer hover:text-fg-1" : "cursor-default",
        )}
        aria-expanded={expandable ? expanded : undefined}
      >
        {/* Stage transition crossfade: when running flips to done
            (or errored), the icon and label swap on a 140ms
            opacity crossfade so the user reads "completed" as a
            morph, not a snap. Scoped narrowly to the icon and
            label — no layout, no y, no nested compounding. */}
        <AnimatePresence mode="wait" initial={false}>
          <motion.span
            key={errored ? "err" : running ? "run" : "done"}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.14 }}
            className="inline-flex"
          >
            <StatusIcon running={running} errored={errored} />
          </motion.span>
        </AnimatePresence>
        <span className="relative inline-flex min-w-0">
          <AnimatePresence mode="wait" initial={false}>
            <motion.span
              key={label}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.14 }}
              className="block truncate"
            >
              {label}
            </motion.span>
          </AnimatePresence>
        </span>
        {expandable ? (
          <ChevronRight
            className={cn(
              "h-3 w-3 shrink-0 text-fg-3 transition-transform duration-200",
              expanded ? "rotate-90" : "rotate-0",
            )}
            aria-hidden
          />
        ) : null}
      </button>
      <AnimatePresence initial={false}>
        {expanded && expandable && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
            className="overflow-hidden"
          >
            <div className="ml-[22px] mt-1 mb-1 max-w-full overflow-x-auto rounded-md border border-border/40 bg-surface-1 px-2.5 py-1.5 font-mono text-[11px] text-fg-3 space-y-1.5">
              {argsText && (
                <div>
                  <span className="text-fg-4">
                    {t.chat.thinking.detailArgsLabel}{" "}
                  </span>
                  <span className="text-fg-2 break-all">{argsText}</span>
                </div>
              )}
              {recallHits && <ChatRecallResults hits={recallHits} />}
              {resultText && (
                <div>
                  <span className="text-fg-4">
                    {t.chat.thinking.detailResultLabel}{" "}
                  </span>
                  <span className="text-fg-2 break-all whitespace-pre-wrap">
                    {resultText}
                  </span>
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function StatusIcon({
  running,
  errored,
}: {
  running: boolean;
  errored: boolean;
}) {
  if (errored) {
    // Muted amber, NOT destructive red. Red is reserved for
    // user-initiated destructive actions; a tool step that
    // failed is an attention moment, not an alarm. Tone-match
    // with the emerald done-check below — both are tailwind
    // *-500 mid-saturations so the visual weight is symmetric.
    return (
      <XCircle className="h-3.5 w-3.5 shrink-0 text-amber-500" aria-hidden />
    );
  }
  if (running) {
    return (
      <Loader2
        className="h-3.5 w-3.5 shrink-0 animate-spin text-fg-3"
        aria-hidden
      />
    );
  }
  return (
    <Check
      className="h-3.5 w-3.5 shrink-0 text-emerald-500"
      aria-hidden
      strokeWidth={2.5}
    />
  );
}

// ── Pure label helpers ────────────────────────────────────────

function compactJSON(value: unknown): string {
  try {
    const json = JSON.stringify(value);
    return json.length > 200 ? json.slice(0, 197) + "…" : json;
  } catch {
    return String(value);
  }
}

function formatResult(value: unknown): string {
  if (typeof value === "string") {
    return value.length > 400 ? value.slice(0, 397) + "…" : value;
  }
  return compactJSON(value);
}
