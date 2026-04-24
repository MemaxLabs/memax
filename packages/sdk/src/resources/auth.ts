import type {
  ApiKey,
  ApiKeyCreateOptions,
  ApiKeyListItem,
  ApiKeyRevokeResult,
  AuthIdentity,
  AuthProviderName,
  AuthTokenPair,
  ImpersonationResult,
  MeResponse,
  OAuthConsentRequest,
  UnlinkProviderResult,
  UpdateApiKeyPayload,
  UpdateApiKeyResult,
  UpdateProfileResult,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class AuthResource {
  constructor(
    private readonly req: RequestFn,
    private readonly apiUrl: string,
  ) {}

  async me(): Promise<MeResponse> {
    return this.req("GET", "/v1/auth/me");
  }

  async createKey(options: ApiKeyCreateOptions): Promise<ApiKey> {
    return this.req("POST", "/v1/auth/api-keys", {
      body: {
        name: options.name,
        hub_id: options.hubId,
        hub_ids: options.hubIds,
        agent_name: options.agentName,
        expires_in_days: options.expiresInDays,
        scopes: options.scopes,
        permissions: options.permissions,
        trust_level: options.trustLevel,
      },
    });
  }

  async listKeys(): Promise<ApiKeyListItem[]> {
    return this.req("GET", "/v1/auth/api-keys");
  }

  /**
   * Revoke an API key. Returns a structured `ApiKeyRevokeResult`
   * with a `skipped` array carrying per-reason skip entries.
   * Partial-success shape mirrors `memories.batchDelete`,
   * `configs.batchDelete`, and `agents.disconnect`.
   *
   * Response is normalized at the SDK boundary so `skipped` is
   * always a real array and `revoked` is always a boolean, even
   * when the server response omits fields.
   */
  async revokeKey(id: string): Promise<ApiKeyRevokeResult> {
    const raw = await this.req<Partial<ApiKeyRevokeResult>>(
      "DELETE",
      `/v1/auth/api-keys/${id}`,
    );
    return {
      revoked: raw?.revoked ?? false,
      skipped: raw?.skipped ?? [],
    };
  }

  /**
   * Patch API key metadata (attribution + standalone flag). Use this to
   * assign an agent to an unassigned key or mark a script key as
   * standalone so it stops surfacing the Assign affordance.
   *
   * Pass `agent_name: ""` to clear an existing assignment.
   */
  async updateKey(
    id: string,
    payload: UpdateApiKeyPayload,
  ): Promise<UpdateApiKeyResult> {
    return this.req("PATCH", `/v1/auth/api-keys/${id}`, {
      body: payload,
    });
  }

  async updateProfile(displayName: string): Promise<UpdateProfileResult> {
    return this.req("PATCH", "/v1/auth/me", {
      body: { display_name: displayName },
    });
  }

  async refresh(refreshToken: string): Promise<AuthTokenPair> {
    return this.req("POST", "/v1/auth/refresh", {
      body: { refresh_token: refreshToken },
    });
  }

  async exchangeCode(code: string): Promise<AuthTokenPair> {
    return this.req("POST", "/v1/auth/exchange", {
      body: { code },
    });
  }

  async getOAuthConsentRequest(
    requestId: string,
    consentToken: string,
  ): Promise<OAuthConsentRequest> {
    return this.req("GET", "/oauth/authorize/consent-request", {
      query: {
        request_id: requestId,
        consent_token: consentToken,
      },
    });
  }

  githubLoginURL(redirectURI: string): string {
    return `${this.apiUrl}/v1/auth/github?redirect_uri=${encodeURIComponent(redirectURI)}`;
  }

  googleLoginURL(redirectURI: string): string {
    return `${this.apiUrl}/v1/auth/google?redirect_uri=${encodeURIComponent(redirectURI)}`;
  }

  providerLoginURL(provider: AuthProviderName, redirectURI: string): string {
    switch (provider) {
      case "github":
        return this.githubLoginURL(redirectURI);
      case "google":
        return this.googleLoginURL(redirectURI);
    }
  }

  linkProviderURL(provider: AuthProviderName, redirectURI: string): string {
    return `${this.apiUrl}/v1/auth/link/${provider}?redirect_uri=${encodeURIComponent(redirectURI)}`;
  }

  async listIdentities(): Promise<AuthIdentity[]> {
    return this.req("GET", "/v1/auth/identities");
  }

  async unlinkProvider(
    provider: AuthProviderName,
  ): Promise<UnlinkProviderResult> {
    return this.req("DELETE", `/v1/auth/link/${provider}`);
  }

  /** Impersonate another user (requires dev_access). Returns a short-lived access-only token. */
  async impersonate(target: {
    userId?: string;
    email?: string;
  }): Promise<ImpersonationResult> {
    return this.req("POST", "/v1/auth/impersonate", {
      body: { user_id: target.userId, email: target.email },
    });
  }
}
