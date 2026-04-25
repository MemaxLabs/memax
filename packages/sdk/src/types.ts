// =============================================================================
// memax-sdk — Type definitions for the Memax API
// =============================================================================

// --- Client Configuration ---

/**
 * Async function that returns authorization headers.
 * Used by CLI and custom integrations that manage their own auth flow.
 */
export type AuthProvider = () => Promise<Record<string, string>>;

export interface MemaxConfig {
  /** API base URL (default: https://api.memax.app) */
  apiUrl?: string;
  /** Static API key — simplest auth for external developers */
  apiKey?: string;
  /** Dynamic auth provider — for CLI/custom integrations that manage tokens */
  auth?: AuthProvider;
  /** Custom fetch implementation for Node, tests, SSR, or edge runtimes */
  fetch?: typeof globalThis.fetch;
  /** Default headers applied to every request before auth headers */
  headers?: Record<string, string>;
  /** Number of retries for retryable API responses like `not_ready` */
  maxRetries?: number;
  /** Delay between retry attempts in milliseconds */
  retryDelayMs?: number;
  /** Optional hook for warning headers returned by the API transport. */
  onWarning?: (warning: string) => void;
}

// --- Request Options ---

export type AskModel = "auto" | "haiku" | "sonnet";
export type Locale = "en" | "zh";
export type MemoryKind = "episodic" | "semantic" | "procedural" | "rationale";
export type MemoryStability = "volatile" | "evolving" | "stable";

export interface PushOptions {
  title?: string;
  /** Context hint to help AI process this memory (e.g. "This is my resume"). Improves summarization and retrieval. */
  hint?: string;
  tags?: string[];
  source?: string;
  /** Agent identity: "claude-code", "cursor", "copilot", etc. Auto-set by hooks/MCP. */
  sourceAgent?: string;
  /** Human collaboration credit, not actor identity. */
  assistedByAgent?: string;
  initiationType?:
    | "human_direct"
    | "human_requested_agent"
    | "agent_proactive"
    | "agent_automatic"
    | "import"
    | "unknown";
  sourcePath?: string;
  contentType?: string;
  projectContext?: Record<string, string>;
  hubId?: string;
  hubReason?: string;
  fileRef?: FileRef;
}

export interface RecallOptions {
  limit?: number;
  kind?: MemoryKind;
  tags?: string[];
  /** Restrict results to memories in this topic. */
  topicId?: string;
  source?: string;
  includeArchived?: boolean;
  /** Skip reranking. Useful for faster results or debugging scoring. */
  noRerank?: boolean;
  workingDir?: string;
  projectContext?: Record<string, string>;
  /** Active hub ranking hint. Recall still searches every hub the token can access. */
  hubId?: string;
  /** Forwarded to fetch — wire React Query's `signal` through for true cancellation. */
  signal?: AbortSignal;
}

export interface AskOptions {
  limit?: number;
  model?: AskModel;
  locale?: Locale;
  /** Skip reranking for source retrieval. */
  noRerank?: boolean;
  /** Active hub ranking hint for Ask retrieval. Ask still searches every hub the token can access. */
  hubId?: string;
  /** Restrict ask retrieval to memories in this topic. */
  topicId?: string;
  /** Forwarded to fetch — wire React Query's `signal` through for true cancellation. */
  signal?: AbortSignal;
}

export interface ListMemoriesOptions {
  limit?: number;
  cursor?: string;
  sort?: "newest" | "relevant";
  kind?: MemoryKind;
  createdAfter?: string;
  actor?: string;
  hubId?: string;
  topicId?: string;
  /** Forwarded to fetch — wire React Query's `signal` through for true cancellation. */
  signal?: AbortSignal;
}

export interface MemoryActorCounts {
  [actor: string]: number;
}

// --- Core Domain Types ---

export type MemoryAttachmentKind = "original";

export interface Memory {
  id: string;
  hub_id: string;
  owner_id: string;
  title: string;
  content: string;
  content_type: string;
  content_hash: string;
  summary: string;
  hint?: string;
  kind: MemoryKind;
  stability: MemoryStability;
  retrieval_weight: number;
  access_intents?: Record<string, number>;
  tags: string[];
  boundary: string;
  state: string;
  pinned: boolean;
  source: string;
  source_agent?: string;
  assisted_by_agent?: string;
  provenance?: MemoryProvenance;
  source_path?: string;
  hub_reason?: string;
  original_file_ref?: string;
  attachments?: MemoryAttachment[];
  project_context?: Record<string, string>;
  topic_id?: string;
  version: number;
  access_count: number;
  created_at: string;
  updated_at: string;
  accessed_at: string;
  author_name?: string;
  author_avatar_url?: string;
  hub_name?: string;
  agent_display_name?: string;
  agent_icon?: string;
  /** Server-resolved dream-delta signals. See MemoryLifecycle docs. */
  lifecycle?: MemoryLifecycle;
}

export interface MemoryProvenance {
  created_by_type: "human" | "agent";
  created_by_slug?: string;
  created_by_display_name?: string;
  created_via?: string;
  assisted_by_agent?: string;
  initiation_type:
    | "human_direct"
    | "human_requested_agent"
    | "agent_proactive"
    | "agent_automatic"
    | "import"
    | "unknown";
  attribution_source?: string;
}

/**
 * MemoryLifecycle — durable dream-delta signals for memory rows + detail.
 *
 * Two fields with distinct scopes:
 *   - pending_dream_action drives scan surfaces (row breadcrumb tint).
 *     Scoped to the viewer's last visit of the memory's current topic;
 *     server returns null after the viewer visits.
 *   - dream_history drives the memory detail provenance strip. Up to 10
 *     most recent dream actions, unscoped, durable. Empty array on list
 *     reads; populated only on detail reads.
 */
export interface MemoryLifecycle {
  pending_dream_action: DreamActionRef | null;
  dream_history: DreamActionRef[];
}

/**
 * DreamActionRef — client-facing shape of a dream action.
 *
 * from_topic / to_topic are nullable — historical rows (pre-migration 069)
 * stay null and UI renders verb + reason without lineage.
 */
export interface DreamActionRef {
  run_id: string;
  action_type: "organize" | "merge" | "archive" | "restructure";
  at: string;
  from_topic: DreamTopicRef | null;
  to_topic: DreamTopicRef | null;
  reason?: string;
}

export interface DreamTopicRef {
  id: string;
  name: string;
  /**
   * Lucide icon name at the time the reference was resolved (e.g.
   * "folder", "code"). Empty/undefined when the topic has no icon.
   * Server uses the scoped topic join, so this field is never present
   * when the topic is out of the viewer's scope (whole ref is null in
   * that case). UI chips render `icon + name` to keep topic identity
   * consistent across row breadcrumb, dream history, and hover card.
   */
  icon?: string;
}

export interface MemoryUpdateInput {
  title?: string;
  kind?: MemoryKind;
  stability?: MemoryStability;
  tags?: string[];
}

export type ShareMemoryResult = Memory;

/**
 * Reason a batch-move request skipped a given memory id.
 * - `not_owned`: the caller does not own this memory (team-hub shared content).
 * - `not_found`: the id does not exist or has been deleted.
 * - `already_at_target`: the memory's current hub + topic already matches.
 * - `source_delete_forbidden`: the caller owns the memory but lacks
 *   authority to remove it from its current hub. Move is semantically
 *   delete-from-source + create-in-destination, so the source hub's
 *   `contributor_delete_policy` must be honored. Emitted only for
 *   cross-hub moves; same-hub topic reassignments skip the check.
 */
export type BatchMoveSkipReason =
  | "not_owned"
  | "not_found"
  | "already_at_target"
  | "source_delete_forbidden";

export interface BatchMoveSkippedMemory {
  id: string;
  reason: BatchMoveSkipReason;
}

/**
 * Structured response from `POST /v1/memories/batch-move`.
 *
 * `moved` is the count of memories actually reassigned in this request.
 * `skipped` lists every input id that was not moved, with a reason code.
 * Consumers should treat any `moved > 0` as partial success and surface
 * skipped counts to the user so they know why fewer moved than requested.
 */
export interface BatchMoveResult {
  moved: number;
  skipped: BatchMoveSkippedMemory[];
}

/**
 * Reason a batch-delete request skipped a given memory id.
 * - `not_owned`: the caller does not own the memory and does not hold a
 *   hub role + contributor_delete_policy that covers it. Single code for
 *   both "wrong owner, no hub context" and "hub member but policy forbids."
 * - `not_found`: the id does not exist at request time. Covers unknown
 *   ids AND the race where a row is removed concurrently between
 *   accessibility load and the actual DELETE.
 * - `delete_failed`: a store-level error occurred while attempting to
 *   delete this id (postgres transient error, object-store failure).
 *   Distinguished from `not_found` so clients can retry infra issues
 *   without blaming the user. Server logs the underlying error.
 */
export type BatchDeleteSkipReason = "not_owned" | "not_found" | "delete_failed";

export interface BatchDeleteSkippedMemory {
  id: string;
  reason: BatchDeleteSkipReason;
}

/**
 * Structured response from `POST /v1/memories/batch-delete`.
 *
 * `deleted` is the count of memories actually removed in this request.
 * `skipped` lists every input id that was not deleted, with a reason.
 *
 * Partial-success semantics: `deleted > 0` with non-empty `skipped` is a
 * normal, intentional outcome — the server commits what it can and
 * reports the rest. Clients should surface the partial count to the user.
 *
 * Full-skip semantics: `deleted === 0` can happen for three different
 * reasons (all-not_owned, all-not_found, all-delete_failed). Clients
 * should inspect `skipped[].reason` to pick appropriate copy rather than
 * showing one generic error for all three. `not_found` in particular is
 * usually the user's desired end state (the memory is gone) and should
 * be treated as effective success.
 */
export interface BatchDeleteResult {
  deleted: number;
  skipped: BatchDeleteSkippedMemory[];
}

/**
 * Skip reasons for `POST /v1/configs/batch-delete`. The agent config reason
 * set is deliberately narrower than `BatchDeleteSkipReason`:
 *
 * - `not_found`: the config id does not resolve for the caller. The server
 *   collapses "unknown id", "already deleted", and "owned by another user"
 *   into this single reason because the SQL WHERE clause makes them
 *   indistinguishable without an extra round-trip, and all three map to
 *   idempotent success on the client (the user's target state is reached).
 * - `delete_failed`: the tombstone insert or the DELETE itself returned an
 *   error mid-batch. Distinguished from `not_found` so the client can
 *   retry infra failures and roll back the optimistic list removal.
 *
 * There is deliberately no `not_owned` — the agent config store enforces
 * ownership via the WHERE clause, so the web hook's catch branch does not
 * need to handle a permission-style reason. Keeping the union narrow means
 * dead code (e.g. a `forgetDenied` toast branch) would be caught at
 * compile time.
 */
export type AgentConfigDeleteSkipReason = "not_found" | "delete_failed";

export interface AgentConfigDeleteSkippedItem {
  id: string;
  reason: AgentConfigDeleteSkipReason;
}

/**
 * Structured response from `POST /v1/configs/batch-delete`.
 *
 * Shares the same shape as `BatchDeleteResult` (deleted count + skipped
 * array) but narrows the reason union to the agent-config taxonomy. The
 * server always returns 200 with this body in the ApiResponse envelope —
 * 4xx/5xx are reserved for full-request failures.
 *
 * Partial-success is the default: `deleted > 0` with non-empty `skipped`
 * means the server committed what it could and reports the rest so the
 * client can surface a partial-success toast.
 */
export interface AgentConfigBatchDeleteResult {
  deleted: number;
  skipped: AgentConfigDeleteSkippedItem[];
}

export interface MemoryAttachment {
  id: string;
  memory_id: string;
  owner_id: string;
  kind: MemoryAttachmentKind;
  filename: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  /** Width in pixels. Present only for images that decoded successfully
   * at upload time. Use with `height` to reserve layout space and
   * avoid CLS when rendering inline previews. */
  width?: number | null;
  /** Height in pixels. See `width`. */
  height?: number | null;
  /** True when the signed view endpoint may serve this attachment
   * with Content-Disposition: inline. Clients should require BOTH
   * this flag AND a raster content_type before rendering via
   * <img src> — defense in depth against partially-migrated rows. */
  inline_eligible?: boolean;
  created_at: string;
}

/** Short-lived signed URL for rendering an attachment inline. Caller
 * passes this straight to <img src>. Reuse within the TTL window is
 * expected — the same image renders across inbox modal, memory
 * detail, and lightbox without re-signing. */
export interface AttachmentViewURL {
  url: string;
  /** Unix seconds UTC. Clients should refresh shortly before expiry. */
  expires_at: number;
}

export interface FileRef {
  object_key: string;
  filename: string;
  content_type: string;
  size_bytes?: number;
  sha256?: string;
}

/** Discriminates the two legitimate upload flows. */
export type UploadPurpose = "memory_attachment" | "agent_session";

export interface UploadIntent {
  object_key: string;
  upload_url: string;
  headers: Record<string, string>;
  filename: string;
  content_type: string;
  size_bytes: number;
  /** The purpose echoed back from the server for client bookkeeping. */
  purpose: UploadPurpose;
  /** The effective server-side cap for this purpose (bytes). */
  max_bytes: number;
  expires_in: number;
}

export interface RelatedMemory {
  memory: Memory;
  similarity: number;
}

export interface RecalledMemory {
  id: string;
  title: string;
  summary?: string;
  hint?: string;
  chunk_content: string;
  heading_chain: string;
  relevance_score: number;
  kind: MemoryKind;
  stability: MemoryStability;
  source: string;
  age: string;
  created_at?: string;
  author_name?: string;
  hub_id?: string;
  hub_name?: string;
  topic_id?: string;
  topic_name?: string;
}

/** Fast FTS result (trigram + tsvector, no embeddings) */
export interface SearchResult {
  memory_id: string;
  title: string;
  snippet: string;
  kind: MemoryKind;
  stability: MemoryStability;
  heading_chain?: string;
  // Hub/topic attribution enriched server-side. Present when the
  // viewer can access the memory's hub/topic — allows quick-match
  // rows to render the correct attribution chip for cross-hub
  // results that would otherwise have no cached source.
  hub_id?: string;
  hub_name?: string;
  topic_id?: string;
  topic_name?: string;
}

/** Topic row surfaced by the bar's unified search endpoint. */
export interface BarTopicMatch {
  id: string;
  name: string;
  description?: string;
  hub_id: string;
}

/** Hub row surfaced by the bar's unified search endpoint. */
export interface BarHubMatch {
  id: string;
  name: string;
}

/**
 * Unified payload for the bar's quick-match layer. Memory rows come
 * from the same FTS path as `memories.search`; topics and hubs are
 * substring-matched against the query server-side.
 */
export interface BarSearchResult {
  query: string;
  memories: SearchResult[];
  topics: BarTopicMatch[];
  hubs: BarHubMatch[];
}

export interface RecallResult {
  memories: RecalledMemory[];
  query_metadata: {
    intent: string;
    kinds_searched?: MemoryKind[];
    total_candidates: number;
    latency_ms: number;
  };
}

export interface AskResult {
  answer: string;
  citations: {
    index: number;
    memory_id: string;
    title: string;
    kind: MemoryKind;
  }[];
  sources: RecalledMemory[];
  metadata: {
    model: string;
    answer_tokens: number;
    retrieval_latency_ms: number;
    synthesis_latency_ms: number;
    total_latency_ms: number;
  };
}

export interface ListMemoriesResult {
  memories: Memory[];
  next_cursor: string;
  has_more: boolean;
  total: number;
  actors?: MemoryActorCounts;
}

// --- Agent Config Sync ---

export type Scope = "global" | `project:${string}`;

export interface AgentConfig {
  id: string;
  owner_id: string;
  agent: string;
  file_path: string;
  scope: Scope;
  content: string;
  content_hash: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface AgentConfigListResult {
  configs: AgentConfig[];
  extraction_counts?: Record<string, number>;
}

export interface DeletedAgentConfig {
  agent: string;
  file_path: string;
  scope: Scope;
  version: number;
  deleted_at: string;
  deleted_content_hash?: string;
  content_expires_at?: string;
}

export type SyncAction =
  | "unchanged"
  | "push"
  | "pull"
  | "conflict"
  | "delete_local";

export interface SyncPlanAction {
  action: SyncAction;
  agent: string;
  file_path: string;
  scope: Scope;
  reason?: string;
  config_id?: string;
  cloud_hash?: string;
  cloud_updated_at?: string;
  version?: number;
}

export interface SyncManifestEntry {
  agent: string;
  file_path: string;
  scope: Scope;
  content_hash: string;
  updated_at: string;
  local_path?: string;
}

export interface ConfigSyncRequest {
  device_id: string;
  configs: SyncManifestEntry[];
}

export interface AgentConfigSyncAck {
  agent: string;
  file_path: string;
  scope: Scope;
  content_hash?: string;
  version: number;
  local_path?: string;
  deleted?: boolean;
}

export interface ConfigSyncAckRequest {
  device_id: string;
  configs: AgentConfigSyncAck[];
}

export interface ConfigLocalDeleteRequest {
  device_id: string;
  agent: string;
  file_path: string;
  scope: Scope;
  local_path?: string;
}

export interface RestoreDeletedConfigRequest {
  agent: string;
  file_path: string;
  scope?: Scope;
  device_id?: string;
  local_path?: string;
}

export interface ConfigUpsertRequest {
  agent: string;
  file_path: string;
  scope: Scope;
  content: string;
  device_id?: string;
  local_path?: string;
}

export interface ConfigMergeRequest {
  local_content: string;
  cloud_content: string;
  file_path: string;
  agent: string;
}

// --- Agent Session Sync ---

export interface AgentSession {
  id: string;
  owner_id: string;
  agent: string;
  file_path: string;
  scope: Scope;
  session_type: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  content_hash: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface DeletedAgentSession {
  agent: string;
  file_path: string;
  scope: Scope;
  version: number;
  deleted_at: string;
  session_type?: string;
  filename?: string;
  content_type?: string;
  size_bytes?: number;
  content_hash?: string;
  content_expires_at?: string;
}

export type SessionSyncAction =
  | "unchanged"
  | "push"
  | "pull"
  | "diverged" // both changed (live session) — use resolve-divergence RPC
  | "tombstone_diverged" // cloud deleted but local changed — push to re-create or delete local
  | "delete_local";

export interface SessionSyncPlanAction {
  action: SessionSyncAction;
  agent: string;
  file_path: string;
  scope: Scope;
  reason?: string;
  session_id?: string;
  cloud_hash?: string;
  cloud_updated_at?: string;
  cloud_version?: number;
  version?: number;
}

/** Request to atomically resolve a diverged session. */
export interface ResolveDivergenceRequest {
  agent: string;
  file_path: string;
  scope: Scope;
  device_id: string;
  local_file_ref?: FileRef;
  local_content_hash: string;
  expected_cloud_version: number;
  expected_cloud_hash: string;
  resolution: "keep_local" | "keep_cloud";
}

/** Response from a successful divergence resolution. */
export interface ResolveDivergenceResponse {
  winner: "local" | "cloud";
  snapshot_id: string;
  snapshot_device: string;
  new_version: number;
}

export interface SessionSyncManifestEntry {
  agent: string;
  file_path: string;
  scope: Scope;
  content_hash: string;
  local_path?: string;
}

export interface AgentSessionSyncRequest {
  device_id: string;
  sessions: SessionSyncManifestEntry[];
}

export interface AgentSessionSyncAck {
  agent: string;
  file_path: string;
  scope: Scope;
  content_hash?: string;
  version: number;
  local_path?: string;
  deleted?: boolean;
}

export interface AgentSessionSyncAckRequest {
  device_id: string;
  sessions: AgentSessionSyncAck[];
}

export interface AgentSessionLocalDeleteRequest {
  device_id: string;
  agent: string;
  file_path: string;
  scope: Scope;
  local_path?: string;
}

export interface RestoreDeletedSessionRequest {
  agent: string;
  file_path: string;
  scope?: Scope;
  device_id?: string;
  local_path?: string;
}

export interface AgentSessionUpsertRequest {
  agent: string;
  file_path: string;
  scope: Scope;
  session_type?: string;
  content_hash?: string;
  device_id?: string;
  local_path?: string;
  file_ref: FileRef;
}

// --- Auth & API Keys ---

export type AuthProviderName = "github" | "google";

export interface User {
  id: string;
  name: string;
  email: string;
  plan: string; // legacy — use personal_plan_id for scoped resolution
  personal_plan_id: string; // scoped personal plan (personal_free, personal_pro, etc.)
  display_name?: string;
  avatar_url?: string;
  can_create_hub?: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface Usage {
  user_id: string;
  period_start: string;
  period_end: string;
  push_count: number;
  recall_count: number;
  ask_count: number;
}

// --- Plans & Limits ---

/** Plan scope distinguishes personal plans (assigned to users) from hub plans (assigned to team hubs). */
export type PlanScope = "personal" | "hub";

export interface PlanDefinition {
  id: string;
  scope: PlanScope;
  display_name: string;
  tier_order: number;
  entitlement_rank: number;
  monthly_price_cents: number;
  memory_limit: number;
  push_limit: number;
  recall_limit: number;
  ask_limit: number;
  max_attachment_bytes: number; // per-file cap for memory_attachment uploads; -1 unlimited
  storage_bytes_limit: number; // total cumulative cap across attachments + sessions; -1 unlimited
  ask_model: "haiku" | "sonnet";
  dreams_enabled: boolean;
  review_inbox: boolean;
  max_team_hubs: number;
  // Scoped ownership and hub fields
  max_owned_free_team_hubs: number; // personal plan: how many free team hubs the user can own
  max_hub_members: number | null; // hub plan: member cap; null = unlimited
  seat_minimum: number; // hub plan: billing floor for paid subscriptions
  seat_billed: boolean; // hub plan: whether seat count is billed externally
  rate_limit_rpm: number;
  rate_limit_heavy_rpm: number;
  rate_limit_light_rpm: number;
  features: Record<string, unknown>;
  active: boolean;
  created_at: string;
  updated_at: string;
}

// Admin APIs are an internal, web-only surface and are intentionally not
// exported by this public SDK.

export interface UserLimits {
  plan_id: string;
  plan_display_name: string;
  memory_limit: number;
  push_limit: number;
  recall_limit: number;
  ask_limit: number;
  ask_model: string;
  dreams_enabled: boolean;
  review_inbox: boolean;
  max_team_hubs: number;
  rate_limit_rpm: number;
  rate_limit_heavy_rpm: number;
  rate_limit_light_rpm: number;
}

/** Enriched usage response with plan limits. Backward-compatible with Usage. */
export interface UsageWithLimits extends Usage {
  limits: UserLimits;
  plan: string;
  plan_display_name: string;
}

export interface MeResponse {
  user: User;
  connected_providers?: AuthProviderName[];
  hubs?: Array<{
    hub: { id: string; name: string; hub_type: HubType };
    role: HubRole;
  }>;
  /** Basic usage from /v1/auth/me. For enriched usage with limits, use settings.usage(). */
  usage?: Usage;
}

export interface AuthTokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface ImpersonationResult {
  access_token: string;
  expires_in: number;
  target_id: string;
  impersonated: true;
}

export interface AuthIdentity {
  id: string;
  user_id: string;
  provider: AuthProviderName;
  provider_id: string;
  provider_email: string;
  provider_name: string;
  created_at: string;
}

export interface UnlinkProviderResult {
  status: "unlinked";
}

export interface ApiKeyCreateOptions {
  name: string;
  hubId?: string;
  hubIds?: string[];
  agentName?: string; // agent identity: "claude-code", "cursor", etc.
  expiresInDays?: number;
  scopes?: string[];
  permissions?: string[];
  trustLevel?: "public" | "standard" | "elevated" | "admin";
}

export interface ApiKey {
  id: string;
  name: string;
  key: string;
  prefix: string;
  scope: string;
  hub_id: string | null;
  hub_ids?: string[];
  hub_scope_mode?: "all_accessible" | "hub_allowlist";
  default_permissions?: string[];
  trust_level?: "public" | "standard" | "elevated" | "admin";
  expires_at: string | null;
  created_at: string;
}

export interface ApiKeyListItem {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  scope: string;
  hub_id: string | null;
  hub_ids?: string[];
  hub_scope_mode?: "all_accessible" | "hub_allowlist";
  default_permissions?: string[];
  trust_level?: "public" | "standard" | "elevated" | "admin";
  /** Structured agent attribution. Empty string = unassigned. */
  agent_name?: string;
  /**
   * Explicit "no agent, by design" flag. Keys used by CI/scripts can be
   * marked standalone so the Assign affordance stops nagging. Defaults
   * to false; every new unassigned key shows the affordance until the
   * user either assigns an agent or marks it standalone.
   */
  standalone?: boolean;
  expires_at: string | null;
  last_used: string | null;
  created_at: string;
}

export interface UpdateApiKeyPayload {
  /** Empty string clears the assignment. Omit to leave unchanged. */
  agent_name?: string;
  /** Toggle explicit "no agent, by design" state. Omit to leave unchanged. */
  standalone?: boolean;
}

export interface UpdateApiKeyResult {
  id: string;
  agent_name: string;
  standalone: boolean;
}

export interface UpdateProfileResult {
  status: string;
  display_name: string;
}

export interface DeleteAllDataResult {
  deleted: boolean;
}

export interface OAuthConsentHub {
  id: string;
  name: string;
  slug: string;
  role: HubRole;
  hub_type: HubType;
  memory_count: number;
  checked: boolean;
  disabled: boolean;
  capability_label: string;
  supported_permissions: string[];
}

export interface OAuthConsentPermission {
  value: string;
  label: string;
  description: string;
  checked: boolean;
  /**
   * Essential permissions are pre-checked and locked in the consent UI —
   * the agent cannot meaningfully operate without them. Server marks
   * memax:read as essential for any recall-capable agent.
   */
  essential?: boolean;
}

export interface OAuthConsentRequest {
  session_id: string;
  csrf_token: string;
  client_name: string;
  agent_name: string;
  resource: string;
  submit_url: string;
  expires_at: string;
  hubs: OAuthConsentHub[];
  permissions: OAuthConsentPermission[];
  not_requested: string[];
}

// --- Hubs ---

export type HubRole = "owner" | "admin" | "contributor" | "viewer";
export type HubType = "personal" | "team";
export type HubHeaderState =
  | "first_time"
  | "review_needed"
  | "dream_deltas"
  | "inbox_overflow"
  | "return_after_absence"
  | "team_activity"
  | "clean";
export type HubTimeBucket =
  | "deep-night"
  | "morning"
  | "noon"
  | "afternoon"
  | "evening"
  | "late-night";
export type HubGreetingKey =
  | "firstTimeA"
  | "firstTimeB"
  | "reviewNeededA"
  | "reviewNeededB"
  | "teamReviewA"
  | "teamReviewB"
  | "dreamDeltasA"
  | "dreamDeltasB"
  | "inboxOverflowA"
  | "inboxOverflowB"
  | "returnAfterAbsenceA"
  | "returnAfterAbsenceB"
  | "morningDreamA"
  | "morningDreamB"
  | "teamMorningA"
  | "teamMorningB"
  | "morningCleanA"
  | "morningCleanB"
  | "afternoonCleanA"
  | "afternoonCleanB"
  | "deepNightCleanA"
  | "deepNightCleanB"
  | "timeEveningA"
  | "timeEveningB"
  | "timeNoonA"
  | "timeNoonB"
  | "timeAfternoonA"
  | "timeAfternoonB";

export type ContributorDeletePolicy = "none" | "own" | "any";
export type HubAccent =
  | "violet"
  | "blue"
  | "green"
  | "amber"
  | "rose"
  | "slate";

/**
 * HubSettings is the per-hub dream-phase configuration. After the
 * per-hub intelligence release this is the single source of truth
 * for dream behavior on a given hub — the old account-level
 * fallback is gone (migration 018 moved personal-hub values into
 * `hubs.settings` and stripped them from `user_preferences`).
 *
 * The keys mirror the server-side allow-list
 * (model.HubSettingsAllowedKeys). Keep the two in lockstep — a key
 * typed here but not allow-listed server-side would cause PATCH
 * /v1/hubs/{id} to 400.
 */
export interface HubSettings {
  dreams_enabled?: boolean;
  dreams_merge_enabled?: boolean;
  dreams_archive_enabled?: boolean;
  dreams_organize_enabled?: boolean;
  dreams_restructure_enabled?: boolean;
}

/**
 * HubSettingsInput is the partial-patch payload. A key set to
 * `null` instructs the server to delete that override (the engine
 * then falls back to DefaultSettings on the next cycle).
 */
export type HubSettingsInput = {
  [K in keyof HubSettings]?: HubSettings[K] | null;
};

export type HubHeaderAuroraMode = "none" | "signature" | "time";

export interface Hub {
  id: string;
  name: string;
  icon?: string;
  accent?: HubAccent;
  slug: string;
  hub_type: HubType;
  owner_id: string;
  allow_contributor_topics?: boolean;
  allow_contributor_dreams?: boolean;
  contributor_delete_policy?: ContributorDeletePolicy;
  /**
   * Team hubs only. Omitted / empty means "inherit default" ("signature").
   * Personal hubs read their aurora mode from user settings
   * (`Settings.hub_header_aurora_mode`) instead.
   */
  header_aurora_mode?: HubHeaderAuroraMode;
  settings?: HubSettings;
}

export interface HubWithRole {
  hub: Hub;
  role: HubRole;
  memory_count: number;
}

export interface HubMember {
  user_id: string;
  user_name: string;
  user_email: string;
  user_avatar_url: string;
  role: HubRole;
  joined_at: string;
}

export interface HubInvite {
  id: string;
  hub_id: string;
  token: string;
  invited_by: string;
  role: HubRole;
  invite_url?: string;
  expires_at: string;
  accepted_by?: string | null;
  /**
   * When present, the invite is addressed to a specific existing
   * user. The server writes this field when `createInvite` is
   * called with an `invitee` that resolves to an account. Link-only
   * invites (the legacy copy-link flow) leave this null / omitted.
   */
  invitee_user_id?: string | null;
  /** The email the admin typed. Set for email invites alongside invitee_user_id (if resolved). */
  invitee_email?: string | null;
  /** When the email job was last submitted to the queue. Enqueue time, not confirmed delivery. */
  email_enqueued_at?: string | null;
  created_at: string;
}

/**
 * HubInviteeInput is the addressed-recipient shape accepted by
 * `hubs.createInvite`. Exactly one of `user_id` or `email` should be
 * set; when both are present the server prefers `user_id`.
 *
 * Unknown emails / user ids do not fail the request — they fall
 * through to a link-only invite so the admin can forward the URL
 * manually.
 */
export interface HubInviteeInput {
  user_id?: string;
  email?: string;
}

export interface HubOwnershipTransfer {
  id: string;
  hub_id: string;
  initiated_by: string;
  target_user_id: string;
  accepted_at?: string | null;
  cancelled_at?: string | null;
  expires_at: string;
  created_at: string;
  target_user_name?: string;
  target_user_email?: string;
}

export interface HubUpdateParams {
  name?: string;
  icon?: string;
  accent?: HubAccent;
  // slug intentionally omitted — immutable after creation
  allow_contributor_topics?: boolean;
  allow_contributor_dreams?: boolean;
  contributor_delete_policy?: ContributorDeletePolicy;
  /**
   * Team hubs only. Server rejects this field on personal hubs (use
   * `settings.hub_header_aurora_mode` instead). Empty string clears the
   * per-hub override and restores the default ("signature").
   */
  header_aurora_mode?: HubHeaderAuroraMode | "";
  /**
   * Partial merge into hub.settings. See HubSettingsInput — sending
   * `null` for a key deletes that override.
   */
  settings?: HubSettingsInput;
}

export interface HubDetailResult {
  hub: Hub;
  /**
   * Derived public flag — true when the hub's subscription is
   * cancelled / past_due, or the over-limit grace window has
   * expired. The full subscription row stays admin-only; this
   * boolean is enough for the web client to gate actions like
   * manual dream trigger before the server returns 409.
   *
   * Optional because older servers that predate commit 3.8 omit
   * it. Treat undefined as "not frozen".
   */
  is_frozen?: boolean;
  members: HubMember[];
  role: HubRole;
  pending_transfer?: HubOwnershipTransfer | null;
}

export interface HubSummaryStats {
  memories: number;
  topics: number;
  inbox: number;
  pending_review: number;
  members?: number;
  dream_topics?: number;
  team_activity?: number;
}

export interface HubSummaryHeader {
  state: HubHeaderState;
  greeting_key: HubGreetingKey;
  greeting_params: Record<string, string>;
  time_bucket: HubTimeBucket;
}

export interface HubSummaryMember {
  user_id: string;
  user_name: string;
  user_avatar_url?: string;
}

export interface HubSummary {
  hub: Hub;
  role: HubRole;
  stats: HubSummaryStats;
  members_preview?: HubSummaryMember[];
  header: HubSummaryHeader;
}

export interface InviteDetails {
  invite: HubInvite;
  hub: Hub;
  member_count: number;
  invited_by: string;
}

export interface InviteAcceptResult {
  hub: Hub;
  role: HubRole;
}

// --- Settings ---

export interface DevFlagsSettings {
  mockDreams: boolean;
  mockDreaming: boolean;
  mockEmptyInbox: boolean;
  mockProUser: boolean;
  debuggerEnabled: boolean;
  skipRerank: boolean;
}

export interface Settings {
  dreams_enabled: boolean;
  dreams_merge_enabled: boolean;
  dreams_archive_enabled: boolean;
  dreams_organize_enabled?: boolean;
  /** Controls phase 5 of the dream cycle — grouping related
   *  root-level topics into hierarchies. Default true. */
  dreams_restructure_enabled?: boolean;
  dreams_excluded_kinds: MemoryKind[];
  dreams_similarity_threshold: number;
  dreams_staleness_days: number;
  hub_header_aurora_mode?: "none" | "signature" | "time";
  dev_flags?: DevFlagsSettings;
  notifications_enabled: boolean;
  theme: string;
  [key: string]: unknown;
}

/**
 * SettingsUpdateInput is the patch payload for PATCH /v1/settings.
 *
 * The five dream-phase toggles (dreams_enabled,
 * dreams_merge_enabled, dreams_archive_enabled,
 * dreams_organize_enabled, dreams_restructure_enabled) moved to
 * per-hub settings in the per-hub intelligence release —
 * the server now rejects them here with a targeted 400
 * (code=moved_to_hub_settings). Send them to PATCH /v1/hubs/{id}
 * with a `settings` body via {@link HubsResource.update} instead.
 *
 * The three non-phase tuning keys (dreams_excluded_kinds,
 * dreams_similarity_threshold, dreams_staleness_days) stay account-
 * scoped.
 */
export interface SettingsUpdateInput {
  dreams_excluded_kinds?: MemoryKind[];
  dreams_similarity_threshold?: number;
  dreams_staleness_days?: number;
  hub_header_aurora_mode?: "none" | "signature" | "time";
  dev_flags?: Partial<DevFlagsSettings>;
  notifications_enabled?: boolean;
  theme?: string;
}

// --- Topics ---

export interface Topic {
  id: string;
  owner_id: string;
  hub_id?: string;
  parent_id: string | null;
  name: string;
  description?: string;
  icon: string;
  position: number;
  pinned: boolean;
  user_modified: boolean;
  created_at: string;
  updated_at: string;
  activity_summary?: TopicActivitySummary;
  /** Server-resolved dream-activity aggregate scoped to viewer's last
   *  visit of this topic. Clears when viewer calls topics.markVisit. */
  lifecycle?: TopicLifecycle;
}

export interface TopicLifecycle {
  delta_since_visit: TopicDeltaSummary | null;
}

/**
 * TopicDeltaSummary — aggregate of dream actions touching this topic
 * since the viewer's last visit. Counts are non-overlapping:
 *   - added: new inbound from unassigned (from_topic = null, to_topic = this)
 *   - reorganized: inter-topic moves involving this topic
 */
export interface TopicDeltaSummary {
  since_visit_at: string;
  run_ids: string[];
  added: number;
  reorganized: number;
}

export interface TopicActivityContributor {
  user_id: string;
  user_name: string;
  user_avatar_url?: string;
}

export interface TopicActivitySummary {
  window_days: number;
  memory_count: number;
  contributor_count: number;
  contributors_preview?: TopicActivityContributor[];
}

export interface TopicTree extends Topic {
  memory_count: number;
  total_memory_count: number;
  children: TopicTree[];
  kind_dots?: MemoryKind[];
}

export interface TopicListResponse {
  topics: TopicTree[];
  unassigned_count: number;
}

export interface TopicCreateParams {
  name: string;
  description?: string;
  icon?: string;
  parent_id?: string;
  hub_id?: string;
}

export interface TopicUpdateParams {
  name?: string;
  description?: string;
  icon?: string;
  position?: number;
  parent_id?: string | null;
  pinned?: boolean;
}

/**
 * TopicUpdateErrorCode enumerates the domain-specific error codes the server
 * can return from PATCH /v1/topics/{id} when a `parent_id` change is
 * rejected. The SDK surfaces these as MemaxError.code on a rejected update
 * so the caller can map them to user-facing toasts.
 *
 * Note on `invalid_parent`: the server collapses two cases under this code —
 * (a) the parent id does not exist, and (b) the parent exists but lives in a
 * different hub. The client cannot tell them apart without a cross-hub read,
 * and collapsing avoids leaking hub existence across tenants. If a future
 * admin-tooling use case needs the split, introduce a separate
 * `cross_hub_parent` code at that point.
 */
export type TopicUpdateErrorCode =
  | "invalid_parent"
  | "cycle_detected"
  | "max_depth_subtree"
  | "max_depth"
  | "duplicate_name";

export interface TopicReorderOperation {
  topic_id: string;
  position: number;
  parent_id?: string | null;
}

export interface TopicMemoriesResponse {
  memories: Memory[];
  next_cursor: string;
  has_more: boolean;
}

// --- Dreams ---

export type DreamRunMode = "maintenance" | "bootstrap";
export type DreamRunStatus =
  | "running"
  | "completed"
  | "partial_failed"
  | "failed"
  | "skipped";
export type DreamActionType =
  | "merge"
  | "contradiction"
  | "archive"
  | "organize"
  | "restructure";

/**
 * ReviewTopicRef is the shared topic-pointer shape used by enriched
 * topic_merge and topic_restructure notification payloads. Mirrors
 * model.ReviewTopicRef on the server. The "Review" prefix is a
 * historical holdover from the legacy /v1/reviews surface retired in
 * Phase 6; the type is still reused verbatim by the notification
 * payloads for review_topic_merge and review_topic_restructure.
 */
export interface ReviewTopicRef {
  id: string;
  name: string;
  memory_count?: number;
}

/**
 * Payload shape for notifications with kind = "review_topic_merge".
 * The merge resolve action collapses every entry in source_topics into
 * target_topic via the server's MergeTopics primitive. source_topics
 * never includes target_topic — the dream restructure writer filters
 * it out at write time.
 */
export interface ReviewTopicMergePayload {
  target_topic: ReviewTopicRef;
  source_topics: ReviewTopicRef[];
  reason: string;
}

/**
 * Payload shape for notifications with kind = "review_topic_restructure".
 * The apply resolve action reparents child_topic under parent_topic.
 */
export interface ReviewTopicRestructurePayload {
  parent_topic: ReviewTopicRef;
  child_topic: ReviewTopicRef;
  reason: string;
}

export interface DreamRun {
  id: string;
  owner_id?: string;
  hub_id?: string;
  mode?: DreamRunMode;
  status: DreamRunStatus;
  started_at: string;
  finished_at?: string;
  memories_scanned: number;
  duplicates_merged: number;
  contradictions_found: number;
  memories_archived: number;
  memories_organized: number;
  topics_restructured?: number;
  phase_metrics?: Record<string, DreamPhaseMetrics>;
  phase_budgets?: Record<string, DreamPhaseBudget>;
  report: string;
}

export interface DreamPhaseMetrics {
  candidates?: number;
  attempted?: number;
  processed?: number;
  actions?: number;
  skipped?: number;
  batches?: number;
  completed_batches?: number;
  timed_out_batches?: number;
  llm_calls?: number;
  llm_errors?: number;
  llm_timeouts?: number;
  duration_ms?: number;
}

export interface DreamPhaseBudget {
  candidate_limit?: number;
  batch_size?: number;
  preview_char_budget?: number;
  topic_context_limit?: number;
  max_llm_calls?: number;
  timeout_ms?: number;
}

export interface DreamAction {
  id: string;
  run_id: string;
  action_type: DreamActionType;
  source_memory_ids: string[];
  result_memory_id?: string;
  reason: string;
  similarity: number;
  created_at: string;
}

export interface DreamLatestRunSummary {
  merged: number;
  contradictions_found: number;
  archived: number;
  organized: number;
  restructured: number;
}

export interface DreamPendingReviewSummary {
  contradictions: number;
  topic_merges: number;
  topic_restructures: number;
  total: number;
}

export interface DreamIntelligenceSummary {
  latest_run?: DreamLatestRunSummary;
  pending_review: DreamPendingReviewSummary;
}

export interface DreamReport {
  has_run: boolean;
  message?: string;
  run?: DreamRun;
  actions?: DreamAction[];
  intelligence: DreamIntelligenceSummary;
}

/**
 * DreamRunListOptions controls `dreams.list`. All fields optional.
 *
 * Passing `hubId` narrows to a single hub (caller must be a
 * member; server 403s otherwise). Omitting it spans every hub the
 * caller participates in — the default for the Dream history view.
 *
 * `cursor` is the opaque `<rfc3339nano>|<uuid>` marker returned by
 * a prior response as `next_cursor`. Callers should treat it as a
 * black box — the format is shared with the admin dream-runs
 * endpoint and may evolve.
 */
export interface DreamRunListOptions {
  hubId?: string;
  limit?: number;
  cursor?: string;
}

/**
 * DreamRunListResponse is the envelope for `dreams.list`. Keyset
 * pagination: `next_cursor` is present only when more rows remain.
 */
export interface DreamRunListResponse {
  runs: DreamRun[];
  next_cursor?: string;
}

/**
 * DreamUsageOptions controls `dreams.usage`. Pass `hubId` to query
 * the per-hub Lucid pool (team hubs); omit it to query the caller's
 * personal dream quota.
 *
 * For credentials scoped to a single hub (allowlist OAuth grants),
 * the server overrides the query param and always returns the
 * credential's scoped hub — querying any other hub returns the
 * scoped one. Defense against probing other hubs' state.
 */
export interface DreamUsageOptions {
  hubId?: string;
}

/**
 * DreamUsage is the read-only quota snapshot returned by
 * `dreams.usage`. Reflects the same scope/tier/limit shape the
 * trigger gate uses — clients can show "X of Y dreams used" and
 * gate the "Dream now" button on `allowed`.
 *
 * - `scope` is `"personal"` (user-scoped quota) or `"hub"` (team
 *   hub's fixed pool).
 * - `tier` is `"basic"` (Free) or `"lucid"` (paid + team hubs).
 *   Only one tier reported per scope — the active one.
 * - `mode` reflects the rollout phase: `"soft"` allows triggers
 *   even at cap (telemetry only); `"hard"` blocks at cap.
 * - `limit` is `-1` for unlimited, `0` for tier disabled, `>0` for
 *   a finite cap. `remaining` is omitted when unlimited; explicit
 *   `0` when exhausted, so clients can distinguish "exhausted"
 *   from "unlimited."
 * - `allowed` reflects whether a manual trigger right now would
 *   succeed. UI clients gate the "Dream now" button on this.
 */
export interface DreamUsage {
  scope: "personal" | "hub";
  hub_id?: string; // populated only when scope = "hub"
  tier: "basic" | "lucid";
  mode: "soft" | "hard";
  limit: number;
  used: number;
  remaining?: number; // omitted when limit === -1 (unlimited)
  allowed: boolean;
  period_start: string; // RFC3339
  period_end: string; // RFC3339
  quota_source: string; // plan ID
}

// --- Notifications ---
//
// Wire types for the /v1/notifications surface. These mirror the public
// server API contract.

/**
 * NotificationAudience is the routing enum for notifications:
 *
 * - `hub`          — reserved for future hub-wide announcements
 *                    (no producer in Phase 3b)
 * - `hub_member`   — scoped to members of a specific hub; the
 *                    default for review notifications
 * - `user`         — addressed to a specific user regardless of
 *                    hub membership; used for dream receipts, hub
 *                    invites, system notices
 */
export type NotificationAudience = "hub" | "hub_member" | "user";

/**
 * NotificationStatus is the lifecycle state machine. Read state
 * lives in `seen_at`, not here: a row can be `status="pending"`
 * with `seen_at` set, meaning "surfaced but not yet acted on".
 *
 * Transitions:
 * - pending → resolved   : every /resolve action (including dismiss)
 * - pending → dismissed  : /dismiss on a receipt row only
 * - pending → expired    : nightly sweep on receipts past expires_at
 */
export type NotificationStatus =
  | "pending"
  | "resolved"
  | "dismissed"
  | "expired";

/**
 * NotificationResolution records which /resolve action actually ran
 * on a decision row. Non-empty only when status=resolved. Mirrors
 * the server's notification_resolution Postgres enum.
 */
export type NotificationResolution =
  | "kept_a"
  | "kept_b"
  | "kept_both"
  | "merged"
  | "kept_separate"
  | "applied"
  | "kept"
  | "dismissed"
  | "accepted"
  | "declined";

/**
 * NotificationKind is the open set of kinds the inbox framework
 * will grow over Phase 3–5. Typed as a union rather than a closed
 * enum so future scaffold kinds don't require a client bump to
 * decode a response.
 */
export type NotificationKind =
  | "review_contradiction"
  | "review_topic_merge"
  | "review_topic_restructure"
  | "review_stale"
  | "review_low_confidence"
  | "dream_run_completed"
  | "hub_invite"
  | "hub_invite_accepted"
  | "hub_invite_declined"
  | "hub_invite_declined_by_you"
  | "hub_member_joined"
  | "hub_ownership_transfer"
  | "hub_ownership_transferred"
  | "hub_over_limit"
  | "hub_frozen"
  | "hub_restored"
  | "system_notice"
  | "gift_invite_link"
  // Open-ended: server may add new kinds before the SDK is regenerated.
  | (string & {});

export type NeedsActionNotificationKind =
  | "review_contradiction"
  | "review_topic_merge"
  | "review_topic_restructure"
  | "review_stale"
  | "review_low_confidence"
  | "hub_invite"
  | "hub_ownership_transfer";

export type UpdateNotificationKind =
  | "dream_run_completed"
  | "hub_member_joined"
  | "hub_invite_accepted"
  | "hub_invite_declined"
  | "hub_invite_declined_by_you"
  | "hub_ownership_transferred"
  | "hub_over_limit"
  | "hub_frozen"
  | "hub_restored"
  | "system_notice"
  | "gift_invite_link";

/**
 * Decision notification kinds that never fold into the Updates feed.
 * Keep this list in sync with the server's supported notification kind
 * bucket and resolution allow-list.
 */
export const NEEDS_ACTION_KINDS: readonly NeedsActionNotificationKind[] = [
  "review_contradiction",
  "review_topic_merge",
  "review_topic_restructure",
  "review_stale",
  "review_low_confidence",
  "hub_invite",
  "hub_ownership_transfer",
] as const;

/**
 * Receipt / announcement notification kinds. These can be bulk-mutated
 * via /v1/notifications/seen and /v1/notifications/dismiss.
 */
export const UPDATES_KINDS: readonly UpdateNotificationKind[] = [
  "dream_run_completed",
  "hub_member_joined",
  "hub_invite_accepted",
  "hub_invite_declined",
  "hub_invite_declined_by_you",
  "hub_ownership_transferred",
  "hub_over_limit",
  "hub_frozen",
  "hub_restored",
  "system_notice",
  "gift_invite_link",
] as const;

/**
 * Runtime list of every notification kind the current SDK knows about.
 * The wire type remains open-ended so newer servers can add kinds before
 * clients update.
 */
export const NOTIFICATION_KINDS: readonly NotificationKind[] = [
  ...NEEDS_ACTION_KINDS,
  ...UPDATES_KINDS,
];

const NEEDS_ACTION_KIND_SET: ReadonlySet<string> = new Set<string>(
  NEEDS_ACTION_KINDS,
);

export function isNeedsActionKind(kind: string): boolean {
  return NEEDS_ACTION_KIND_SET.has(kind);
}

/**
 * Notification is the wire shape for a single row returned by
 * /v1/notifications. Payload shape varies by `kind` — consumers
 * do a type switch on `kind` before decoding `payload`.
 */
export interface Notification {
  id: string;
  audience: NotificationAudience;
  /** Required when audience in (hub, hub_member). */
  hub_id?: string;
  /** Required when audience = user. */
  recipient_user_id?: string;
  /** Optional narrowing for hub_member audience. */
  hub_member_role?: string;
  kind: NotificationKind;
  status: NotificationStatus;
  /** Set only on resolved decision rows. */
  resolution?: NotificationResolution;
  priority: number;
  source_kind: string;
  source_id?: string;
  /**
   * The originating dream_runs.id for notifications produced by a
   * dream cycle (dream_run_completed, review_contradiction,
   * review_topic_merge, review_topic_restructure). Absent on
   * non-dream notifications (hub_invite, hub_frozen, ownership
   * transfers, etc.). Admin tools can filter by this field to
   * surface every row a specific run produced.
   */
  dream_run_id?: string;
  /**
   * Per-kind payload. Shape varies:
   *   - review_contradiction    → ReviewContradictionPayload
   *   - review_topic_merge      → ReviewTopicMergePayload
   *   - review_topic_restructure → ReviewTopicRestructurePayload
   *   - dream_run_completed     → DreamRunCompletedPayload
   *   - hub_invite              → HubInvitePayload
   *   - other kinds             → opaque
   */
  payload?: Record<string, unknown>;
  created_at: string;
  expires_at?: string;
  resolved_at?: string;
  seen_at?: string;
}

/**
 * NotificationKindCount is the per-kind slice of the summary shape.
 * Both fields are always present, even when zero, per plan §4.4.
 */
export interface NotificationKindCount {
  pending: number;
  unseen: number;
}

/**
 * NotificationSummary is the response shape for
 * GET /v1/notifications/summary. Plan §4.4 canonical shape. The
 * badge dot reads `needs_action_pending`; the Updates header reads
 * `updates_pending`; the unseen emphasis reads `updates_unseen`;
 * the settings > dreams contradictions counter reads
 * `by_kind.review_contradiction.pending`. Every supported kind is
 * pre-populated with zero counts so consumers can iterate
 * `by_kind[kind]` without null-checking.
 */
export interface NotificationSummary {
  needs_action_pending: number;
  updates_pending: number;
  updates_unseen: number;
  by_kind: Record<string, NotificationKindCount>;
}

/**
 * NotificationListQuery is the query surface for GET /v1/notifications.
 * All fields optional; defaults match the server behavior:
 *   - status defaults to "pending"
 *   - limit defaults to 50, max 500
 */
export interface NotificationListQuery {
  hub?: string;
  status?: NotificationStatus;
  /** Repeatable. Narrows to one or more kinds. */
  kind?: NotificationKind[];
  /** Repeatable. Only meaningful with status=resolved. */
  resolution?: NotificationResolution[];
  unseenOnly?: boolean;
  /** RFC3339 lower bound on created_at. */
  since?: string;
  /** 1..500; default 50. */
  limit?: number;
  /** Opaque RFC3339 cursor from a previous list response. */
  cursor?: string;
}

/**
 * NotificationListResponse is the paginated list response shape.
 */
export interface NotificationListResponse {
  notifications: Notification[];
  next_cursor?: string;
  has_more: boolean;
}

/**
 * NotificationBulkQuery is the body shape for the bulk seen/dismiss
 * endpoints. Decision kinds are refused with
 * `400 bulk_not_allowed_for_decision_kind`.
 */
export interface NotificationBulkQuery {
  hub?: string;
  kinds: NotificationKind[];
  /** RFC3339 lower bound on created_at. */
  since?: string;
}

export interface NotificationBulkResult {
  affected: number;
}

/**
 * NotificationResolveAction is the closed set of actions the
 * /v1/notifications/{id}/resolve endpoint accepts. Kept in lockstep
 * with the per-kind allow-list in the server notifications handler.
 */
export type NotificationResolveAction =
  // contradiction
  | "keep_a"
  | "keep_b"
  | "keep_both"
  // topic_merge
  | "merge"
  | "keep_separate"
  // topic_restructure
  | "apply"
  | "keep"
  // hub_invite
  | "accept"
  | "decline"
  // shared by every decision kind except hub_invite
  | "dismiss";

export interface NotificationResolveResult {
  status: string;
  action: string;
  resolution: string;
}

// The Review*-prefixed topic payload types above
// (ReviewTopicRef / ReviewTopicMergePayload / ReviewTopicRestructurePayload)
// are the canonical payload shapes for the review_topic_merge and
// review_topic_restructure notification kinds. The "Review" prefix is
// a historical holdover from the retired /v1/reviews surface.

/**
 * ReviewContradictionPayload is the notification payload shape for
 * kind = "review_contradiction". Carries lightweight memory refs so
 * the inbox row can render without a /v1/memories round-trip.
 */
export interface ReviewContradictionPayload {
  memory_a: ReviewMemoryRef;
  memory_b: ReviewMemoryRef;
  similarity: number;
  reason: string;
}

/**
 * ReviewMemoryRef is the lightweight memory pointer used by
 * review notification payloads.
 */
export interface ReviewMemoryRef {
  id: string;
  title: string;
}

/**
 * DreamRunCompletedPayload is the payload for kind =
 * "dream_run_completed". Produced at the end of a successful dream
 * run, including clean runs with zero counts.
 *
 * memories_scanned is embedded here (not read from the active hub's
 * dream report) because dream_run_completed is a user-audience
 * notification that can render while the user is browsing an
 * unrelated hub. Clients MUST read the scan count from the payload,
 * never from the active hub's dreamReport — see the Phase 4 bar-fix
 * commit for the original cross-hub leak bug.
 */
export interface DreamRunCompletedPayload {
  run_id: string;
  mode: string;
  /**
   * Terminal DreamRunStatus ("completed" or "partial_failed"). Absent
   * on historical rows written before the field existed — consumers
   * should treat missing as "completed".
   */
  status?: DreamRunStatus;
  counts: DreamRunCounts;
  memories_scanned: number;
  finished_at: string;
  report?: string;
  touched_topic_ids?: string[];
}

/**
 * DreamRunCounts is the per-phase outcome counts embedded in a
 * dream_run_completed payload.
 */
export interface DreamRunCounts {
  merged: number;
  archived: number;
  organized: number;
  contradictions: number;
  restructures: number;
}

/**
 * HubInvitePayload is the payload for kind = "hub_invite".
 */
export interface HubInvitePayload {
  hub: HubInviteHubRef;
  inviter: HubInviteInviterRef;
  role: string;
  expires_at: string;
}

export interface HubInviteHubRef {
  id: string;
  name: string;
  icon?: string;
  accent?: string;
}

export interface HubInviteInviterRef {
  id: string;
  display?: string;
  avatar_url?: string;
}

/**
 * HubMemberJoinedPayload is the payload for kind = "hub_member_joined".
 * Fired to the hub owner when a user accepts an invite.
 */
export interface HubMemberJoinedPayload {
  hub: HubInviteHubRef;
  member: HubMemberRef;
  role: string;
}

export interface HubMemberRef {
  id: string;
  display?: string;
  avatar_url?: string;
}

/**
 * HubInviteAcceptedPayload is the payload for kind =
 * "hub_invite_accepted" — the invitee's self-receipt after a
 * successful accept. Renders as "You joined {hub}". Fan-out target
 * is the invitee themself, not the hub owner (the owner's receipt
 * is `hub_member_joined`). Both receipts exist so each side of the
 * accept reads their own view of the same event.
 */
export interface HubInviteAcceptedPayload {
  hub: HubInviteHubRef;
  role: string;
}

/**
 * HubInviteDeclinedPayload is the payload for kind =
 * "hub_invite_declined" — fan-out to the original inviter (the
 * admin who created the invite, via `invite.invited_by`), telling
 * them their invitee declined. Renders as "{invitee} declined
 * your invite to {hub}".
 */
export interface HubInviteDeclinedPayload {
  hub: HubInviteHubRef;
  invitee: HubMemberRef;
}

/**
 * HubInviteDeclinedByYouPayload is the payload for kind =
 * "hub_invite_declined_by_you" — the invitee's self-receipt after
 * a decline. Renders as "You declined {hub}". Lives in the Updates
 * bucket so the invitee has a durable self-oriented record of the
 * decline.
 */
export interface HubInviteDeclinedByYouPayload {
  hub: HubInviteHubRef;
}

/**
 * SystemNoticePayload is the payload for kind = "system_notice".
 * Used for product announcements and billing notices. Phase 5 ships
 * the renderer + types only; producers land alongside the individual
 * features that emit them.
 */
export interface SystemNoticePayload {
  title: string;
  body: string;
  link?: string;
  link_text?: string;
}

/**
 * GiftInviteLinkPayload is the payload for kind = "gift_invite_link".
 * Phase 5 ships the renderer only; the gift / referral producer lands
 * alongside the gift feature.
 */
export interface GiftInviteLinkPayload {
  sender: HubInviteInviterRef;
  hub?: HubInviteHubRef;
  token: string;
  url?: string;
  expires_at: string;
}

/**
 * HubOwnershipTransferPayload is the payload for kind =
 * "hub_ownership_transfer". Addressed to the transfer target so the
 * inbox shows a decision row with accept / decline actions. The
 * source_id on the notification row is the transfer id, so there is
 * exactly one pending notification per transfer.
 */
export interface HubOwnershipTransferPayload {
  hub: HubInviteHubRef;
  initiator: HubMemberRef;
  role?: string;
  expires_at: string;
}

/**
 * HubOwnershipTransferredPayload is the payload for kind =
 * "hub_ownership_transferred" — a receipt fired to both sides of an
 * accepted ownership handoff. Clients should render recipient-aware
 * copy from the current user id versus `new_owner.id`.
 */
export interface HubOwnershipTransferredPayload {
  hub: HubInviteHubRef;
  new_owner: HubMemberRef;
  old_owner: HubMemberRef;
}

// --- Connected Agents ---

export interface ConnectedAgent {
  id: string;
  owner_id: string;
  agent_name: string;
  display_name: string;
  icon: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface ConnectedAgentWithStats extends ConnectedAgent {
  key_count: number;
  config_count: number;
  memory_count: number;
  needs_reconnect?: boolean;
  last_active_at: string | null;
  last_operation?: string | null;
  last_activity_summary?: string | null;
  last_observed_at?: string | null;
}

export interface ConnectedAgentUpdate {
  displayName?: string;
  icon?: string;
}

/**
 * Skip reasons for `DELETE /v1/auth/api-keys/{id}`.
 *
 * - `not_found`: the key id did not match a row the caller owns.
 *   Unknown id, already revoked, or belongs to another user (the
 *   SQL WHERE clause makes these indistinguishable). Client treats
 *   as idempotent success — the user's target state is reached.
 * - `revoke_failed`: the DELETE returned a store-level error. Server
 *   still has the key; client rolls back the optimistic removal
 *   and surfaces retry copy.
 */
export type ApiKeyRevokeSkipReason = "not_found" | "revoke_failed";

export interface ApiKeyRevokeSkippedItem {
  id: string;
  reason: ApiKeyRevokeSkipReason;
}

/**
 * Structured response from `DELETE /v1/auth/api-keys/{id}`.
 *
 * Always 200 — 4xx/5xx are reserved for full-request failures (bad
 * path, missing auth). Partial-success semantics mirror
 * `memories.batchDelete` and `configs.batchDelete`: `revoked: false`
 * with a non-empty `skipped` array is a normal outcome, and clients
 * branch on the reason to distinguish idempotent success
 * (`not_found`) from rollback + retry (`revoke_failed`).
 */
export interface ApiKeyRevokeResult {
  revoked: boolean;
  skipped: ApiKeyRevokeSkippedItem[];
}

/**
 * Skip reasons for `DELETE /v1/agents/{slug}`.
 *
 * - `not_found`: `GetConnectedAgent` did not resolve for the caller.
 *   Unknown slug or another user's agent. Client treats this as
 *   idempotent success — the user's target state is reached.
 * - `cascade_failed`: the atomic delete tx (revoke keys + tombstone
 *   configs + delete agent row) returned an error. Server still has
 *   the agent; client must roll back the optimistic removal and
 *   surface an inline retry hint.
 */
export type AgentDisconnectSkipReason = "not_found" | "cascade_failed";

export interface AgentDisconnectSkippedItem {
  id: string;
  reason: AgentDisconnectSkipReason;
}

/**
 * Structured response from `DELETE /v1/agents/{slug}`.
 *
 * Always 200 — 4xx/5xx are reserved for full-request failures (bad
 * path, auth). Partial-success semantics: `disconnected: false` with a
 * non-empty `skipped` array is a normal outcome; clients branch on the
 * reason to choose between silent success (`not_found`) and rollback
 * + retry (`cascade_failed`).
 *
 * `keys_revoked` / `configs_tombstoned` are pre-query counts — they
 * describe what the cascade is about to clean up, not the exact exec
 * RowsAffected. This matches the memory-move pattern of reporting the
 * user-visible outcome rather than raw DB stats, and gives the web
 * hook enough data to render "Disconnected claude-code — revoked 3
 * keys, forgot 12 configs" without waiting for a refetch.
 */
export interface DisconnectAgentResult {
  disconnected: boolean;
  keys_revoked: number;
  configs_tombstoned: number;
  skipped: AgentDisconnectSkippedItem[];
}
