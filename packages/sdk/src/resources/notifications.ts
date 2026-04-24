import type {
  Notification,
  NotificationBulkQuery,
  NotificationBulkResult,
  NotificationListQuery,
  NotificationListResponse,
  NotificationResolveAction,
  NotificationResolveResult,
  NotificationSummary,
} from "../types.js";
import type { RequestFn } from "../transport.js";

/**
 * NotificationsResource is the SDK client for the /v1/notifications
 * surface.
 */
export class NotificationsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * GET /v1/notifications
   *
   * Returns the page of notifications visible to the caller (union of
   * hub membership and direct user address). Defaults to pending rows;
   * pass `status: "resolved"` + `resolution` filters for history views.
   */
  async list(query?: NotificationListQuery): Promise<NotificationListResponse> {
    return this.req("GET", "/v1/notifications", {
      query: {
        hub: query?.hub,
        status: query?.status,
        kind: query?.kind,
        resolution: query?.resolution,
        unseen_only: query?.unseenOnly,
        since: query?.since,
        limit: query?.limit,
        cursor: query?.cursor,
      },
    });
  }

  /**
   * GET /v1/notifications/summary
   *
   * Returns the canonical summary shape from plan §4.4: badge-dot
   * total, Updates bucket totals, and a `by_kind` map pre-populated
   * with zero counts for every supported kind.
   */
  async summary(hubId?: string): Promise<NotificationSummary> {
    return this.req("GET", "/v1/notifications/summary", {
      query: { hub: hubId },
    });
  }

  /**
   * POST /v1/notifications/{id}/seen (idempotent)
   *
   * Writes seen_at=now() on a row the caller can see. Called by the
   * bar push renderer the moment a notification is shown so the
   * same row cannot re-push on reconnect or first-load hydration.
   */
  async markSeen(id: string, hubId?: string): Promise<{ status: "seen" }> {
    return this.req("POST", `/v1/notifications/${id}/seen`, { hubId });
  }

  /**
   * POST /v1/notifications/{id}/dismiss (receipt-only)
   *
   * Server refuses decision kinds with
   * `400 dismiss_not_applicable_to_decision`. Clients should check
   * `notification.kind` against the decision-kind set before calling
   * so the affordance never appears on a decision row.
   */
  async dismiss(id: string, hubId?: string): Promise<{ status: "dismissed" }> {
    return this.req("POST", `/v1/notifications/${id}/dismiss`, { hubId });
  }

  /**
   * POST /v1/notifications/{id}/resolve (decision-only)
   *
   * Server validates action against the per-kind allow-list and
   * refuses with `400 invalid_action_for_notification_kind` on a
   * mismatch. Mirrors the Phase 1 /v1/reviews/resolve contract so
   * the Phase 4 client migration is a straight swap of endpoint
   * paths.
   */
  async resolve(
    id: string,
    action: NotificationResolveAction,
    hubId?: string,
  ): Promise<NotificationResolveResult> {
    return this.req("POST", `/v1/notifications/${id}/resolve`, {
      hubId,
      body: { action },
    });
  }

  /**
   * POST /v1/notifications/seen
   *
   * Bulk mark-seen for every pending row matching the filter.
   * Decision kinds are refused with
   * `400 bulk_not_allowed_for_decision_kind`.
   */
  async bulkSeen(
    query: NotificationBulkQuery,
  ): Promise<NotificationBulkResult> {
    return this.req("POST", "/v1/notifications/seen", {
      hubId: query.hub,
      body: { kinds: query.kinds, since: query.since },
    });
  }

  /**
   * POST /v1/notifications/dismiss
   *
   * Bulk dismiss for every pending receipt row matching the filter.
   */
  async bulkDismiss(
    query: NotificationBulkQuery,
  ): Promise<NotificationBulkResult> {
    return this.req("POST", "/v1/notifications/dismiss", {
      hubId: query.hub,
      body: { kinds: query.kinds, since: query.since },
    });
  }
}

// Re-export the list-response row type so callers can import it
// from the resource module alongside the client.
export type { Notification };
