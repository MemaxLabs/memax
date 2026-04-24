import type {
  AgentConfig,
  AgentConfigBatchDeleteResult,
  DeletedAgentConfig,
  AgentConfigListResult,
  ConfigLocalDeleteRequest,
  ConfigMergeRequest,
  RestoreDeletedConfigRequest,
  ConfigSyncAckRequest,
  ConfigSyncRequest,
  ConfigUpsertRequest,
  SyncPlanAction,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class ConfigsResource {
  constructor(private readonly req: RequestFn) {}

  async sync(
    request: ConfigSyncRequest,
  ): Promise<{ actions: SyncPlanAction[] }> {
    return this.req("POST", "/v1/configs/sync", { body: request });
  }

  async ack(request: ConfigSyncAckRequest): Promise<void> {
    await this.req("POST", "/v1/configs/ack", { body: request });
  }

  async localDelete(request: ConfigLocalDeleteRequest): Promise<void> {
    await this.req("POST", "/v1/configs/local-delete", { body: request });
  }

  async get(id: string): Promise<AgentConfig> {
    return this.req("GET", `/v1/configs/${id}`);
  }

  async upsert(config: ConfigUpsertRequest): Promise<AgentConfig> {
    return this.req("PUT", "/v1/configs", { body: config });
  }

  async list(options?: { agent?: string }): Promise<AgentConfigListResult> {
    return this.req("GET", "/v1/configs", {
      query: { agent: options?.agent },
    });
  }

  async listDeleted(): Promise<{ configs: DeletedAgentConfig[] }> {
    return this.req("GET", "/v1/configs/deleted");
  }

  async delete(id: string): Promise<void> {
    await this.req<void>("DELETE", `/v1/configs/${id}`);
  }

  /**
   * Delete multiple agent configs in a single request. Mirrors
   * `memories.batchDelete` — returns a structured
   * `AgentConfigBatchDeleteResult` with `deleted` count and a `skipped`
   * array carrying per-id reasons (`not_found` | `delete_failed`).
   *
   * Partial-success is the default: the server commits what it can and
   * reports the rest. `deleted > 0` with non-empty `skipped` is a normal
   * outcome. Clients should inspect `skipped[].reason` to pick the right
   * toast copy — `not_found` is "already gone" (idempotent success),
   * `delete_failed` is retryable infra failure.
   *
   * Response is normalized at the SDK boundary so `skipped` is always a
   * real array and `deleted` is always a number, even if the server
   * response omits fields or encodes an empty slice as JSON null. This
   * matches the same defense-in-depth used by `memories.batchDelete` and
   * `memories.batchMove`.
   */
  async batchDelete(ids: string[]): Promise<AgentConfigBatchDeleteResult> {
    const raw = await this.req<Partial<AgentConfigBatchDeleteResult>>(
      "POST",
      "/v1/configs/batch-delete",
      { body: { ids } },
    );
    return {
      deleted: raw?.deleted ?? 0,
      skipped: raw?.skipped ?? [],
    };
  }

  async restore(request: RestoreDeletedConfigRequest): Promise<AgentConfig> {
    return this.req("POST", "/v1/configs/restore", { body: request });
  }

  async merge(
    request: ConfigMergeRequest,
  ): Promise<{ merged_content: string }> {
    return this.req("POST", "/v1/configs/merge", { body: request });
  }
}
