"use client";

import { ArrowRight } from "lucide-react";
import type { ReactNode } from "react";
import { pluralize } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";

export function buildHubHeaderStatsLine({
  stats,
  t,
  interpolate,
}: {
  stats: {
    memories: number;
    topics: number;
    inbox?: number;
    pendingReview?: number;
    members?: number;
  };
  t: Translations;
  interpolate: (template: string, vars: Record<string, string>) => string;
}): ReactNode {
  const parts: ReactNode[] = [];
  if (stats.memories > 0) {
    parts.push(
      <span key="mem" className="tabular-nums">
        {pluralize(t.topics.memoryOne, t.topics.memories, stats.memories)}
      </span>,
    );
  }
  if (stats.topics > 0) {
    parts.push(
      <span key="topics" className="tabular-nums">
        {pluralize(t.topics.topicOne, t.topics.topics, stats.topics)}
      </span>,
    );
  }
  if (stats.members && stats.members > 0) {
    parts.push(
      <span key="members" className="tabular-nums">
        {pluralize(t.hubs.memberOne, t.hubs.members, stats.members)}
      </span>,
    );
  }
  if (stats.pendingReview && stats.pendingReview > 0) {
    parts.push(
      <span
        key="review"
        className="tabular-nums font-medium"
        style={{ color: "oklch(0.55 0.15 290)" }}
      >
        ✦{" "}
        {interpolate(t.hubHeader.stats.pendingReview, {
          n: String(stats.pendingReview),
        })}
      </span>,
    );
  }

  if (parts.length === 0) {
    return (
      <span className="text-[13px] text-fg-3">
        {t.hubHeader.stats.cta} <ArrowRight className="inline h-3 w-3" />
      </span>
    );
  }

  return (
    <div className="flex items-center flex-wrap gap-x-2 text-[12px] text-fg-3">
      {parts.map((part, i) => (
        <span key={i} className="flex items-center gap-2">
          {i > 0 && <span className="text-fg-4">·</span>}
          {part}
        </span>
      ))}
    </div>
  );
}
