import type {
  ConnectedAgentUpdate,
  ConnectedAgentWithStats,
  DisconnectAgentResult,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class AgentsResource {
  constructor(private readonly req: RequestFn) {}

  async list(): Promise<ConnectedAgentWithStats[]> {
    return this.req("GET", "/v1/agents");
  }

  async update(
    slug: string,
    input: ConnectedAgentUpdate,
  ): Promise<ConnectedAgentWithStats> {
    return this.req("PATCH", `/v1/agents/${slug}`, {
      body: {
        display_name: input.displayName,
        icon: input.icon,
      },
    });
  }

  /**
   * Disconnect a connected agent: revoke all API keys, tombstone +
   * delete synced configs, remove the agent row. Memories are
   * preserved — they belong to the user, not the agent.
   *
   * Returns a structured `DisconnectAgentResult` with the cascade
   * counts (`keys_revoked`, `configs_tombstoned`) and a `skipped`
   * array carrying per-reason skip entries. Partial-success shape
   * mirrors `memories.batchDelete` and `configs.batchDelete`.
   *
   * Response is normalized at the SDK boundary so `skipped` is always
   * a real array and the numeric fields are always numbers, even when
   * the server response omits fields.
   */
  async disconnect(slug: string): Promise<DisconnectAgentResult> {
    const raw = await this.req<Partial<DisconnectAgentResult>>(
      "DELETE",
      `/v1/agents/${slug}`,
    );
    return {
      disconnected: raw?.disconnected ?? false,
      keys_revoked: raw?.keys_revoked ?? 0,
      configs_tombstoned: raw?.configs_tombstoned ?? 0,
      skipped: raw?.skipped ?? [],
    };
  }
}
