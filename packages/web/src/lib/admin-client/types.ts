// Internal, web-only admin API types. Do NOT re-export from the public
// SDK — admin is not a public API surface. Types re-use
// public SDK types (User, Usage, PlanDefinition, etc.) where the shape
// already exists, but every Admin* type lives here.

import type {
  DreamAction,
  DreamRun,
  GiftInviteLinkPayload,
  Notification,
  PlanDefinition,
  PlanScope,
  SystemNoticePayload,
  Usage,
  User,
  UserLimits,
} from "memax-sdk";

// Re-export SDK types that admin code commonly uses so importers only
// need one source. PlanDefinition is the one public type with admin
// ergonomics — it describes the pricing tiers, which the web pricing
// page also reads via a non-admin endpoint. Keep it public-SDK-owned.
export type { PlanDefinition, PlanScope, Usage, User, UserLimits };

// --- Users ---

export interface AdminUserListParams {
  q?: string;
  plan?: string;
  cursor?: string;
  limit?: number;
}

export interface AdminUserListResponse {
  users: User[];
  next_cursor: string;
  total: number;
}

export interface AdminUserDetail {
  user: User;
  plan: PlanDefinition | null;
  effective_plan?: {
    plan_id: string;
    source: string;
    computed_at: string;
  } | null;
  /** Full plan definition for effective_plan.plan_id. Only populated
   *  when it differs from the personal plan; otherwise undefined. */
  effective_plan_def?: PlanDefinition | null;
  override?: { overrides: Record<string, unknown>; reason: string } | null;
  usage?: Usage | null;
  limits?: UserLimits | null;
  /** Limits resolved against the effective plan (max(personal, best_hub)).
   *  Only populated when the effective plan differs from the personal
   *  plan; otherwise undefined (callers should fall back to `limits`). */
  effective_limits?: UserLimits | null;
  hubs?: Array<{
    hub: { id: string; name: string; hub_type: string };
    role: string;
  }>;
  hub_subscriptions?: HubSubscription[];
  memory_count: number;
}

/** Current-period + lifetime quota state for a single user, returned by
 *  GET /v1/admin/users/{id}/usage. Limits follow the plan convention
 *  (-1 = unlimited) so UI should special-case that marker rather than
 *  attempt to render "current / -1". */
export interface AdminUserUsage {
  plan_id: string;
  plan_display: string;
  period: { start: string; end: string };
  monthly: {
    push: { current: number; limit: number };
    recall: { current: number; limit: number };
    ask: { current: number; limit: number };
  };
  lifetime: {
    memory_count: { current: number; limit: number };
    storage_bytes: { current: number; limit: number };
  };
}

// --- Stats + infra status ---

export interface SystemStats {
  total_users: number;
  users_by_plan: Record<string, number>;
  total_memories: number;
  active_today: number;
}

/** Read-only infra + security status. origin_secret_set deliberately
 *  does NOT expose the secret value; it tracks env at request time. */
export interface AdminConfig {
  trusted_proxy: "none" | "cloudflare" | "fly" | "xff";
  origin_secret_set: boolean;
  meter_enabled: boolean;
  rate_limit_enabled: boolean;
  server_time: string;
  /** Short commit SHA the binary was built from. */
  version: string;
  /**
   * Semantic version string — the git tag this deploy produced (prod:
   * exact `vX.Y.Z`; staging: `git describe` output like
   * `v0.1.11-5-gabc…`). Empty string when the binary wasn't built via
   * the CI ldflags chain (dev, stale binaries). Render as the primary
   * Version value with the SHA as a secondary reference.
   */
  semver?: string;
}

// --- Hubs ---

export interface AdminTeamHub {
  id: string;
  name: string;
  slug: string;
  hub_type: string;
  plan: string;
  owner_id: string;
  created_at: string;
  subscription?: HubSubscription | null;
  member_count: number;
}

export interface AdminHubListParams {
  q?: string;
  cursor?: string;
  limit?: number;
}

export interface AdminHubListResponse {
  hubs: AdminTeamHub[];
  next_cursor: string;
  total: number;
}

/** Single hub member as rendered in the admin hub detail page.
 *  `user_name` / `user_email` / `user_avatar_url` are joined from the
 *  users table so the table row can render + cross-link without a
 *  second fetch. */
export interface AdminHubMember {
  hub_id: string;
  user_id: string;
  role: string;
  joined_at: string;
  user_name?: string;
  user_email?: string;
  user_avatar_url?: string;
}

/** Full admin view of a single team hub. Mirrors
 *  model.AdminHubDetail on the server. The member roster is NOT
 *  inlined — fetch it via listHubMembers() so large teams don't
 *  balloon the detail response. member_count is authoritative for
 *  stat tiles. */
export interface AdminHubDetail {
  hub: AdminTeamHub;
  owner?: User | null;
  subscription?: HubSubscription | null;
  plan?: PlanDefinition | null;
  member_count: number;
  memory_count: number;
}

/** Params for the paginated hub-members endpoint. */
export interface AdminHubMemberListParams {
  q?: string;
  cursor?: string;
  limit?: number;
}

export interface AdminHubMemberListResponse {
  members: AdminHubMember[];
  next_cursor: string;
  total: number;
}

export interface HubSubscription {
  id: string;
  hub_id: string;
  plan_id: string;
  seat_count: number;
  provider: string;
  status: string;
  billing_user_id: string;
  current_period_start?: string;
  current_period_end?: string;
  cancelled_at?: string;
  created_at: string;
  updated_at: string;
  /** When the hub's current memory count first exceeded the plan's
   *  memory_limit. null when within limits. Grace window is 7 days
   *  from this timestamp; past that the hub is frozen. */
  over_limit_since?: string | null;
  /** Populated by admin endpoints that denormalize the hub row so the
   *  UI can render "<name> (<slug>)" with a link to /admin/hubs/{id}.
   *  Absent on other code paths. */
  hub_name?: string | null;
  hub_slug?: string | null;
}

// --- Email templates ---

export interface AdminEmailTemplateVariable {
  key: string;
  description: string;
  example: string;
}

export interface AdminEmailTemplateSummary {
  name: string;
  category: string;
  description: string;
  subject: string;
  has_override: boolean;
  /** Whether the latest admin save hasn't been promoted to the
   *  published columns yet — drives the "draft — not published" badge
   *  on list rows. Mirrors `EmailTemplateOverride.HasUnpublishedDraft`
   *  on the server. */
  has_unpublished_draft: boolean;
  updated_at?: string;
  updated_by?: string;
  variable_count: number;
}

export interface AdminEmailTemplatePreview {
  subject: string;
  html: string;
  text: string;
  data: Record<string, string>;
}

export interface AdminEmailTemplateSendResult {
  status: string;
  name: string;
  to: string;
  subject: string;
  queued_at: string;
}

export interface AdminEmailTemplateOverride {
  name: string;
  /** Published content — what real sends use. Only advances on Publish. */
  subject: string;
  html: string;
  text: string;
  /** Draft content — what the editor edits. Promoted to published fields
   *  atomically when the admin calls POST …/publish. */
  draft_subject: string;
  draft_html: string;
  draft_text: string;
  /** null when the admin has saved a draft but never published.
   *  Real sends fall back to the file-based default until this is set. */
  published_at?: string;
  notes: string;
  editor_kind: string;
  editor_state?: Record<string, unknown>;
  updated_by?: string;
  created_at: string;
  updated_at: string;
}

export interface AdminEmailTemplateRevision {
  id: string;
  name: string;
  action: string;
  subject: string;
  html: string;
  text: string;
  notes: string;
  editor_kind: string;
  editor_state?: Record<string, unknown>;
  created_by?: string;
  created_at: string;
}

export interface AdminEmailTemplateDetail {
  name: string;
  category: string;
  description: string;
  variables: AdminEmailTemplateVariable[];
  sample_data: Record<string, string>;
  default_subject: string;
  default_html: string;
  default_text: string;
  /** Draft-aware effective content — drives the editor + preview.
   *  Don't reuse in contexts that reach real recipients (cross-campaign
   *  scaffolds, etc.); use `published_*` fields for those. */
  effective_subject: string;
  effective_html: string;
  effective_text: string;
  effective_editor_kind: string;
  /** Published content — matches what Render() emits for real
   *  transactional sends right now. Safe for cross-context reuse
   *  (scaffold picker etc.) because it is never draft-influenced. */
  published_subject: string;
  published_html: string;
  published_text: string;
  /** False when the admin is still on the file default with no publish
   *  yet AND has an unpublished draft — the published fieldset is not
   *  meaningfully different from defaults; UIs should degrade the
   *  "use as scaffold" affordance accordingly. */
  published_available: boolean;
  override?: AdminEmailTemplateOverride | null;
  revisions: AdminEmailTemplateRevision[];
  preview: AdminEmailTemplatePreview;
}

// --- AI-assist for email copy ---

/** Field kinds the AI-assist endpoint understands. Matches the
 *  server's `allowedFieldKinds` map. */
export type AdminAIAssistFieldKind =
  | "subject"
  | "body_html"
  | "body_text"
  | "footer_html"
  | "footer_text"
  | "description"
  | "product_name";

export type AdminAIAssistIntent =
  | "rewrite"
  | "polish"
  | "shorten"
  | "expand"
  | "translate"
  | "draft";

export type AdminAIAssistTone =
  | "warm"
  | "professional"
  | "playful"
  | "urgent"
  | "minimal"
  | "friendly";

export type AdminAIAssistLocale = "en" | "zh";

export interface AdminAIAssistParams {
  field_kind: AdminAIAssistFieldKind;
  intent: AdminAIAssistIntent;
  tone: AdminAIAssistTone;
  locale: AdminAIAssistLocale;
  current: string;
  /** Optional admin note about the situation (launch, apology,
   *  onboarding) so the model has framing the field alone can't carry. */
  context?: string;
  /** Whether to ground the generation in the admin's own memax
   *  knowledge (recall). Default true on the server when omitted. Set
   *  false for blank-slate copy that ignores founder notes. */
  use_recall?: boolean;
}

export interface AdminAIAssistResult {
  text: string;
  model: string;
  tokens: number;
  /** Number of memax notes the server surfaced as grounding for this
   *  generation. Combine with `grounding_attempted` / `grounding_available`
   *  to distinguish opted-out / empty / unavailable in the UI. */
  grounding_count: number;
  /** True when the server actually ran recall for this request (admin
   *  opted in AND server has recall wired). Drives "No matching notes"
   *  vs. "Grounding unavailable" vs. silence. */
  grounding_attempted: boolean;
  /** True when the server is configured for grounding at all. False
   *  in degraded environments (no embedder / no recall wiring) even
   *  if the admin toggled the checkbox on. */
  grounding_available: boolean;
}

// --- Custom campaign templates ---

/** Admin-authored reusable email campaign body. Picked at authoring
 *  time; COPIED into a campaign's inline content, never referenced at
 *  send time. Editing a template later doesn't retroactively touch
 *  campaigns; deleting a template likewise has no effect on them. */
export interface AdminCampaignTemplate {
  id: string;
  slug: string;
  name: string;
  description: string;
  subject: string;
  body_html: string;
  body_text: string;
  is_archived: boolean;
  created_by?: string | null;
  updated_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface AdminCampaignTemplateListResponse {
  templates: AdminCampaignTemplate[];
}

export interface AdminCampaignTemplateCreateParams {
  slug: string;
  name: string;
  description?: string;
  subject: string;
  body_html: string;
  body_text: string;
}

export interface AdminCampaignTemplateUpdateParams {
  name: string;
  description?: string;
  subject: string;
  body_html: string;
  body_text: string;
}

// --- Email Brand (singleton layout wrapper) ---

export interface AdminEmailBrandSettings {
  logo_url: string;
  logo_alt: string;
  product_name: string;
  // Optional tagline / soft-reassurance slot rendered above the
  // structured compliance footer. NOT the place for sender identity,
  // postal address, support email, or legal links — those have
  // dedicated fields below.
  footer_html: string;
  footer_text: string;
  primary_color: string;
  background_color: string;
  surface_color: string;
  border_color: string;
  muted_color: string;
  body_color: string;
  // Structured compliance footer fields. All optional; rendered only
  // when set. Unsubscribe is injected per-recipient by the campaign
  // worker and is not admin-configurable.
  support_email: string;
  company_name: string;
  company_address: string;
  privacy_url: string;
  terms_url: string;
  updated_at: string;
  updated_by?: string | null;
}

// Partial patch — only send the keys you want to change.
// Pass an empty string explicitly to clear a field.
export type AdminEmailBrandUpdateParams = Partial<
  Omit<AdminEmailBrandSettings, "updated_at" | "updated_by">
>;

// --- Notifications ---

export interface AdminNotificationTargeting {
  mode: "users" | "hub" | "all";
  user_ids?: string[];
  emails?: string[];
  hub_id?: string;
  hub_member_role?: string;
}

export interface AdminSendNotificationParams {
  kind: "system_notice" | "gift_invite_link" | "email_announcement";
  targeting: AdminNotificationTargeting;
  payload: SystemNoticePayload | GiftInviteLinkPayload;
}

export interface AdminSendNotificationResult {
  batch_id: string;
  sent_count: number;
  failed_emails: string[];
}

export interface AdminSentNotificationsParams {
  kind?: string;
  cursor?: string;
  limit?: number;
}

export interface AdminNotificationBatch {
  batch_id: string;
  kind: string;
  payload_preview: string;
  recipient_count: number;
  created_at: string;
}

export interface AdminSentNotificationsResponse {
  batches: AdminNotificationBatch[];
  next_cursor: string;
}

// Plan 18 §7.3 — super-notif funnel response.
export interface AdminSuperNotifParentBucket {
  status: string;
  resolution?: string;
  count: number;
}
export interface AdminSuperNotifItemRow {
  cohort_day: string; // YYYY-MM-DD
  item_id: string;
  recipients: number;
  viewed: number;
  completed: number;
}
export interface AdminSuperNotifFunnel {
  kind: "checklist" | "digest";
  source_kind?: string;
  window_days: number;
  parent: AdminSuperNotifParentBucket[];
  items: AdminSuperNotifItemRow[];
}

// --- Audiences ---

export type AdminAudienceRuleType = "all" | "users" | "hub";

export interface AdminAudienceRule {
  user_ids?: string[];
  emails?: string[];
  hub_id?: string;
  hub_member_role?: string;
}

export interface AdminAudience {
  id: string;
  name: string;
  description: string;
  rule_type: AdminAudienceRuleType;
  rule: AdminAudienceRule;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface AdminAudienceSnapshot {
  audience_id?: string;
  rule_type: AdminAudienceRuleType;
  rule: AdminAudienceRule;
}

export interface AdminAudienceEstimate {
  count: number;
  sample: string[];
}

export interface AdminAudienceCreateParams {
  name: string;
  description?: string;
  rule_type: AdminAudienceRuleType;
  rule: AdminAudienceRule;
}

export type AdminAudienceUpdateParams = AdminAudienceCreateParams;

export interface AdminAudienceEstimateParams {
  audience_id?: string;
  snapshot?: AdminAudienceSnapshot;
}

// --- Campaigns ---

export type AdminCampaignStatus =
  | "draft"
  | "scheduled"
  | "sending"
  | "sent"
  | "cancelled"
  | "failed";

export interface AdminCampaign {
  id: string;
  name: string;
  channel: "inapp" | "email";
  kind: "system_notice" | "gift_invite_link" | "email_announcement";
  status: AdminCampaignStatus;
  audience_id?: string;
  audience_snapshot?: AdminAudienceSnapshot;
  content: unknown;
  scheduled_at?: string;
  send_started_at?: string;
  send_finished_at?: string;
  sent_count: number;
  failed_count: number;
  batch_id?: string;
  error_message?: string;
  created_by: string;
  updated_by?: string;
  cancelled_by?: string;
  created_at: string;
  updated_at: string;
}

export interface AdminCommsAuditEntry {
  id: string;
  resource_type: "campaign" | "audience";
  resource_id: string;
  action: string;
  actor_id?: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface AdminCampaignListParams {
  status?: AdminCampaignStatus;
  cursor?: string;
  limit?: number;
}

export interface AdminCampaignListResponse {
  campaigns: AdminCampaign[];
  next_cursor: string;
}

export interface AdminCampaignCreateParams {
  name: string;
  kind: "system_notice" | "gift_invite_link" | "email_announcement";
  channel?: "inapp" | "email";
  audience_id?: string;
  audience_snapshot?: AdminAudienceSnapshot;
  content: unknown;
}

export interface AdminCampaignDeliveryStats {
  total: number;
  queued: number;
  sent: number;
  delivered: number;
  opened: number;
  bounced: number;
  complained: number;
  failed: number;
  suppressed: number;
}

export interface AdminCampaignUpdateParams {
  name?: string;
  audience_id?: string;
  audience_snapshot?: AdminAudienceSnapshot;
  content?: unknown;
}

export interface AdminCampaignScheduleParams {
  scheduled_at: string;
}

export interface AdminCampaignDetail {
  campaign: AdminCampaign;
  audit: AdminCommsAuditEntry[];
}

// --- Dream runs (admin audit) ---

/**
 * Params for listing a user's dream runs across every hub they
 * participate in (personal + team-hub membership). Keyset
 * pagination by started_at DESC.
 */
export interface AdminDreamRunListParams {
  /** Scope to a single hub. Optional. */
  hubId?: string;
  /** started_at of the last row from the previous page (RFC3339Nano). */
  cursor?: string;
  /** Page size; server default 20, capped at 100. */
  limit?: number;
}

export interface AdminDreamRunListResponse {
  runs: DreamRun[];
  /** started_at of the last row; empty when no more pages. */
  next_cursor?: string;
}

/**
 * One cycle's full audit: the run row, every dream_action the
 * cycle produced, and every notification tagged with this run's
 * dream_run_id. The admin detail view renders these as three
 * timeline sections.
 */
export interface AdminDreamRunDetail {
  run: DreamRun;
  actions: DreamAction[];
  notifications: Notification[];
}

// --- Waitlist orphan repair ---
//
// The 2026-04-14 → 2026-04-20 regression stranded invited users:
// their invite token got dropped during OAuth, so the server
// never consumed the invite and never upgraded the plan. These
// types back the admin surfaces that let operators repair the
// cohort. See packages/server/internal/handler/admin_waitlist_reconcile.go
// for the authoritative server contract.

/**
 * One row in the orphan cohort. invite_token is NOT exposed by the
 * server (json:"-" on the Go side) — raw waitlist tokens grant
 * registration authority and should never leave the DB.
 */
export interface AdminOrphanCandidate {
  user_id: string;
  user_email: string;
  user_created_at: string;
  user_plan_id: string;
  invite_id: string;
  invite_expires_at: string;
  waitlist_id: string;
}

export interface AdminOrphanListParams {
  /** RFC3339 timestamp; users with created_at >= since are scanned.
   * Required by the server to force a narrow window. */
  since: string;
  limit?: number;
}

export interface AdminOrphanListResponse {
  candidates: AdminOrphanCandidate[];
  scanned: number;
  since: string;
  limit: number;
}

/**
 * Per-user orphan check used by the user-detail page to render
 * the reconcile affordance conditionally. When is_orphan is
 * false the candidate field is omitted.
 */
export interface AdminUserOrphanStatus {
  is_orphan: boolean;
  candidate?: AdminOrphanCandidate;
}

export interface AdminReconcileRequest {
  user_ids: string[];
}

/**
 * Per-user outcome of a reconcile batch.
 *
 * action values:
 *   - "reconciled":      consumer ran, postcondition held (plan
 *                        flipped to personal_early_access).
 *   - "skipped":         user was not an orphan (reason
 *                        disambiguates: "not_an_orphan",
 *                        "already_upgraded", "duplicate_in_batch").
 *   - "error":           something failed; reason carries the
 *                        specific code ("user_not_found",
 *                        "plan_upgrade_did_not_apply",
 *                        "post_consume_user_lookup_failed").
 */
export interface AdminReconcileItemResult {
  user_id: string;
  action: "reconciled" | "skipped" | "error";
  reason?: string;
  before_plan?: string;
  after_plan?: string;
  invite_id?: string;
}

export interface AdminReconcileResponse {
  results: AdminReconcileItemResult[];
  reconciled: number;
  skipped: number;
  errored: number;
}
