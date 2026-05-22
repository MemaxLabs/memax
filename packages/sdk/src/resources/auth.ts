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
  RequestEmailOtpOptions,
  RequestEmailOtpResponse,
  UnlinkProviderResult,
  UpdateApiKeyPayload,
  UpdateApiKeyResult,
  UpdateProfileResult,
  VerifyEmailOtpOptions,
  VerifyEmailOtpResponse,
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
      case "email":
        // Email OTP does not redirect to a third-party authorize page;
        // the client posts to requestEmailOtp() instead. Returning the
        // login page keeps the symmetry obvious if a caller does call
        // providerLoginURL("email") by mistake.
        return `${this.apiUrl}/login?provider=email`;
    }
  }

  /**
   * Request a 6-digit email sign-in code. The server canonicalizes
   * the email, stores a hashed code, and queues an email through the
   * existing transactional pipeline. The response is uniform
   * regardless of whether the email maps to an existing account —
   * eligibility is enforced at {@link verifyEmailOtp}.
   *
   * Rate-limited per-email and per-IP; expect 429 with
   * `code: "rate_limited"` if the caller crosses the budget.
   */
  async requestEmailOtp(
    options: RequestEmailOtpOptions,
  ): Promise<RequestEmailOtpResponse> {
    return this.req("POST", "/v1/auth/email/request", {
      body: {
        email: options.email,
        redirect_uri: options.redirect_uri,
        invite_token: options.invite_token,
      },
    });
  }

  /**
   * Verify a sign-in code and complete the login. Runs the same
   * registration-gate + invite-consumption path the OAuth callbacks
   * use, so behavior is consistent across all three sign-in surfaces.
   *
   * When the caller supplied a redirect_uri at request time, the
   * response carries an exchange code (mirrors the OAuth callback
   * dance) — bounce through `auth.exchangeCode()` to receive the
   * token pair. Without a redirect, tokens are returned directly.
   */
  async verifyEmailOtp(
    options: VerifyEmailOtpOptions,
  ): Promise<VerifyEmailOtpResponse> {
    return this.req("POST", "/v1/auth/email/verify", {
      body: {
        email: options.email,
        code: options.code,
      },
    });
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
