// Wire types for GET /v1/admin/v2/metrics: plan 25's phase gates (§12),
// computed from the V2 record by weekly signup cohort (server:
// internal/ledger/product_metrics.go, migration 053). Counts and seconds
// only. Admin-only: never in the public SDK.

export type AdminCohortKind = "new" | "from_v1";

export type AdminPhaseGateStatus = "pass" | "fail" | "pending";

export type AdminPhaseGateName =
  | "activation"
  | "first_file"
  | "week4_keeping"
  | "team_pull";

export interface AdminCohortMetrics {
  /** Monday (UTC) of the signup week; absent on a total. */
  week?: string;
  cohort: AdminCohortKind;
  people: number;
  /** People whose first session (24 h) has ended: the denominator of the first-session counts. */
  sessions_closed: number;
  two_connections: number;
  two_agent_kinds: number;
  compiled: number;
  activated: number;
  agents_read: number;
  first_files: number;
  first_files_under_5m: number;
  first_file_p50_seconds: number | null;
  first_file_p90_seconds: number | null;
  signup_to_file_p50_seconds: number | null;
  retention_eligible: number;
  retained: number;
  team_eligible: number;
  team_pulled: number;
}

export interface AdminReviewHealth {
  /** Monday (UTC) of the week the proposals were made; absent on the total. */
  week?: string;
  proposals: number;
  kept: number;
  rejected: number;
  folded: number;
  forgotten: number;
  open: number;
  decision_p50_seconds: number | null;
  decision_p90_seconds: number | null;
}

export interface AdminPhaseGate {
  phase: number;
  name: AdminPhaseGateName;
  /** A rate, or for first_file the seconds the median must stay under. */
  bar: number;
  /** A rate, or for first_file the median seconds; null while pending. */
  value: number | null;
  numerator: number;
  denominator: number;
  status: AdminPhaseGateStatus;
  /** The same measure for people from V1, for context. */
  from_v1: number | null;
}

export interface AdminNorthStar {
  week_ending: string;
  spaces_read: number;
  spaces_two_agents: number;
  spaces_two_connections: number;
  spaces_hook_loads: number;
  connections_reading: number;
  connections_seen: number;
  coverage: number;
}

export interface AdminV2Metrics {
  /** Signups from (inclusive) … to (exclusive), RFC 3339. */
  from: string;
  to: string;
  as_of: string;
  first_session_hours: number;
  cohorts: AdminCohortMetrics[];
  totals: AdminCohortMetrics[];
  review: AdminReviewHealth[];
  review_total: AdminReviewHealth;
  gates: AdminPhaseGate[];
  north_star: AdminNorthStar;
}
