// Internal admin client. Mirrors the former SDK AdminResource 1:1
// (same method names, same request/response shapes) but lives inside
// packages/web so the admin surface never ships to npm.
//
// Prefer this module over `getMemaxClient().admin.*` in every new
// admin code path. The old SDK surface was removed — see
// docs/engineering/admin-client.md (if added later) or the relevant
// commit history.

import { adminReq } from "./transport";
import type {
  AdminAudience,
  AdminAudienceCreateParams,
  AdminAudienceEstimate,
  AdminAudienceEstimateParams,
  AdminAudienceUpdateParams,
  AdminCampaign,
  AdminCampaignCreateParams,
  AdminCampaignDetail,
  AdminCampaignListParams,
  AdminCampaignListResponse,
  AdminCampaignScheduleParams,
  AdminCampaignUpdateParams,
  AdminCommsAuditEntry,
  AdminConfig,
  AdminDreamRunDetail,
  AdminDreamRunListParams,
  AdminDreamRunListResponse,
  AdminEmailBrandSettings,
  AdminEmailBrandUpdateParams,
  AdminEmailTemplateDetail,
  AdminEmailTemplatePreview,
  AdminEmailTemplateSendResult,
  AdminEmailTemplateSummary,
  AdminHubDetail,
  AdminHubListParams,
  AdminHubListResponse,
  AdminHubMemberListParams,
  AdminHubMemberListResponse,
  AdminOrphanListParams,
  AdminOrphanListResponse,
  AdminReconcileRequest,
  AdminReconcileResponse,
  AdminUserOrphanStatus,
  AdminSendNotificationParams,
  AdminSendNotificationResult,
  AdminSentNotificationsParams,
  AdminSentNotificationsResponse,
  AdminSuperNotifFunnel,
  AdminUserDetail,
  AdminUserListParams,
  AdminUserListResponse,
  AdminUserUsage,
  HubSubscription,
  PlanDefinition,
  PlanScope,
  SystemStats,
} from "./types";

// --- Users ---

export function listUsers(
  params: AdminUserListParams = {},
): Promise<AdminUserListResponse> {
  const qs = new URLSearchParams();
  if (params.q) qs.set("q", params.q);
  if (params.plan) qs.set("plan", params.plan);
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq("GET", `/v1/admin/users${query ? `?${query}` : ""}`);
}

export function getUser(userId: string): Promise<AdminUserDetail> {
  return adminReq("GET", `/v1/admin/users/${userId}`);
}

/** Current billing-period counters + lifetime totals for this user. */
export function getUserUsage(userId: string): Promise<AdminUserUsage> {
  return adminReq("GET", `/v1/admin/users/${userId}/usage`);
}

export function setUserPlan(
  userId: string,
  planId: string,
): Promise<{ status: string; plan_id: string }> {
  return adminReq("POST", `/v1/admin/users/${userId}/set-plan`, {
    plan_id: planId,
  });
}

export function setUserOverrides(
  userId: string,
  overrides: Record<string, unknown>,
  reason: string,
): Promise<{ status: string }> {
  return adminReq("PUT", `/v1/admin/users/${userId}/overrides`, {
    overrides,
    reason,
  });
}

export function deleteUserOverrides(
  userId: string,
): Promise<{ status: string }> {
  return adminReq("DELETE", `/v1/admin/users/${userId}/overrides`);
}

// --- Plans ---

export function listPlans(): Promise<PlanDefinition[]> {
  return adminReq("GET", "/v1/admin/plans");
}

/** Returns active plans filtered by scope. */
export async function listPlansByScope(
  scope: PlanScope,
): Promise<PlanDefinition[]> {
  const all = await listPlans();
  return all.filter((p) => p.scope === scope);
}

export function updatePlan(
  planId: string,
  patch: Partial<PlanDefinition>,
): Promise<PlanDefinition> {
  return adminReq("PATCH", `/v1/admin/plans/${planId}`, patch);
}

// --- Hubs ---

export function listTeamHubs(
  params: AdminHubListParams = {},
): Promise<AdminHubListResponse> {
  const qs = new URLSearchParams();
  if (params.q) qs.set("q", params.q);
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq("GET", `/v1/admin/hubs${query ? `?${query}` : ""}`);
}

export function getHub(hubId: string): Promise<AdminHubDetail> {
  return adminReq("GET", `/v1/admin/hubs/${hubId}`);
}

export function listHubMembers(
  hubId: string,
  params: AdminHubMemberListParams = {},
): Promise<AdminHubMemberListResponse> {
  const qs = new URLSearchParams();
  if (params.q) qs.set("q", params.q);
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq(
    "GET",
    `/v1/admin/hubs/${hubId}/members${query ? `?${query}` : ""}`,
  );
}

export async function getHubSubscription(
  hubId: string,
): Promise<HubSubscription | null> {
  const result = await adminReq<HubSubscription | undefined>(
    "GET",
    `/v1/admin/hubs/${hubId}/subscription`,
  );
  return result ?? null;
}

export function setHubPlan(
  hubId: string,
  planId: string,
): Promise<{ status: string; hub_id: string; plan_id: string }> {
  return adminReq("POST", `/v1/admin/hubs/${hubId}/set-plan`, {
    plan_id: planId,
  });
}

export function listBillingHubs(userId: string): Promise<HubSubscription[]> {
  return adminReq("GET", `/v1/admin/users/${userId}/billing-hubs`);
}

// --- Email Templates ---

export function listEmailTemplates(): Promise<{
  templates: AdminEmailTemplateSummary[];
}> {
  return adminReq("GET", "/v1/admin/email/templates");
}

export function getEmailTemplate(
  name: string,
): Promise<AdminEmailTemplateDetail> {
  return adminReq("GET", `/v1/admin/email/templates/${name}`);
}

export function previewEmailTemplate(
  name: string,
  params: {
    subject?: string;
    html?: string;
    text?: string;
    sample_data?: Record<string, string>;
  },
): Promise<AdminEmailTemplatePreview> {
  return adminReq("POST", `/v1/admin/email/templates/${name}/preview`, params);
}

export function updateEmailTemplate(
  name: string,
  params: {
    subject: string;
    html: string;
    text: string;
    notes?: string;
    editor_kind?: string;
    editor_state?: Record<string, unknown>;
  },
): Promise<AdminEmailTemplateDetail> {
  return adminReq("PUT", `/v1/admin/email/templates/${name}`, params);
}

export function resetEmailTemplate(
  name: string,
): Promise<{ status: string; name: string }> {
  return adminReq("DELETE", `/v1/admin/email/templates/${name}`);
}

export function sendEmailTemplate(
  name: string,
  params: {
    to: string;
    subject: string;
    html: string;
    text: string;
    sample_data?: Record<string, string>;
  },
): Promise<AdminEmailTemplateSendResult> {
  return adminReq("POST", `/v1/admin/email/templates/${name}/send`, params);
}

/**
 * Promote the current draft to published. Real sends will start using
 * the new content on the next render. Throws 404 `no_draft` if the
 * admin hasn't saved a draft yet.
 */
export function publishEmailTemplate(
  name: string,
): Promise<AdminEmailTemplateDetail> {
  return adminReq("POST", `/v1/admin/email/templates/${name}/publish`, {});
}

// --- AI-assist for email copy ---

/** Generate / rewrite / polish email copy. Admin-authenticated only;
 *  the server injects memax brand voice + marketing-legal constraints
 *  (CAN-SPAM, GDPR, CASL) into the system prompt, so the admin only
 *  picks intent + tone + locale. Returns ready-to-paste text; the
 *  client decides whether to apply it. */
export function generateAIAssistEmailCopy(
  params: import("./types").AdminAIAssistParams,
): Promise<import("./types").AdminAIAssistResult> {
  return adminReq("POST", "/v1/admin/ai-assist/email-copy", params);
}

// --- Custom campaign templates ---

export function listCampaignTemplates(
  includeArchived = false,
): Promise<import("./types").AdminCampaignTemplateListResponse> {
  const qs = includeArchived ? "?include_archived=true" : "";
  return adminReq("GET", `/v1/admin/campaign-templates${qs}`);
}

export function getCampaignTemplate(
  id: string,
): Promise<import("./types").AdminCampaignTemplate> {
  return adminReq("GET", `/v1/admin/campaign-templates/${id}`);
}

export function createCampaignTemplate(
  params: import("./types").AdminCampaignTemplateCreateParams,
): Promise<import("./types").AdminCampaignTemplate> {
  return adminReq("POST", "/v1/admin/campaign-templates", params);
}

export function updateCampaignTemplate(
  id: string,
  params: import("./types").AdminCampaignTemplateUpdateParams,
): Promise<import("./types").AdminCampaignTemplate> {
  return adminReq("PATCH", `/v1/admin/campaign-templates/${id}`, params);
}

/** Toggle the archive flag. Both directions are idempotent on the server. */
export function setCampaignTemplateArchived(
  id: string,
  archived: boolean,
): Promise<import("./types").AdminCampaignTemplate> {
  return adminReq("POST", `/v1/admin/campaign-templates/${id}/archive`, {
    archived,
  });
}

/** Hard-delete. Prefer archive unless certain. */
export function deleteCampaignTemplate(
  id: string,
): Promise<{ status: string; id: string }> {
  return adminReq("DELETE", `/v1/admin/campaign-templates/${id}`);
}

// --- Email Brand (singleton layout wrapper) ---

export function getEmailBrand(): Promise<AdminEmailBrandSettings> {
  return adminReq("GET", "/v1/admin/email/brand");
}

export function updateEmailBrand(
  params: AdminEmailBrandUpdateParams,
): Promise<AdminEmailBrandSettings> {
  return adminReq("PUT", "/v1/admin/email/brand", params);
}

// --- Stats + infra ---

export function getStats(): Promise<SystemStats> {
  return adminReq("GET", "/v1/admin/stats");
}

/** Read-only infra + security status (trust mode, origin secret,
 *  meter/rate-limit flags, version). */
export function getConfig(): Promise<AdminConfig> {
  return adminReq("GET", "/v1/admin/config");
}

// --- Notifications ---

export function sendNotification(
  params: AdminSendNotificationParams,
): Promise<AdminSendNotificationResult> {
  return adminReq("POST", "/v1/admin/notifications/send", params);
}

export function listSentNotifications(
  params?: AdminSentNotificationsParams,
): Promise<AdminSentNotificationsResponse> {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set("kind", params.kind);
  if (params?.cursor) qs.set("cursor", params.cursor);
  if (params?.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq(
    "GET",
    `/v1/admin/notifications/sent${query ? `?${query}` : ""}`,
  );
}

// Plan 18 §7.3 — super-notif funnel (parent histogram + per-cohort
// item funnel). Operator-only.
export function adminSuperNotifFunnel(params: {
  kind: "checklist" | "digest";
  sourceKind?: string;
  windowDays?: number;
}): Promise<AdminSuperNotifFunnel> {
  const qs = new URLSearchParams();
  qs.set("kind", params.kind);
  if (params.sourceKind) qs.set("source_kind", params.sourceKind);
  if (params.windowDays) qs.set("window_days", String(params.windowDays));
  return adminReq("GET", `/v1/admin/notifications/super?${qs.toString()}`);
}

// adminSuperNotifFunnelCsv fetches the admin funnel as a CSV blob
// and triggers a browser download. Goes through fetch (so the admin
// bearer token attaches) — a plain `<a download href="/v1/admin/...">`
// would skip auth entirely and 401. Codex P4 review M1.
export async function adminSuperNotifFunnelCsv(params: {
  kind: "checklist" | "digest";
  sourceKind?: string;
  windowDays?: number;
}): Promise<void> {
  const qs = new URLSearchParams();
  qs.set("kind", params.kind);
  if (params.sourceKind) qs.set("source_kind", params.sourceKind);
  if (params.windowDays) qs.set("window_days", String(params.windowDays));
  qs.set("format", "csv");
  // adminReq parses JSON envelopes by default; download the raw CSV
  // via fetch + Blob and click an in-memory <a download>.
  const { apiAuthHeaders, ApiError } = await import("@/lib/api");
  const { API_URL } = await import("@/lib/urls");
  const res = await fetch(
    `${API_URL}/v1/admin/notifications/super?${qs.toString()}`,
    { method: "GET", headers: apiAuthHeaders(), cache: "no-store" },
  );
  if (!res.ok) {
    throw new ApiError(
      `super-notif CSV export: HTTP ${res.status}`,
      "csv_export_failed",
      res.status,
      {},
    );
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `super-notif-funnel-${params.kind}-${params.windowDays ?? 30}d.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// --- Audiences ---

export function listAudiences(): Promise<{ audiences: AdminAudience[] }> {
  return adminReq("GET", "/v1/admin/audiences");
}

export function getAudience(id: string): Promise<AdminAudience> {
  return adminReq("GET", `/v1/admin/audiences/${id}`);
}

export function createAudience(
  params: AdminAudienceCreateParams,
): Promise<AdminAudience> {
  return adminReq("POST", "/v1/admin/audiences", params);
}

export function updateAudience(
  id: string,
  params: AdminAudienceUpdateParams,
): Promise<AdminAudience> {
  return adminReq("PATCH", `/v1/admin/audiences/${id}`, params);
}

export function deleteAudience(
  id: string,
): Promise<{ status: string; id: string }> {
  return adminReq("DELETE", `/v1/admin/audiences/${id}`);
}

export function estimateAudience(
  params: AdminAudienceEstimateParams,
): Promise<AdminAudienceEstimate> {
  return adminReq("POST", "/v1/admin/audiences/estimate", params);
}

// --- Campaigns ---

export function listCampaigns(
  params: AdminCampaignListParams = {},
): Promise<AdminCampaignListResponse> {
  const qs = new URLSearchParams();
  if (params.status) qs.set("status", params.status);
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq("GET", `/v1/admin/campaigns${query ? `?${query}` : ""}`);
}

export function getCampaign(id: string): Promise<AdminCampaignDetail> {
  return adminReq("GET", `/v1/admin/campaigns/${id}`);
}

export function createCampaign(
  params: AdminCampaignCreateParams,
): Promise<AdminCampaign> {
  return adminReq("POST", "/v1/admin/campaigns", params);
}

export function updateCampaignDraft(
  id: string,
  params: AdminCampaignUpdateParams,
): Promise<AdminCampaign> {
  return adminReq("PATCH", `/v1/admin/campaigns/${id}`, params);
}

export function deleteCampaign(
  id: string,
): Promise<{ status: string; id: string }> {
  return adminReq("DELETE", `/v1/admin/campaigns/${id}`);
}

export function sendCampaign(id: string): Promise<AdminCampaign> {
  return adminReq("POST", `/v1/admin/campaigns/${id}/send`, {});
}

export function scheduleCampaign(
  id: string,
  params: AdminCampaignScheduleParams,
): Promise<AdminCampaign> {
  return adminReq("POST", `/v1/admin/campaigns/${id}/schedule`, params);
}

export function cancelCampaign(id: string): Promise<AdminCampaign> {
  return adminReq("POST", `/v1/admin/campaigns/${id}/cancel`, {});
}

export interface AdminCampaignTestSendResult {
  status: string;
  campaign_id: string;
  to: string;
  subject: string;
  queued_at: string;
}

/** Send a rendered preview of the current draft to a single address. */
export function testSendCampaign(
  id: string,
  to: string,
): Promise<AdminCampaignTestSendResult> {
  return adminReq("POST", `/v1/admin/campaigns/${id}/test-send`, { to });
}

export function getCampaignDeliveryStats(
  id: string,
): Promise<import("./types").AdminCampaignDeliveryStats> {
  return adminReq("GET", `/v1/admin/campaigns/${id}/deliveries/stats`);
}

export function listCampaignAudit(
  id: string,
): Promise<{ entries: AdminCommsAuditEntry[] }> {
  return adminReq("GET", `/v1/admin/campaigns/${id}/audit`);
}

/**
 * List dream-run history for a user — spans personal hub +
 * team-hub membership. Paginated by started_at DESC; pass the
 * previous response's next_cursor as params.cursor to fetch the
 * next page.
 *
 * hubId narrows to a single hub when the admin UI is viewing a
 * specific hub rather than a user overview.
 */
export function listUserDreamRuns(
  userId: string,
  params: AdminDreamRunListParams = {},
): Promise<AdminDreamRunListResponse> {
  const qs = new URLSearchParams();
  if (params.hubId) qs.set("hubId", params.hubId);
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return adminReq(
    "GET",
    `/v1/admin/users/${userId}/dream-runs${query ? `?${query}` : ""}`,
  );
}

/**
 * Dream-run detail — run row + all actions the cycle produced + all
 * notifications tagged with this run's dream_run_id. Powers the
 * admin "audit trail" view for one cycle.
 */
export function getDreamRunDetail(runId: string): Promise<AdminDreamRunDetail> {
  return adminReq("GET", `/v1/admin/dream-runs/${runId}`);
}

// --- Waitlist orphan repair ---

/**
 * List users stranded by the 2026-04 invite-token drop:
 * active waitlist invite + users.invite_id NULL + plan in
 * free/empty. Server requires `since` (RFC3339) to force a
 * narrow scan window.
 */
export function listOrphanCandidates(
  params: AdminOrphanListParams,
): Promise<AdminOrphanListResponse> {
  const qs = new URLSearchParams();
  qs.set("since", params.since);
  if (params.limit) qs.set("limit", String(params.limit));
  return adminReq("GET", `/v1/admin/waitlist/orphans?${qs.toString()}`);
}

/**
 * Per-user orphan check. Used by the admin user-detail page to
 * conditionally render the reconcile card without loading the
 * bulk list.
 */
export function getUserOrphanStatus(
  userId: string,
): Promise<AdminUserOrphanStatus> {
  return adminReq("GET", `/v1/admin/users/${userId}/orphan-status`);
}

/**
 * Repair a specific list of orphan user ids. Idempotent — re-runs
 * on already-repaired users surface as "skipped: already_upgraded".
 * Max 100 ids per call (server-enforced).
 */
export function reconcileOrphanInvites(
  body: AdminReconcileRequest,
): Promise<AdminReconcileResponse> {
  return adminReq("POST", "/v1/admin/waitlist/reconcile", body);
}

// --- Onboarding seed memories (plan 23 §5.7) ---
//
// Admin CRUD over the seed templates that get copied into every new
// user's personal hub on signup. Edits affect FUTURE signups only;
// existing user copies stay untouched (plan 23 principle 4).

export interface AdminSeedMemory {
  id: string;
  title: string;
  content: string;
  summary?: string;
  hint?: string;
  tags: string[];
  state: string; // "active" | "archived"
  kind: string;
  stability: string;
  content_type: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface AdminSeedMemoryUpdateParams {
  /** Pointer-style — `undefined` means "no change". */
  title?: string;
  content?: string;
  summary?: string;
  hint?: string;
  /** Empty array clears all tags. `undefined` means "no change". */
  tags?: string[];
  /** "active" or "archived" — toggles enable on future signups. */
  state?: "active" | "archived";
}

export function listSeedMemories(): Promise<{ seeds: AdminSeedMemory[] }> {
  return adminReq("GET", "/v1/admin/seed-memories");
}

export function getSeedMemory(id: string): Promise<{ seed: AdminSeedMemory }> {
  return adminReq("GET", `/v1/admin/seed-memories/${id}`);
}

export function updateSeedMemory(
  id: string,
  body: AdminSeedMemoryUpdateParams,
): Promise<{ seed: AdminSeedMemory }> {
  return adminReq("PATCH", `/v1/admin/seed-memories/${id}`, body);
}

export interface AdminSeedMemoryCreateParams {
  title: string;
  content: string;
  summary?: string;
  hint?: string;
  tags?: string[];
  state?: "active" | "archived";
}

export function createSeedMemory(
  body: AdminSeedMemoryCreateParams,
): Promise<{ seed: AdminSeedMemory }> {
  return adminReq("POST", "/v1/admin/seed-memories", body);
}

export function deleteSeedMemory(id: string): Promise<{ deleted: true }> {
  return adminReq("DELETE", `/v1/admin/seed-memories/${id}`);
}

/**
 * Result of replaying the seed-copy logic against the calling admin's
 * own personal hub. `deleted` is the count of prior seed copies wiped;
 * `copied` is the count of templates re-inserted. Equal counts indicate
 * a clean refresh; `copied > deleted` means new templates were added
 * since the last sync; `copied < deleted` means templates were
 * archived or deleted between syncs.
 */
export interface AdminSeedSyncSelfResult {
  deleted: number;
  copied: number;
}

/**
 * Replays the seed-copy worker against the calling admin's own
 * personal hub so the operator can preview live seed templates
 * without making a fresh signup. Scoped to the caller — no other
 * user's onboarding is touched.
 */
export function syncSeedMemoriesToSelf(): Promise<AdminSeedSyncSelfResult> {
  return adminReq("POST", "/v1/admin/seed-memories/sync-self");
}

/**
 * Three-phase image upload for inline-markdown images in seed memory
 * bodies (plan 26 follow-up).
 *
 * 1. POST size/type metadata to /v1/admin/seed-images/upload — server
 *    validates content-type whitelist (png/jpeg/webp/gif; SVG blocked
 *    for XSS reasons) and 5MB cap, then returns a presigned PUT URL +
 *    a stable public read URL.
 * 2. PUT the file body directly to the presigned URL (R2 / MinIO
 *    handles the upload).
 * 3. Resolves with { url } — the stable public URL the caller inserts
 *    into the seed's markdown as `![alt](url)`. Public-read by design
 *    since seed images ship to every signup.
 *
 * Throws on any step's failure. Caller surfaces via toast / inline
 * status. Returns 503 if R2_PUBLIC_BASE_URL isn't configured on the
 * server (deploy-time misconfig surfaces clearly).
 */
export async function uploadSeedImage(
  file: File,
): Promise<{ url: string; objectKey: string }> {
  const presigned = (await adminReq("POST", "/v1/admin/seed-images/upload", {
    filename: file.name,
    content_type: file.type,
    size_bytes: file.size,
  })) as {
    object_key: string;
    public_url: string;
    upload_url: string;
    headers: Record<string, string> | null;
    content_type: string;
  };
  const putRes = await fetch(presigned.upload_url, {
    method: "PUT",
    headers: presigned.headers ?? { "Content-Type": presigned.content_type },
    body: file,
  });
  if (!putRes.ok) {
    throw new Error(`Image upload failed: ${putRes.status}`);
  }
  return { url: presigned.public_url, objectKey: presigned.object_key };
}
