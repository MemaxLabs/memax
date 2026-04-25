import type {
  DreamReport,
  DreamRunListOptions,
  DreamRunListResponse,
  DreamUsage,
  DreamUsageOptions,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class DreamsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * List recent dream runs for the caller, across hubs they
   * belong to. Keyset paginated: pass the previous response's
   * `next_cursor` as `cursor` to fetch the next page.
   *
   * Scoping:
   *   - `hubId` narrows to one hub (the caller must be a member,
   *     else the server returns 403).
   *   - Omitting `hubId` spans every hub the caller participates
   *     in — the default behavior for the Dream history view.
   *
   * Default limit is 20 on the server (max 100). Passing a
   * `limit` overrides that per request.
   */
  async list(opts: DreamRunListOptions = {}): Promise<DreamRunListResponse> {
    const query: Record<string, string | number | undefined> = {};
    if (opts.limit !== undefined) query.limit = opts.limit;
    if (opts.hubId !== undefined) query.hub_id = opts.hubId;
    if (opts.cursor !== undefined) query.cursor = opts.cursor;
    return this.req("GET", "/v1/dreams", { query });
  }

  async report(hubId?: string): Promise<DreamReport> {
    return this.req("GET", "/v1/dreams/report", { hubId });
  }

  async trigger(hubId?: string): Promise<{ status: string }> {
    return this.req("POST", "/v1/dreams/trigger", {
      body: {},
      hubId,
    });
  }

  /**
   * Read-only quota snapshot for the dream-tier counters.
   *
   * Pass `hubId` to query a team hub's per-hub Lucid pool; omit it
   * for the caller's personal quota. Hub-scoped credentials always
   * resolve to their scoped hub regardless of `hubId` — querying a
   * different hub silently returns the scoped one.
   *
   * Use this to render quota state in the UI ("X of Y dreams used
   * this month") and gate the "Dream now" button on `allowed`.
   *
   * Returns 503 on dev/test environments where the plan resolver
   * isn't wired (the SDK throws via the standard MemaxError path).
   */
  async usage(opts: DreamUsageOptions = {}): Promise<DreamUsage> {
    const query: Record<string, string | undefined> = {};
    if (opts.hubId !== undefined) query.hub_id = opts.hubId;
    return this.req("GET", "/v1/usage/dreams", { query });
  }
}
