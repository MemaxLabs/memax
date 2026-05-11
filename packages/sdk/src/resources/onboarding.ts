import type { Notification, NotificationStatus } from "../types.js";
import type { RequestFn } from "../transport.js";

/**
 * OnboardingState — the small lookup the Settings → Getting started
 * row uses to decide button label + helper text without paging
 * through the full notifications list.
 */
export interface OnboardingState {
  /** True when the user has at least one prior `source_kind=onboarding`
   *  row (any status). Drives the "Start" vs "Restart" button label. */
  has_prior: boolean;
  /** Highest version observed; 0 when no prior row. */
  current_version: number;
  /** Status of the latest row, if any. */
  current_status?: NotificationStatus;
}

/**
 * OnboardingResource is the plan-18 onboarding-specific surface.
 * Everything else about the onboarding lifecycle (read, dismiss,
 * complete-item) lives on `notifications` — the onboarding checklist
 * is just a super-notif row.
 */
export class OnboardingResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * GET /v1/onboarding/state — Settings UI lookup.
   */
  async state(): Promise<OnboardingState> {
    return this.req("GET", "/v1/onboarding/state");
  }

  /**
   * POST /v1/onboarding/restart (plan 18 §3.3)
   *
   * Resolves the current pending checklist (if any) with
   * resolution=dismissed, then emits a fresh checklist row at the
   * next version. Initial state (no prior row) also goes through
   * this path — the server emits v1.
   *
   * Server is rate-limited to 3 calls per 24h per user.
   */
  async restart(): Promise<Notification> {
    return this.req("POST", "/v1/onboarding/restart", {});
  }
}
