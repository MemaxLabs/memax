import type {
  Topic,
  TopicArchiveResult,
  TopicArchivedListResponse,
  TopicCreateParams,
  TopicListResponse,
  TopicMemoriesResponse,
  TopicReorderOperation,
  TopicUpdateParams,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class TopicsResource {
  constructor(private readonly req: RequestFn) {}

  async list(hubId?: string): Promise<TopicListResponse> {
    return this.req<TopicListResponse>("GET", "/v1/topics", {
      query: { hub_id: hubId },
    });
  }

  async get(id: string, hubId?: string): Promise<Topic> {
    return this.req<Topic>("GET", `/v1/topics/${id}`, { hubId });
  }

  async create(params: TopicCreateParams, hubId?: string): Promise<Topic> {
    return this.req<Topic>("POST", "/v1/topics", { body: params, hubId });
  }

  /**
   * update patches an existing topic. Name/description/icon/pinned/position
   * are thin field updates. A `parent_id` change runs the full reparent
   * validation pipeline on the server:
   *
   *   - `invalid_parent` — parent id does not exist in the moving topic's
   *     hub. This code collapses "no such topic" with "topic in a different
   *     hub" to avoid leaking cross-hub existence; if you need to
   *     distinguish them for admin tooling, do it server-side with a
   *     dedicated error code at that point.
   *   - `cycle_detected` — parent id equals the moving topic id, or is a
   *     transitive descendant of it (would create a cycle).
   *   - `max_depth_subtree` — the moving topic's subtree, planted under the
   *     new parent, would exceed the 5-level (0-indexed cap 4) hard limit.
   *   - `max_depth` — legacy code from the Create path; preserved for
   *     symmetry.
   *   - `duplicate_name` — a sibling with the same name already exists at
   *     the destination.
   *
   * On success, the server flips UserModified = true on the moving topic
   * so the dream engine respects the manual intent, parity with rename.
   *
   * Pass `parent_id: ""` (or `null`) to reparent to the root.
   */
  async update(
    id: string,
    params: TopicUpdateParams,
    hubId?: string,
  ): Promise<Topic> {
    return this.req<Topic>("PATCH", `/v1/topics/${id}`, {
      body: params,
      hubId,
    });
  }

  async delete(id: string, hubId?: string): Promise<void> {
    await this.req<void>("DELETE", `/v1/topics/${id}`, { hubId });
  }

  /**
   * listArchived returns the hub's archived topics as a flat list, most
   * recently archived first. Archived topics keep their memory assignments
   * (restore is lossless) but are hidden from the active tree and excluded
   * from AI organization (dreams, inline classification, recall boost).
   */
  async listArchived(hubId?: string): Promise<TopicArchivedListResponse> {
    return this.req<TopicArchivedListResponse>("GET", "/v1/topics/archived", {
      query: { hub_id: hubId },
      hubId,
    });
  }

  /**
   * archive soft-archives a topic and its entire subtree. Idempotent —
   * archiving an already-archived topic returns archived_count 0.
   *
   * While archived, the topic is read-only: update, memory assignment, and
   * creating children all return 409 (`topic_archived` / `parent_archived`)
   * until the topic is restored.
   */
  async archive(id: string, hubId?: string): Promise<TopicArchiveResult> {
    return this.req<TopicArchiveResult>("POST", `/v1/topics/${id}/archive`, {
      body: {},
      hubId,
    });
  }

  /**
   * restore un-archives a topic and its subtree. If the topic's parent is
   * still archived (partial branch restore), the topic is re-planted at the
   * root so it never hangs under an invisible node. Idempotent — restoring
   * an active topic returns restored_count 0.
   */
  async restore(id: string, hubId?: string): Promise<TopicArchiveResult> {
    return this.req<TopicArchiveResult>("POST", `/v1/topics/${id}/restore`, {
      body: {},
      hubId,
    });
  }

  async listMemories(
    topicId: string,
    options?: { limit?: number; cursor?: string; hubId?: string },
  ): Promise<TopicMemoriesResponse> {
    return this.req<TopicMemoriesResponse>(
      "GET",
      `/v1/topics/${topicId}/memories`,
      {
        query: {
          limit: options?.limit,
          cursor: options?.cursor,
        },
        hubId: options?.hubId,
      },
    );
  }

  /**
   * assignMemory is the AUTO-ASSIGNMENT primitive — confidence-gated on the
   * server (ingest + dreams workers only). User-initiated moves MUST go
   * through memories.batchMove, which is authoritative and replaces
   * unconditionally.
   *
   * The HTTP route is unchanged (POST /v1/topics/{id}/memories) so external
   * API users are not broken — only the TypeScript method name changes to
   * match the actual auto-assignment semantics.
   */
  async assignMemory(
    topicId: string,
    memoryId: string,
    confidence?: number,
    hubId?: string,
  ): Promise<void> {
    await this.req<void>("POST", `/v1/topics/${topicId}/memories`, {
      body: { memory_id: memoryId, ...(confidence != null && { confidence }) },
      hubId,
    });
  }

  /**
   * unassignMemory removes an auto-assigned topic link. Like assignMemory,
   * this is reserved for worker surfaces. User-initiated clears go through
   * memories.batchMove with a hub-only target (no topic_id).
   */
  async unassignMemory(
    topicId: string,
    memoryId: string,
    hubId?: string,
  ): Promise<void> {
    await this.req<void>(
      "DELETE",
      `/v1/topics/${topicId}/memories/${memoryId}`,
      { hubId },
    );
  }

  async reorder(
    operations: TopicReorderOperation[],
    hubId?: string,
  ): Promise<void> {
    await this.req<void>("POST", "/v1/topics/reorder", {
      body: { operations },
      hubId,
    });
  }

  /**
   * markVisit records that the caller has visited this topic now. This
   * anchors the clear-on-visit semantics for scan-surface dream-delta
   * signals (memory row breadcrumb tint + topic card delta chip).
   *
   * Clients should fire this ONLY on real topic page mount — never from
   * prefetch, hover, or memory detail. The server response is a plain
   * ack; after a successful write, invalidate `['topics']` and
   * `['memories']` query caches so lifecycle signals resolve against
   * the updated visit timestamp on the next read.
   */
  async markVisit(topicId: string): Promise<void> {
    await this.req<void>("POST", `/v1/topics/${topicId}/visit`, {
      body: {},
    });
  }
}
