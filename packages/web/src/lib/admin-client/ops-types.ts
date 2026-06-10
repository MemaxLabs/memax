// Types for /v1/admin/ops/*. Mirrors the server-side model.Ops* types
// in packages/server/internal/model/ops.go. Kept separate from the
// main `types.ts` because the ops surface is a self-contained module
// — fewer symbols in types.ts, easier to audit for the
// memax-sdk-boundary CI check.

export type AdminOpsJobState =
  | "available"
  | "running"
  | "scheduled"
  | "completed"
  | "discarded"
  | "cancelled"
  | "retryable"
  | "pending";

/** Worker client row from river_client joined with river_client_queue. */
export interface AdminOpsWorkerClient {
  client_id: string;
  queues: Array<{ queue: string; max_workers: number }>;
  heartbeat_at: string;
  age_seconds: number;
  healthy: boolean;
  is_leader: boolean;
}

export interface AdminOpsWorkers {
  healthy: number;
  stale: number;
  leader: string;
  clients: AdminOpsWorkerClient[];
}

export interface AdminOpsQueueDepth {
  queue: string;
  available: number;
  scheduled: number;
  running: number;
  retryable: number;
  pending: number;
  discarded: number;
}

export interface AdminOpsThroughput {
  window_minutes: number;
  memories_processed: number;
  memories_failed: number;
  dreams_completed: number;
  emails_sent: number;
}

export interface AdminOpsIngestionCards {
  in_processing: number;
  stuck_over_five_min: number;
  failed_last_hour: number;
}

export type AdminOpsRedFlagKind =
  | "worker_offline"
  | "stuck_memories"
  | "ingestion_failures"
  | "queue_backlog"
  | "no_successful_dream";

export type AdminOpsRedFlagSeverity = "info" | "warn" | "error";

export interface AdminOpsRedFlag {
  kind: AdminOpsRedFlagKind | string; // server may add new kinds
  severity: AdminOpsRedFlagSeverity;
  message: string;
  meta?: unknown;
}

export interface AdminOpsPulse {
  generated_at: string;
  workers: AdminOpsWorkers;
  queues: AdminOpsQueueDepth[];
  throughput: AdminOpsThroughput;
  red_flags: AdminOpsRedFlag[];
  ingestion: AdminOpsIngestionCards;
}

export interface AdminOpsJobProductContext {
  kind: string;
  label: string;
  sub_label?: string;
  resource_id?: string;
}

export interface AdminOpsJobSummary {
  id: number;
  kind: string;
  queue: string;
  state: AdminOpsJobState | string;
  attempt: number;
  max_attempts: number;
  priority: number;
  created_at: string;
  attempted_at?: string;
  finalized_at?: string;
  scheduled_at: string;
  last_error?: string;
  product_context?: AdminOpsJobProductContext;
}

export interface AdminOpsJobErrorRecord {
  at: string;
  attempt: number;
  error: string;
  trace?: string;
}

export interface AdminOpsJobDetail extends AdminOpsJobSummary {
  args: unknown;
  errors: AdminOpsJobErrorRecord[];
  metadata?: unknown;
  tags?: string[];
}

export interface AdminOpsJobListResponse {
  jobs: AdminOpsJobSummary[];
  next_cursor: string;
  has_more: boolean;
}

export interface AdminOpsJobListParams {
  states?: string[];
  /** Exact-set match. Prefer kind_search for free-text. */
  kinds?: string[];
  /** Case-insensitive substring match on river_job.kind. */
  kind_search?: string;
  /** Exact-set match. Prefer queue_search for free-text. */
  queues?: string[];
  /** Case-insensitive substring match on river_job.queue. */
  queue_search?: string;
  cursor?: string;
  limit?: number;
  created_after?: string;
  created_before?: string;
}

export interface AdminOpsStuckMemory {
  id: string;
  owner_id: string;
  hub_id: string;
  title: string;
  content_type: string;
  source: string;
  source_agent?: string;
  created_at: string;
  age_seconds: number;
  associated_job?: AdminOpsJobSummary;
}

export interface AdminOpsFailedIngestion {
  id: string;
  owner_id: string;
  hub_id: string;
  title: string;
  content_type: string;
  source: string;
  source_agent?: string;
  created_at: string;
  summary: string;
}

export interface AdminOpsIngestionBySource {
  source: string;
  memories_created: number;
  still_processing: number;
  failed_silent: number;
  window_minutes: number;
}

export interface AdminOpsIngestionSnapshot {
  stuck_memories: AdminOpsStuckMemory[];
  failed_ingestions: AdminOpsFailedIngestion[];
  by_source: AdminOpsIngestionBySource[];
  window_minutes: number;
  stuck_age_seconds: number;
}

/** SSE envelope on /v1/admin/ops/stream. */
export interface AdminOpsStreamEvent {
  type: "pulse" | "heartbeat";
  generated_at: string;
  pulse?: AdminOpsPulse;
}

/**
 * One log line returned from Loki. `level` is INFO/WARN/ERROR/DEBUG.
 * `fields` carries every slog attr that wasn't msg/level/time — the
 * caller-supplied structured data (memory_id, attempt, error, etc).
 */
export interface AdminOpsJobLogLine {
  timestamp: string;
  level: string;
  message: string;
  fields?: Record<string, unknown>;
}

/** One-shot GET /v1/admin/ops/jobs/{id}/logs payload. */
export interface AdminOpsJobLogsResponse {
  environment: string;
  since: string;
  until: string;
  lines: AdminOpsJobLogLine[];
}

/**
 * SSE frame for /v1/admin/ops/jobs/{id}/logs/stream.
 * - `snapshot` = the initial full window on connect.
 * - `append` = only lines newer than the last seen timestamp.
 * - `error` = upstream failure; stream will close right after.
 */
export interface AdminOpsJobLogsStreamEvent {
  type: "snapshot" | "append" | "error";
  environment?: string;
  lines?: AdminOpsJobLogLine[];
  error?: string;
  detail?: string;
}
