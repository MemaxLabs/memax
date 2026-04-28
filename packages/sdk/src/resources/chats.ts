import type {
  CancelChatMessageResult,
  ChatMessage,
  ChatSession,
  ChatStreamOptions,
  ChatToolDescriptor,
  CreateChatSessionInput,
  DecideApprovalResult,
  ListChatMessagesOptions,
  ListChatMessagesResult,
  ListChatSessionsOptions,
  ListChatSessionsResult,
  PatchChatSessionInput,
  RegenerateChatMessageResult,
  SendChatMessageInput,
  SendChatMessageResult,
} from "../types.js";
import type { RequestFn, StreamFn } from "../transport.js";

/**
 * ChatResource is the SDK surface for plan 24's Agent Chat.
 *
 * Endpoint mapping:
 *   POST   /v1/chat/sessions                                          → createSession
 *   GET    /v1/chat/sessions                                          → listSessions
 *   GET    /v1/chat/sessions/{id}                                     → getSession
 *   PATCH  /v1/chat/sessions/{id}                                     → patchSession
 *   DELETE /v1/chat/sessions/{id}                                     → deleteSession
 *   POST   /v1/chat/sessions/{id}/messages                            → sendMessage
 *   GET    /v1/chat/sessions/{id}/messages                            → listMessages
 *   GET    /v1/chat/sessions/{id}/messages/{msg_id}                   → getMessage
 *   GET    /v1/chat/sessions/{id}/messages/{msg_id}/stream            → streamMessage
 *   POST   /v1/chat/sessions/{id}/messages/{msg_id}/cancel            → cancelMessage
 *   POST   /v1/chat/sessions/{id}/messages/{msg_id}/regenerate        → regenerateMessage
 *   GET    /v1/chat/tools                                             → listTools
 *   POST   /v1/chat/sessions/{id}/approvals/{approval_id}             → decideApproval
 *
 * Owner isolation, hub-scope gating, idempotency replay, and the
 * cancel/regenerate semantics are enforced server-side; the SDK is
 * a thin typed transport. See plan 24 §"Send-message + resume" for
 * the lifecycle and §"Cancel UX" for the soft-cancel pattern.
 */
export class ChatsResource {
  constructor(
    private readonly req: RequestFn,
    private readonly stream: StreamFn,
  ) {}

  async createSession(
    input: CreateChatSessionInput,
    options?: { hubId?: string; signal?: AbortSignal },
  ): Promise<ChatSession> {
    return this.req<ChatSession>("POST", "/v1/chat/sessions", {
      body: {
        title: input.title,
        scope_type: input.scopeType,
        scope_hub_ids: input.scopeHubIds,
        write_hub_id: input.writeHubId,
        tools: input.tools,
        model: input.model,
      },
      hubId: options?.hubId,
      signal: options?.signal,
    });
  }

  async listSessions(
    options?: ListChatSessionsOptions,
  ): Promise<ListChatSessionsResult> {
    return this.req<ListChatSessionsResult>("GET", "/v1/chat/sessions", {
      query: {
        cursor: options?.cursor,
        limit: options?.limit,
        archived: options?.archived,
        pinned: options?.pinned,
      },
      signal: options?.signal,
    });
  }

  async getSession(
    id: string,
    options?: { signal?: AbortSignal },
  ): Promise<ChatSession> {
    return this.req<ChatSession>("GET", `/v1/chat/sessions/${id}`, {
      signal: options?.signal,
    });
  }

  /**
   * Partial update. All fields are optional; absent fields leave
   * the existing value in place. Scope (scope_type / scope_hub_ids /
   * write_hub_id-with-restrictions) is not patchable beyond the
   * write_hub_id case server-side; sending other scope fields will
   * surface a 400 from the server.
   */
  async patchSession(
    id: string,
    patch: PatchChatSessionInput,
  ): Promise<ChatSession> {
    return this.req<ChatSession>("PATCH", `/v1/chat/sessions/${id}`, {
      body: {
        title: patch.title,
        pinned: patch.pinned,
        archived: patch.archived,
        tools: patch.tools,
        write_hub_id: patch.writeHubId,
      },
    });
  }

  /** Soft-delete. The session row is tombstoned; messages are retained for audit. */
  async deleteSession(id: string): Promise<void> {
    await this.req<void>("DELETE", `/v1/chat/sessions/${id}`);
  }

  /**
   * Send a user message. Returns one of three outcomes:
   *   - `fresh` (HTTP 202): new turn enqueued, connect to
   *     `result.resume_url` for the SSE stream.
   *   - `in_flight` (HTTP 202): an identical idempotency-key turn
   *     is already mid-flight; same resume_url.
   *   - `replay` (HTTP 200): an identical idempotency-key turn
   *     already finalized; assistant_message carries the persisted
   *     terminal content.
   *
   * The SDK normalizes all three into a single typed shape so the
   * client only needs one decoder.
   */
  async sendMessage(
    sessionId: string,
    input: SendChatMessageInput,
  ): Promise<SendChatMessageResult> {
    return this.req<SendChatMessageResult>(
      "POST",
      `/v1/chat/sessions/${sessionId}/messages`,
      {
        body: {
          content: input.content,
          idempotency_key: input.idempotencyKey,
        },
      },
    );
  }

  async listMessages(
    sessionId: string,
    options?: ListChatMessagesOptions,
  ): Promise<ListChatMessagesResult> {
    return this.req<ListChatMessagesResult>(
      "GET",
      `/v1/chat/sessions/${sessionId}/messages`,
      {
        query: {
          cursor: options?.cursor,
          limit: options?.limit,
        },
        signal: options?.signal,
      },
    );
  }

  async getMessage(
    sessionId: string,
    messageId: string,
    options?: { signal?: AbortSignal },
  ): Promise<ChatMessage> {
    return this.req<ChatMessage>(
      "GET",
      `/v1/chat/sessions/${sessionId}/messages/${messageId}`,
      { signal: options?.signal },
    );
  }

  /**
   * Open the SSE replay+tail stream for one assistant message.
   * Returns an AbortController; calling `controller.abort()` closes
   * the stream cleanly. The server keeps the replay buffer for 24h;
   * past that, the connect returns 410 chat_replay_expired (surfaces
   * via `onEvent("error", ...)`).
   *
   * Resume: pass `lastEventId` to receive only events with
   * `seq > lastEventId`. Empty / zero / unset means "from the start
   * of the buffer". Use the `id` of the last successfully-consumed
   * event when reconnecting after a disconnect — same contract as
   * native EventSource.
   *
   * Event names: see `ChatStreamEventName` in types.ts. Forward-
   * compatibility: new events MUST not crash an older client that
   * only handles the listed names — branch on event name and ignore
   * unknowns.
   */
  streamMessage(
    sessionId: string,
    messageId: string,
    options: ChatStreamOptions & {
      onEvent: (event: string, data: unknown) => void;
      onClose?: () => void;
    },
  ): AbortController {
    const headers: Record<string, string> = {};
    if (options.lastEventId !== undefined && options.lastEventId !== "") {
      headers["Last-Event-ID"] = String(options.lastEventId);
    }
    return this.stream(
      "GET",
      `/v1/chat/sessions/${sessionId}/messages/${messageId}/stream`,
      {
        hubId: options.hubId,
        extraHeaders: headers,
        onEvent: options.onEvent,
        onClose: options.onClose,
      },
    );
  }

  /**
   * Soft-cancel an in-flight assistant turn. Always returns 200 when
   * the row is reachable to the caller — re-cancel of a finished
   * message is a UX no-op (`cancel_registered: false` + the row's
   * actual status). 4xx codes only fire on shape errors (404 for
   * cross-owner / wrong session, 400 for cancel against a user
   * message).
   *
   * Behavior: the handler writes `status='canceling'`, the worker's
   * cancel watcher observes the flip and finalizes via
   * FinalizeChatMessage with `status='canceled'`. Single-source-of-
   * truth terminal writes are preserved; clients only need to
   * watch the SSE stream for the terminal frame.
   */
  async cancelMessage(
    sessionId: string,
    messageId: string,
  ): Promise<CancelChatMessageResult> {
    return this.req<CancelChatMessageResult>(
      "POST",
      `/v1/chat/sessions/${sessionId}/messages/${messageId}/cancel`,
    );
  }

  /**
   * Regenerate the reply for a finalized assistant turn. The new
   * assistant is linked to the prior assistant via
   * `parent_message_id` (supersession); the worker walks the chain
   * back to the original user message to drive the new turn.
   *
   * Eligibility: the prior assistant must be in a natural-terminal
   * state (`completed` / `partial_failed` / `failed`). Canceled
   * messages return 409 `regenerate_not_eligible` — the user
   * already chose to stop, so resending is a fresh send. In-flight
   * sessions return 409 `chat_session_locked` with active_message_id
   * details so the client can connect to the existing stream.
   */
  async regenerateMessage(
    sessionId: string,
    priorAssistantId: string,
  ): Promise<RegenerateChatMessageResult> {
    return this.req<RegenerateChatMessageResult>(
      "POST",
      `/v1/chat/sessions/${sessionId}/messages/${priorAssistantId}/regenerate`,
    );
  }

  /**
   * Catalog of tools the chat surface knows about. Filter by
   * `available` for the runtime-resolvable subset, or render the
   * full list to communicate the roadmap. `default_in_chat` is the
   * subset auto-enabled on a fresh session that doesn't pass `tools`
   * at create time.
   */
  async listTools(options?: {
    signal?: AbortSignal;
  }): Promise<ChatToolDescriptor[]> {
    return this.req<ChatToolDescriptor[]>("GET", "/v1/chat/tools", {
      signal: options?.signal,
    });
  }

  /**
   * Decide a pending tool-approval. `decision` is `"approve"` or
   * `"deny"`. Idempotency: a second decision against an already-
   * decided row returns 409 `chat_approval_already_decided`; a
   * decision against an expired row returns 408
   * `chat_approval_timeout`. Both should surface as terminal in
   * the UI — never auto-retry.
   */
  async decideApproval(
    sessionId: string,
    approvalId: string,
    decision: "approve" | "deny",
  ): Promise<DecideApprovalResult> {
    return this.req<DecideApprovalResult>(
      "POST",
      `/v1/chat/sessions/${sessionId}/approvals/${approvalId}`,
      { body: { decision } },
    );
  }
}
