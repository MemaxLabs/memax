"use client";

import type { LucideIcon } from "lucide-react";
import { highlightMatch } from "@/lib/highlight-match";

function formatRelevancePercent(relevance: number | undefined) {
  if (relevance === undefined || !Number.isFinite(relevance)) return null;
  const normalized = relevance <= 1 ? relevance * 100 : relevance;
  const clamped = Math.max(0, Math.min(100, normalized));
  return `${Math.round(clamped)}%`;
}

export function SourceRow({
  index,
  title,
  snippet,
  query,
  relevance,
  topic,
  topicIcon: TopicIcon,
  hub,
  highlighted,
  onClick,
}: {
  index?: number;
  title: string;
  snippet?: string;
  query?: string;
  relevance?: number;
  topic?: string;
  topicIcon?: LucideIcon;
  hub?: string;
  highlighted?: boolean;
  onClick?: () => void;
}) {
  const relevanceLabel = formatRelevancePercent(relevance);

  return (
    <button
      type="button"
      onClick={onClick}
      data-nav-highlighted={highlighted || undefined}
      className={`group flex w-full cursor-pointer flex-col px-4 text-left transition-colors ${
        highlighted ? "bg-surface-1" : "hover:bg-surface-1"
      }`}
    >
      <div className="flex min-h-9 items-center gap-2">
        {index !== undefined ? (
          <span className="inline-flex shrink-0 items-center justify-center rounded bg-surface-2 px-1.5 py-px font-mono text-[12px] text-fg-3">
            {index}
          </span>
        ) : null}
        <p className="min-w-0 flex-1 truncate text-[14px] font-medium text-fg-1">
          {query ? highlightMatch(title, query) : title}
        </p>
        {topic ? (
          <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-fg-4">
            {TopicIcon ? <TopicIcon className="h-3 w-3" /> : null}
            {topic}
          </span>
        ) : null}
        {topic && hub ? (
          <span className="shrink-0 text-[10px] text-fg-4">·</span>
        ) : null}
        {hub ? (
          <span className="shrink-0 rounded bg-surface-1 px-1.5 py-px text-[11px] text-fg-3">
            {hub}
          </span>
        ) : null}
        {relevanceLabel ? (
          <span className="shrink-0 tabular-nums text-[11px] text-fg-3">
            {relevanceLabel}
          </span>
        ) : null}
      </div>
      {snippet ? (
        <p
          className="truncate pb-1.5 text-[13px] text-fg-3"
          style={index !== undefined ? { paddingLeft: 26 } : undefined}
        >
          {query ? highlightMatch(snippet, query) : snippet}
        </p>
      ) : null}
    </button>
  );
}
