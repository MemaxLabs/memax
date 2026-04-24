import type { BarSearchResult } from "../types.js";
import type { RequestFn } from "../transport.js";

export class BarResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Unified search for the web bar's pre-Enter "quick matches" layer.
   * Returns memory FTS matches plus topic and hub jump-to candidates
   * in a single response. Empty query returns empty arrays instead of
   * a 400 so the client can call this unconditionally.
   */
  async search(
    query: string,
    options?: {
      limit?: number;
      hubId?: string;
      topicId?: string;
      signal?: AbortSignal;
    },
  ): Promise<BarSearchResult> {
    return this.req<BarSearchResult>("GET", "/v1/bar/search", {
      query: { q: query, limit: options?.limit, topic_id: options?.topicId },
      hubId: options?.hubId,
      signal: options?.signal,
    });
  }
}
