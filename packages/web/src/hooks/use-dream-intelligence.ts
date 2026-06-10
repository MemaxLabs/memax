"use client";

import { useAuth } from "@/lib/auth";
import { useDreamReport } from "@/hooks/use-dreams";

export interface DreamIntelligenceSnapshot {
  latestRun: {
    merged: number;
    contradictionsFound: number;
    archived: number;
    organized: number;
    restructured: number;
  } | null;
  pending: {
    contradictions: number;
    topicMerges: number;
    topicRestructures: number;
    total: number;
  };
}

/**
 * Consolidates dream intelligence into a semantic model for one hub:
 *
 * - `latestRun`     = what the latest dream did in this hub
 * - `pending`       = what still needs review in this hub right now
 *
 * This intentionally keeps "latest run outcome" separate from "current
 * attention queue" so surfaces do not mix historical run totals with live
 * pending notifications as if they were the same number.
 */
export function useDreamIntelligence(hubIdOverride?: string) {
  const { activeHubId } = useAuth();
  const hubId = hubIdOverride ?? activeHubId;
  const { data: dreamReport } = useDreamReport(hubId);
  const latest = dreamReport?.intelligence.latest_run;
  const latestRun = latest
    ? {
        merged: latest.merged,
        contradictionsFound: latest.contradictions_found,
        archived: latest.archived,
        organized: latest.organized,
        restructured: latest.restructured,
      }
    : null;

  const pendingReview = dreamReport?.intelligence.pending_review;
  const pending = {
    contradictions: pendingReview?.contradictions ?? 0,
    topicMerges: pendingReview?.topic_merges ?? 0,
    topicRestructures: pendingReview?.topic_restructures ?? 0,
  };

  const snapshot: DreamIntelligenceSnapshot = {
    latestRun,
    pending: {
      ...pending,
      total:
        pending.contradictions +
        pending.topicMerges +
        pending.topicRestructures,
    },
  };

  return {
    hubId,
    dreamReport,
    snapshot,
  };
}
