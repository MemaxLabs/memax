/**
 * chat-tool-labels — shared running/done label resolution for
 * tool-step rendering. Both `ChatStepRow` (the per-row view) and
 * `FoldedToolSummary` (the auto-fold pill) call into these so a
 * tool's running text and done text stay in sync between the
 * expanded and collapsed surfaces.
 *
 * `runningLabel(name)` — what the row shows while a tool's
 * result hasn't landed yet ("Searching memories…").
 * `doneLabel(tc)` — what the row shows once the result lands
 * ("Searched 3 memories" — recall counts unwrap from the tool
 * result's JSON shape).
 */

import type { ChatToolCallRecord } from "@/hooks/use-chat-stream";
import type { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";

export type ThinkingLabels = ReturnType<
  typeof useLocale
>["t"]["chat"]["thinking"];

export function runningLabel(name: string, tt: ThinkingLabels): string {
  switch (name) {
    case "recall_memories":
      return tt.recallMemoriesRunning;
    case "list_memories":
      return tt.listMemoriesRunning;
    case "get_memory":
      return tt.getMemoryRunning;
    case "list_hubs":
      return tt.listHubsRunning;
    case "get_hub":
      return tt.getHubRunning;
    case "list_topics":
      return tt.listTopicsRunning;
    case "push_memory":
      return tt.pushMemoryRunning;
    case "forget_memory":
      return tt.forgetMemoryRunning;
    case "propose_topic_merge":
      return tt.proposeTopicMergeRunning;
    case "fetch_url":
      return tt.fetchUrlRunning;
    default:
      return tt.genericRunning;
  }
}

export function doneLabel(tc: ChatToolCallRecord, tt: ThinkingLabels): string {
  switch (tc.name) {
    case "recall_memories": {
      const n = countRecallResults(tc.result);
      if (n === 0) return tt.recallMemoriesDoneZero;
      if (n === 1) return tt.recallMemoriesDoneOne;
      return interpolate(tt.recallMemoriesDoneN, { count: n });
    }
    case "list_memories":
      return tt.listMemoriesDone;
    case "get_memory":
      return tt.getMemoryDone;
    case "list_hubs":
      return tt.listHubsDone;
    case "get_hub":
      return tt.getHubDone;
    case "list_topics":
      return tt.listTopicsDone;
    case "push_memory":
      return tt.pushMemoryDone;
    case "forget_memory":
      return tt.forgetMemoryDone;
    case "propose_topic_merge":
      return tt.proposeTopicMergeDone;
    case "fetch_url":
      return tt.fetchUrlDone;
    default:
      return tt.genericDone;
  }
}

function countRecallResults(result: unknown): number {
  if (typeof result !== "string") return 0;
  try {
    const parsed = JSON.parse(result);
    if (Array.isArray(parsed)) return parsed.length;
    return 0;
  } catch {
    return 0;
  }
}
