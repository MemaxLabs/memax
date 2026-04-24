import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  accessToken: "user-token",
  refreshToken: "refresh-token",
  expiresAt: undefined as number | undefined,
  localAgentKeys: {} as Record<string, string>,
}));

vi.mock("./config.js", () => ({
  loadConfig: () => ({ api_url: "http://localhost:8080" }),
}));

vi.mock("./credentials.js", () => ({
  loadCredentials: () => ({
    access_token: state.accessToken,
    refresh_token: state.refreshToken,
    expires_at: state.expiresAt,
    local_agent_keys: state.localAgentKeys,
  }),
  saveCredentials: vi.fn(),
  isTokenExpired: () => false,
  getLocalAgentKey: (agentID: string) => state.localAgentKeys[agentID],
}));

import { getAuthHeaders, resetClient, setClientAgent } from "./client.js";

describe("client auth selection", () => {
  beforeEach(() => {
    state.accessToken = "user-token";
    state.refreshToken = "refresh-token";
    state.expiresAt = undefined;
    state.localAgentKeys = {};
    setClientAgent(undefined);
    resetClient();
  });

  it("prefers a stored local agent key when an agent-scoped CLI flow is active", async () => {
    state.localAgentKeys["claude-code"] = "mxk_agent_key";

    setClientAgent("claude-code");
    const headers = await getAuthHeaders();

    expect(headers.Authorization).toBe("Bearer mxk_agent_key");
  });

  it("falls back to the user token when no local agent key exists", async () => {
    setClientAgent("claude-code");
    const headers = await getAuthHeaders();

    expect(headers.Authorization).toBe("Bearer user-token");
  });
});
