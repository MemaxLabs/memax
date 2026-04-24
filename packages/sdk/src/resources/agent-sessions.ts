import type {
  AgentSession,
  DeletedAgentSession,
  AgentSessionLocalDeleteRequest,
  RestoreDeletedSessionRequest,
  AgentSessionSyncAckRequest,
  AgentSessionSyncRequest,
  AgentSessionUpsertRequest,
  SessionSyncPlanAction,
  ResolveDivergenceRequest,
  ResolveDivergenceResponse,
} from "../types.js";
import type { DownloadFn, RequestFn } from "../transport.js";

export class AgentSessionsResource {
  constructor(
    private readonly req: RequestFn,
    private readonly download: DownloadFn,
  ) {}

  async sync(
    request: AgentSessionSyncRequest,
  ): Promise<{ actions: SessionSyncPlanAction[] }> {
    return this.req("POST", "/v1/agent-sessions/sync", { body: request });
  }

  async ack(request: AgentSessionSyncAckRequest): Promise<void> {
    await this.req("POST", "/v1/agent-sessions/ack", { body: request });
  }

  async localDelete(request: AgentSessionLocalDeleteRequest): Promise<void> {
    await this.req("POST", "/v1/agent-sessions/local-delete", {
      body: request,
    });
  }

  async get(id: string): Promise<AgentSession> {
    return this.req("GET", `/v1/agent-sessions/${id}`);
  }

  async upsert(session: AgentSessionUpsertRequest): Promise<AgentSession> {
    return this.req("PUT", "/v1/agent-sessions", { body: session });
  }

  async list(options?: {
    agent?: string;
  }): Promise<{ sessions: AgentSession[] }> {
    return this.req("GET", "/v1/agent-sessions", {
      query: { agent: options?.agent },
    });
  }

  async delete(id: string): Promise<void> {
    await this.req<void>("DELETE", `/v1/agent-sessions/${id}`);
  }

  async listDeleted(): Promise<{ sessions: DeletedAgentSession[] }> {
    return this.req("GET", "/v1/agent-sessions/deleted");
  }

  async restore(request: RestoreDeletedSessionRequest): Promise<AgentSession> {
    return this.req("POST", "/v1/agent-sessions/restore", { body: request });
  }

  async downloadBlob(id: string): Promise<Response> {
    return this.download(`/v1/agent-sessions/${id}/download`);
  }

  /** Atomically resolve a diverged session — snapshots the losing branch. */
  async resolveDivergence(
    request: ResolveDivergenceRequest,
  ): Promise<ResolveDivergenceResponse> {
    return this.req("POST", "/v1/agent-sessions/resolve-divergence", {
      body: request,
    });
  }
}
