// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useChatStream } from "./use-chat-stream";

const mocks = vi.hoisted(() => ({
  abort: vi.fn(),
  streamMessage: vi.fn(),
  latestOptions: undefined as
    | {
        lastEventId?: string | number;
        onEvent: (event: string, data: unknown) => void;
        onClose?: () => void;
      }
    | undefined,
}));

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    chats: {
      streamMessage: mocks.streamMessage,
    },
  }),
}));

describe("useChatStream", () => {
  beforeEach(() => {
    mocks.abort.mockReset();
    mocks.latestOptions = undefined;
    mocks.streamMessage.mockReset();
    mocks.streamMessage.mockImplementation(
      (
        _sessionId: string,
        _messageId: string,
        options: NonNullable<typeof mocks.latestOptions>,
      ) => {
        mocks.latestOptions = options;
        return { abort: mocks.abort } as unknown as AbortController;
      },
    );
  });

  it("renders server model.delta text as it streams", async () => {
    const { result, unmount } = renderHook(() =>
      useChatStream({ sessionId: "sess_1", messageId: "msg_1", maxRetries: 0 }),
    );

    await waitFor(() => expect(mocks.streamMessage).toHaveBeenCalledTimes(1));

    act(() => {
      mocks.latestOptions?.onEvent("model.delta", { seq: 1, text: "Hello" });
      mocks.latestOptions?.onEvent("model.delta", { seq: 2, text: " there" });
    });

    expect(result.current.text).toBe("Hello there");
    expect(result.current.connected).toBe(true);
    expect(result.current.lastEventId).toBe(2);

    unmount();
    expect(mocks.abort).toHaveBeenCalledTimes(1);
  });

  it("normalizes live tool calls and results from the public stream vocabulary", async () => {
    const { result } = renderHook(() =>
      useChatStream({ sessionId: "sess_1", messageId: "msg_1", maxRetries: 0 }),
    );

    await waitFor(() => expect(mocks.streamMessage).toHaveBeenCalledTimes(1));

    act(() => {
      mocks.latestOptions?.onEvent("tool.call", {
        seq: 1,
        tool_use_id: "tool_1",
        name: "recall_memories",
        input: { query: "chat stream" },
      });
    });

    expect(result.current.toolCalls).toEqual([
      {
        id: "tool_1",
        name: "recall_memories",
        args: { query: "chat stream" },
      },
    ]);

    act(() => {
      mocks.latestOptions?.onEvent("tool.result", {
        seq: 2,
        tool_use_id: "tool_1",
        content: "3 memories",
        is_error: false,
      });
    });

    expect(result.current.toolCalls).toEqual([
      {
        id: "tool_1",
        name: "recall_memories",
        args: { query: "chat stream" },
        result: "3 memories",
        is_error: false,
      },
    ]);
    expect(result.current.lastEventId).toBe(2);
  });

  it("normalizes approval.required payloads and removes them on approval.decided", async () => {
    const { result } = renderHook(() =>
      useChatStream({ sessionId: "sess_1", messageId: "msg_1", maxRetries: 0 }),
    );

    await waitFor(() => expect(mocks.streamMessage).toHaveBeenCalledTimes(1));

    act(() => {
      mocks.latestOptions?.onEvent("approval.required", {
        seq: 1,
        approval_id: "approval_1",
        tool: "push_memory",
        args: { content: "Remember this" },
        expires_at: "2026-04-29T00:00:00Z",
      });
    });

    expect(result.current.approvals).toEqual([
      {
        approval_id: "approval_1",
        tool_name: "push_memory",
        args: { content: "Remember this" },
        expires_at: "2026-04-29T00:00:00Z",
      },
    ]);

    act(() => {
      mocks.latestOptions?.onEvent("approval.decided", {
        seq: 2,
        approval_id: "approval_1",
        status: "approved",
      });
    });

    expect(result.current.approvals).toEqual([]);
    expect(result.current.lastEventId).toBe(2);
  });
});
