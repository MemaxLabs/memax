"use client";

/**
 * Chat hooks — Plan 24 §"UI surface (web)" + §"Send-message + resume".
 *
 * These hooks own the React Query cache axis for chat sessions and
 * messages. The transport is `memax-sdk`'s ChatsResource (added in
 * the same Phase 3.7c batch). The mutation callsites carry
 * `meta.errorMessage` so the global mutation-toast bridge in
 * `lib/query-client.ts` surfaces a faithful error toast on
 * failure — see the per-mutation comments for the message
 * vocabulary.
 *
 * Cache shape:
 *   ["chat", "sessions", "list", listOpts]
 *   ["chat", "sessions", "detail", sessionId]
 *   ["chat", "messages", "list", sessionId, listOpts]
 *   ["chat", "messages", "detail", sessionId, messageId]
 *   ["chat", "tools"]
 *
 * Invalidations after mutations stay coarse (`["chat", "sessions"]`,
 * `["chat", "messages", sessionId]`) — chat lists are short and
 * the perf cost of a full refetch is tiny compared to the
 * complexity of optimistic-merging structurally diverse mutations
 * (cancel produces a status flip, regenerate produces a new row,
 * send produces two rows + a stream). Optimistic patches are
 * reserved for cases where the round-trip latency is user-
 * visible; chat sends already drive an SSE stream that lands the
 * authoritative state in <100ms.
 */

import { useMutation, useQuery, type QueryKey } from "@tanstack/react-query";
import type {
  CancelChatMessageResult,
  ChatMessage,
  ChatSession,
  ChatToolDescriptor,
  CreateChatSessionInput,
  ListChatMessagesOptions,
  ListChatMessagesResult,
  ListChatSessionsOptions,
  ListChatSessionsResult,
  PatchChatSessionInput,
  RegenerateChatMessageResult,
  SendChatMessageInput,
  SendChatMessageResult,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useLocale } from "@/i18n";

// --- Query keys ---------------------------------------------------

export const chatQueryPrefix = (): readonly ["chat"] => ["chat"] as const;

const sessionsListKey = (
  opts: ListChatSessionsOptions | undefined,
): QueryKey => ["chat", "sessions", "list", opts ?? {}];

const sessionDetailKey = (id: string): QueryKey => [
  "chat",
  "sessions",
  "detail",
  id,
];

// Exported so ChatBrainView can seed the cache with an optimistic user
// message before navigating to a freshly-created session — keeps the
// thread render path instant on first paint instead of falling through
// to the spinner during create-then-send latency (issue 2 fix).
export const messagesListKey = (
  sessionId: string,
  opts: ListChatMessagesOptions | undefined,
): QueryKey => ["chat", "messages", "list", sessionId, opts ?? {}];

const toolsKey = (): QueryKey => ["chat", "tools"];

// --- Sessions: list / get -----------------------------------------

export function useChatSessions(opts?: ListChatSessionsOptions) {
  return useQuery<ListChatSessionsResult>({
    queryKey: sessionsListKey(opts),
    queryFn: ({ signal }) =>
      getMemaxClient().chats.listSessions({ ...opts, signal }),
    staleTime: 15_000,
  });
}

export function useChatSession(id: string | undefined) {
  return useQuery<ChatSession>({
    queryKey: id ? sessionDetailKey(id) : ["chat", "sessions", "detail", "_"],
    queryFn: ({ signal }) => getMemaxClient().chats.getSession(id!, { signal }),
    enabled: Boolean(id),
    staleTime: 30_000,
  });
}

// --- Sessions: create / patch / delete ----------------------------

export function useCreateChatSession() {
  const { t } = useLocale();
  return useMutation<ChatSession, Error, CreateChatSessionInput>({
    mutationFn: (input) => getMemaxClient().chats.createSession(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["chat", "sessions"] });
    },
    meta: {
      // Generic copy — most failures here are quota / offline / 5xx;
      // the global classifier handles those. Hub-scope errors render
      // server message verbatim via the classifier's fallback.
      errorMessage: t.chat.errors.createSession,
    },
  });
}

export function usePatchChatSession(sessionId: string) {
  const { t } = useLocale();
  return useMutation<ChatSession, Error, PatchChatSessionInput>({
    mutationFn: (patch) =>
      getMemaxClient().chats.patchSession(sessionId, patch),
    onSuccess: (next) => {
      // Patch the detail cache directly so the UI updates without
      // a refetch round-trip; the list invalidation below covers
      // any title/pinned/archived field that affects ordering.
      queryClient.setQueryData(sessionDetailKey(sessionId), next);
      void queryClient.invalidateQueries({
        queryKey: ["chat", "sessions", "list"],
      });
    },
    meta: { errorMessage: t.chat.errors.patchSession },
  });
}

export function useDeleteChatSession() {
  const { t } = useLocale();
  return useMutation<void, Error, string>({
    mutationFn: (sessionId) => getMemaxClient().chats.deleteSession(sessionId),
    onSuccess: (_void, sessionId) => {
      queryClient.removeQueries({ queryKey: sessionDetailKey(sessionId) });
      void queryClient.invalidateQueries({
        queryKey: ["chat", "sessions", "list"],
      });
    },
    meta: { errorMessage: t.chat.errors.deleteSession },
  });
}

// --- Messages: list / get -----------------------------------------

export function useChatMessages(
  sessionId: string | undefined,
  opts?: ListChatMessagesOptions,
) {
  return useQuery<ListChatMessagesResult>({
    queryKey: sessionId
      ? messagesListKey(sessionId, opts)
      : ["chat", "messages", "list", "_"],
    queryFn: ({ signal }) =>
      getMemaxClient().chats.listMessages(sessionId!, { ...opts, signal }),
    enabled: Boolean(sessionId),
    // Messages are append-only at the list level (cancellation /
    // regeneration changes individual rows but doesn't reorder).
    // The SSE stream pushes the authoritative state; lists refresh
    // when the user navigates back to the session.
    staleTime: 30_000,
  });
}

export function useChatMessage(
  sessionId: string | undefined,
  messageId: string | undefined,
) {
  return useQuery<ChatMessage>({
    queryKey:
      sessionId && messageId
        ? ["chat", "messages", "detail", sessionId, messageId]
        : ["chat", "messages", "detail", "_"],
    queryFn: ({ signal }) =>
      getMemaxClient().chats.getMessage(sessionId!, messageId!, { signal }),
    enabled: Boolean(sessionId && messageId),
    staleTime: 30_000,
  });
}

// --- Send / cancel / regenerate -----------------------------------
//
// These mutation hooks take sessionId per-call (not at construction
// time) so the create-and-send flow on the empty state works
// without a stale-closure race: the empty state creates a session,
// then sends — both with the SAME hook instance. Binding sessionId
// at hook-construction was the wrong shape; it forced the brain
// view to either (a) hold a per-mutation hook tree that re-keyed on
// every session switch (expensive + fragile) or (b) defer the send
// across React's render cycle via setTimeout (the v1 race that
// landed the "broken UX" report).

export function useSendChatMessage() {
  const { t } = useLocale();
  return useMutation<
    SendChatMessageResult,
    Error,
    { sessionId: string; input: SendChatMessageInput }
  >({
    mutationFn: ({ sessionId, input }) =>
      getMemaxClient().chats.sendMessage(sessionId, input),
    onSuccess: (_data, { sessionId }) => {
      void queryClient.invalidateQueries({
        queryKey: ["chat", "messages", "list", sessionId],
      });
      void queryClient.invalidateQueries({
        queryKey: sessionDetailKey(sessionId),
      });
      // Also bump the sessions list — last_message_at moved.
      void queryClient.invalidateQueries({
        queryKey: ["chat", "sessions", "list"],
      });
    },
    meta: { errorMessage: t.chat.errors.sendMessage },
  });
}

export function useCancelChatMessage() {
  const { t } = useLocale();
  return useMutation<
    CancelChatMessageResult,
    Error,
    { sessionId: string; messageId: string }
  >({
    mutationFn: ({ sessionId, messageId }) =>
      getMemaxClient().chats.cancelMessage(sessionId, messageId),
    onSuccess: (_data, { sessionId, messageId }) => {
      void queryClient.invalidateQueries({
        queryKey: ["chat", "messages", "list", sessionId],
      });
      void queryClient.invalidateQueries({
        queryKey: ["chat", "messages", "detail", sessionId, messageId],
      });
    },
    meta: { errorMessage: t.chat.errors.cancelMessage },
  });
}

export function useRegenerateChatMessage() {
  const { t } = useLocale();
  return useMutation<
    RegenerateChatMessageResult,
    Error,
    { sessionId: string; priorAssistantId: string }
  >({
    mutationFn: ({ sessionId, priorAssistantId }) =>
      getMemaxClient().chats.regenerateMessage(sessionId, priorAssistantId),
    onSuccess: (_data, { sessionId }) => {
      void queryClient.invalidateQueries({
        queryKey: ["chat", "messages", "list", sessionId],
      });
      void queryClient.invalidateQueries({
        queryKey: sessionDetailKey(sessionId),
      });
    },
    meta: { errorMessage: t.chat.errors.regenerateMessage },
  });
}

// --- Tools catalog ------------------------------------------------

export function useChatTools() {
  return useQuery<ChatToolDescriptor[]>({
    queryKey: toolsKey(),
    queryFn: async ({ signal }) =>
      normalizeChatTools(await getMemaxClient().chats.listTools({ signal })),
    // The catalog is process-wide and rarely changes — long stale
    // window so we don't refetch on every navigation.
    staleTime: 5 * 60_000,
  });
}

function normalizeChatTools(raw: unknown): ChatToolDescriptor[] {
  if (Array.isArray(raw)) {
    return raw;
  }
  if (
    raw &&
    typeof raw === "object" &&
    "tools" in raw &&
    Array.isArray((raw as { tools?: unknown }).tools)
  ) {
    return (raw as { tools: ChatToolDescriptor[] }).tools;
  }
  return [];
}

// --- Approvals ----------------------------------------------------

export function useDecideChatApproval() {
  const { t } = useLocale();
  return useMutation<
    { approval_id: string; status: "approved" | "denied" | "timed_out" },
    Error,
    { sessionId: string; approvalId: string; decision: "approve" | "deny" }
  >({
    mutationFn: ({ sessionId, approvalId, decision }) =>
      getMemaxClient().chats.decideApproval(sessionId, approvalId, decision),
    onSuccess: (_data, { sessionId }) => {
      void queryClient.invalidateQueries({
        queryKey: ["chat", "messages", "list", sessionId],
      });
    },
    meta: { errorMessage: t.chat.errors.decideApproval },
  });
}
