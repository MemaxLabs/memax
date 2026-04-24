import type {
  AskOptions,
  AskResult,
  AttachmentViewURL,
  BatchDeleteResult,
  BatchMoveResult,
  ListMemoriesOptions,
  ListMemoriesResult,
  Memory,
  MemoryUpdateInput,
  PushOptions,
  RecallOptions,
  RecallResult,
  RelatedMemory,
  SearchResult,
  ShareMemoryResult,
} from "../types.js";
import type { DownloadFn, RequestFn, StreamFn } from "../transport.js";

export class MemoriesResource {
  constructor(
    private readonly req: RequestFn,
    private readonly stream: StreamFn,
    private readonly download: DownloadFn,
  ) {}

  async push(content: string, options?: PushOptions): Promise<Memory> {
    return this.req<Memory>("POST", "/v1/memories", {
      body: {
        content,
        title: options?.title ?? "",
        hint: options?.hint ?? "",
        tags: options?.tags ?? [],
        source: options?.source ?? "sdk",
        source_agent: options?.sourceAgent ?? "",
        assisted_by_agent: options?.assistedByAgent ?? "",
        initiation_type: options?.initiationType ?? "",
        source_path: options?.sourcePath,
        hub_reason: options?.hubReason ?? "",
        content_type: options?.contentType ?? "markdown",
        project_context: options?.projectContext,
        file_ref: options?.fileRef,
      },
      hubId: options?.hubId,
    });
  }

  async recall(query: string, options?: RecallOptions): Promise<RecallResult> {
    return this.req<RecallResult>("POST", "/v1/recall", {
      body: {
        query,
        limit: options?.limit ?? 10,
        kind: options?.kind,
        tags: options?.tags,
        topic_id: options?.topicId,
        include_archived: options?.includeArchived,
        no_rerank: options?.noRerank,
        source: options?.source ?? "sdk",
        working_dir: options?.workingDir,
        project_context: options?.projectContext,
      },
      hubId: options?.hubId,
      signal: options?.signal,
    });
  }

  async ask(query: string, options?: AskOptions): Promise<AskResult> {
    return this.req<AskResult>("POST", "/v1/ask", {
      body: {
        query,
        limit: options?.limit ?? 10,
        model: options?.model ?? "auto",
        locale: options?.locale,
        no_rerank: options?.noRerank,
        topic_id: options?.topicId,
      },
      hubId: options?.hubId,
      signal: options?.signal,
    });
  }

  askStream(
    query: string,
    options: AskOptions | undefined,
    onEvent: (event: string, data: unknown) => void,
  ): AbortController {
    return this.stream("POST", "/v1/ask", {
      body: {
        query,
        limit: options?.limit ?? 10,
        model: options?.model ?? "auto",
        locale: options?.locale,
        no_rerank: options?.noRerank,
        topic_id: options?.topicId,
      },
      hubId: options?.hubId,
      onEvent,
    });
  }

  async list(options?: ListMemoriesOptions): Promise<ListMemoriesResult> {
    const res = await this.req<ListMemoriesResult | Memory[]>(
      "GET",
      "/v1/memories",
      {
        query: {
          limit: options?.limit,
          cursor: options?.cursor,
          sort: options?.sort,
          kind: options?.kind,
          created_after: options?.createdAfter,
          actor: options?.actor,
          topic_id: options?.topicId,
        },
        hubId: options?.hubId,
        signal: options?.signal,
      },
    );

    if (Array.isArray(res)) {
      return {
        memories: res,
        next_cursor: "",
        has_more: false,
        total: res.length,
      };
    }

    return res;
  }

  /** Fast full-text search (trigram + tsvector, no embeddings). Target: <200ms. */
  async search(
    query: string,
    options?: {
      limit?: number;
      hubId?: string;
      topicId?: string;
      signal?: AbortSignal;
    },
  ): Promise<SearchResult[]> {
    return this.req<SearchResult[]>("GET", "/v1/memories/search", {
      query: { q: query, limit: options?.limit, topic_id: options?.topicId },
      hubId: options?.hubId,
      signal: options?.signal,
    });
  }

  async get(id: string, options?: { signal?: AbortSignal }): Promise<Memory> {
    return this.req<Memory>("GET", `/v1/memories/${id}`, {
      signal: options?.signal,
    });
  }

  /**
   * Signal that the memory was deliberately viewed by a user or agent —
   * bumps access_count and accessed_at on the server, which feeds the
   * retrieval decay multiplier. The paired endpoint (POST
   * /v1/memories/{id}/access) is the explicit companion to the pure-read
   * GET above, so speculative fetches (hover prefetches, React Query
   * refetch-on-focus, cached reloads) cannot inflate the signal.
   *
   * Call exactly once per deliberate open — e.g. when a detail page
   * mounts, a modal opens, or `memax get` runs. Failures are intentionally
   * silent: the signal is advisory, not transactional.
   */
  async trackAccessed(
    id: string,
    options?: { signal?: AbortSignal },
  ): Promise<void> {
    await this.req<void>("POST", `/v1/memories/${id}/access`, {
      signal: options?.signal,
    });
  }

  /**
   * Semantically related memories via vector nearest-neighbor. Sub-50ms, no
   * external API calls.
   *
   * Neighbor scope is derived server-side from the source memory's hub, so
   * there is no hubId parameter here — a memory's neighbors are the same set
   * regardless of which hub the viewer is currently in.
   */
  async related(
    id: string,
    options?: { signal?: AbortSignal },
  ): Promise<RelatedMemory[]> {
    return this.req<RelatedMemory[]>("GET", `/v1/memories/${id}/related`, {
      signal: options?.signal,
    });
  }

  async delete(id: string): Promise<void> {
    await this.req<void>("DELETE", `/v1/memories/${id}`);
  }

  async update(id: string, patch: MemoryUpdateInput): Promise<Memory> {
    return this.req("PATCH", `/v1/memories/${id}`, { body: patch });
  }

  async share(id: string, targetHubId: string): Promise<ShareMemoryResult> {
    return this.req("POST", `/v1/memories/${id}/share`, {
      body: { target_hub_id: targetHubId },
    });
  }

  async downloadAttachment(
    memoryID: string,
    attachmentID: string,
  ): Promise<Response> {
    return this.download(
      `/v1/memories/${memoryID}/attachments/${attachmentID}/download`,
    );
  }

  /**
   * Request a short-lived signed URL that renders the attachment
   * inline via <img src>. Reuse within the TTL window is expected;
   * the caller should cache on the client and refresh near expiry.
   *
   * Returns a rejected promise if the endpoint is unavailable (503 —
   * ATTACHMENT_VIEW_SIGNING_KEY not set on the server) or the
   * attachment is not found. Callers should fall through to
   * downloadAttachment in those cases.
   */
  async attachmentViewURL(
    memoryID: string,
    attachmentID: string,
  ): Promise<AttachmentViewURL> {
    return this.req<AttachmentViewURL>(
      "POST",
      `/v1/memories/${memoryID}/attachments/${attachmentID}/view-url`,
    );
  }

  /**
   * Delete multiple memories in a single request. Returns a structured
   * result mirroring `batchMove`: `deleted` counts committed removals
   * and `skipped` carries per-id reasons (`not_owned`, `not_found`,
   * `delete_failed`). Consumers should inspect skip reasons to pick
   * UX copy — `not_found` is usually the user's desired end state,
   * `delete_failed` is infra failure the user may retry, and
   * `not_owned` is a permission denial that retry will not fix.
   *
   * Response is normalized so `skipped` is always a real array and
   * `deleted` is always a number, even if the server omits fields or
   * encodes an empty slice as JSON null. See `batchMove` comment for
   * the rationale.
   */
  async batchDelete(ids: string[]): Promise<BatchDeleteResult> {
    const raw = await this.req<Partial<BatchDeleteResult>>(
      "POST",
      "/v1/memories/batch-delete",
      {
        body: { ids },
      },
    );
    return {
      deleted: raw?.deleted ?? 0,
      skipped: raw?.skipped ?? [],
    };
  }

  /**
   * Move multiple memories to a topic and/or hub in a single request.
   * Returns a structured result with `moved` (count committed) and
   * `skipped` (per-id reasons: `not_owned`, `not_found`,
   * `already_at_target`, `source_delete_forbidden`). The last reason
   * surfaces cross-hub moves where the caller owns the memory but
   * lacks authority to remove it from its current hub — move is
   * semantically delete-from-source + create-in-destination, so the
   * source hub's `contributor_delete_policy` is enforced.
   *
   * Response is normalized at the SDK boundary: `skipped` is guaranteed
   * to be an array (never undefined or null) and `moved` is guaranteed
   * to be a number. The Go server initializes `Skipped` to an empty
   * slice so the wire format should already conform, but the SDK
   * defends against (a) older server builds that omitted the field,
   * (b) JSON encodings that emit nil slices as `null`, and (c) future
   * middleware that might strip empty arrays. Callers can read
   * `result.skipped.length` without a defensive guard.
   */
  async batchMove(
    ids: string[],
    target: { topicId?: string; hubId?: string },
  ): Promise<BatchMoveResult> {
    const raw = await this.req<Partial<BatchMoveResult>>(
      "POST",
      "/v1/memories/batch-move",
      {
        body: { ids, topic_id: target.topicId, hub_id: target.hubId },
        hubId: target.hubId,
      },
    );
    return {
      moved: raw?.moved ?? 0,
      skipped: raw?.skipped ?? [],
    };
  }
}
