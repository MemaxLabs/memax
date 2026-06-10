"use client";

/**
 * ChatToolCallList — collapsible inline cards rendering each tool
 * invocation under an assistant message. Plan 24 §"UI surface"
 * pins the shape: `▸ recall_memories(query="…") → 5 memories`.
 *
 * Args render as compact JSON for transparency; results render as
 * a one-line preview with full content available on click. Errors
 * surface a destructive tint on the chip.
 */

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "@memaxlabs/ui";
import type { ChatToolCallRecord } from "@/hooks/use-chat-stream";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";

export function ChatToolCallList({
  toolCalls,
}: {
  toolCalls: ChatToolCallRecord[];
}) {
  return (
    <div className="flex flex-col gap-1.5 w-full">
      {toolCalls.map((tc, idx) => (
        <ChatToolCallChip key={tc.id ?? idx} tc={tc} />
      ))}
    </div>
  );
}

function ChatToolCallChip({ tc }: { tc: ChatToolCallRecord }) {
  const { t } = useLocale();
  const [expanded, setExpanded] = useState(false);
  const running = tc.result === undefined && !tc.is_error;
  const errored = Boolean(tc.is_error);

  const label = running
    ? interpolate(t.chat.toolCall.running, { name: tc.name })
    : errored
      ? interpolate(t.chat.toolCall.error, { name: tc.name })
      : interpolate(t.chat.toolCall.done, { name: tc.name });

  const argsText = tc.args ? compactJSON(tc.args) : undefined;
  const resultText = tc.result ? formatResult(tc.result) : undefined;
  const expandable = Boolean(argsText || resultText);

  return (
    <div
      className={cn(
        "rounded-lg border text-[12px] transition-colors",
        errored
          ? "border-destructive/30 bg-destructive/5"
          : "border-border/60 bg-surface-1",
      )}
    >
      <button
        type="button"
        onClick={() => expandable && setExpanded((v) => !v)}
        className={cn(
          "flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left",
          expandable ? "cursor-pointer hover:bg-surface-2" : "cursor-default",
        )}
        aria-expanded={expandable ? expanded : undefined}
      >
        {expandable ? (
          expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0 text-fg-3" aria-hidden />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0 text-fg-3" aria-hidden />
          )
        ) : (
          <span className="h-3 w-3 shrink-0" />
        )}
        <span className={cn("font-mono text-fg-2", running && "text-fg-3")}>
          {label}
        </span>
      </button>
      {expanded && (argsText || resultText) && (
        <div className="border-t border-border/40 px-2.5 py-2 font-mono text-[11px] text-fg-3 space-y-1.5">
          {argsText && (
            <div>
              <span className="text-fg-4">args: </span>
              <span className="text-fg-2 break-all">{argsText}</span>
            </div>
          )}
          {resultText && (
            <div>
              <span className="text-fg-4">result: </span>
              <span className="text-fg-2 break-all whitespace-pre-wrap">
                {resultText}
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

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
