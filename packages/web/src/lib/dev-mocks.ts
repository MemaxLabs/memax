/**
 * Dev mock data for testing dream UI without backend dream runs.
 * Shapes match Go backend structs exactly (model.go).
 *
 * Gated by useDevMode().flags.mockDreams — never loaded in production.
 */

import type { DreamAction, DreamReport, DreamRun } from "memax-sdk";

// Matches model.DreamRun JSON output
const MOCK_RUN: DreamRun = {
  id: "dev-dream-run-001",
  owner_id: "",
  status: "completed" as const,
  started_at: new Date(Date.now() - 8 * 3600_000).toISOString(),
  finished_at: new Date(Date.now() - 7 * 3600_000).toISOString(),
  memories_scanned: 247,
  duplicates_merged: 3,
  contradictions_found: 1,
  memories_archived: 2,
  memories_organized: 5,
  report:
    "Scanned 247 memories. Merged 3 duplicates, found 1 contradiction, archived 2 stale notes, organized 5 into topics.",
};

// Matches model.DreamAction JSON output
const MOCK_ACTIONS: DreamAction[] = [
  {
    id: "dev-action-merge-001",
    run_id: "dev-dream-run-001",
    action_type: "merge",
    source_memory_ids: ["mem-a1", "mem-a2"],
    result_memory_id: "mem-a1",
    reason: "React Server Components rendering pipeline",
    similarity: 0.92,
    created_at: new Date(Date.now() - 7 * 3600_000).toISOString(),
  },
  {
    id: "dev-action-archive-001",
    run_id: "dev-dream-run-001",
    action_type: "archive",
    source_memory_ids: ["mem-b1"],
    result_memory_id: "",
    reason: "Last accessed 90 days ago. No recent recalls matched.",
    similarity: 0,
    created_at: new Date(Date.now() - 7 * 3600_000).toISOString(),
  },
  {
    id: "dev-action-contradiction-001",
    run_id: "dev-dream-run-001",
    action_type: "contradiction",
    source_memory_ids: ["mem-c1", "mem-c2"],
    result_memory_id: "",
    reason: "Two notes disagree about the default Redis cache TTL value.",
    similarity: 0.87,
    created_at: new Date(Date.now() - 7 * 3600_000).toISOString(),
  },
];

// Running dream state — for testing "Dreaming..." banner
const MOCK_RUN_DREAMING: DreamRun = {
  id: "dev-dream-run-002",
  owner_id: "",
  status: "running" as const,
  started_at: new Date(Date.now() - 30_000).toISOString(),
  finished_at: "",
  memories_scanned: 247,
  duplicates_merged: 0,
  contradictions_found: 0,
  memories_archived: 0,
  memories_organized: 0,
  report: "",
};

// Matches GET /v1/dreams/report response envelope (handler/dreams.go)
export const MOCK_DREAM_REPORT: DreamReport = {
  has_run: true as const,
  run: MOCK_RUN,
  actions: MOCK_ACTIONS,
  intelligence: {
    latest_run: {
      merged: MOCK_RUN.duplicates_merged,
      contradictions_found: MOCK_RUN.contradictions_found,
      archived: MOCK_RUN.memories_archived,
      organized: MOCK_RUN.memories_organized,
      restructured: MOCK_RUN.topics_restructured ?? 0,
    },
    pending_review: {
      contradictions: 1,
      topic_merges: 0,
      topic_restructures: 0,
      total: 1,
    },
  },
};

export const MOCK_DREAM_REPORT_RUNNING: DreamReport = {
  has_run: true as const,
  run: MOCK_RUN_DREAMING,
  actions: [],
  intelligence: {
    latest_run: {
      merged: 0,
      contradictions_found: 0,
      archived: 0,
      organized: 0,
      restructured: 0,
    },
    pending_review: {
      contradictions: 0,
      topic_merges: 0,
      topic_restructures: 0,
      total: 0,
    },
  },
};
