import type {
  ApiKey,
  ApiKeyCreateOptions,
  ApiKeyListItem,
  ApiKeyRevokeResult,
  AuthIdentity,
  AuthProviderName,
  AuthTokenPair,
  DeviceSignIn,
  DeviceSignInOptions,
  DeviceSignInPoll,
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
import type { FormFn, FormResult, RequestFn } from "../transport.js";
import { MemaxError } from "../errors.js";

/** An OAuth error answer as a MemaxError (`code` is OAuth's `error`). */
function oauthFailure(
  status: number,
  body: Record<string, unknown>,
  retryAfter?: number,
): MemaxError {
  const code =
    status === 429
      ? "rate_limited"
      : typeof body.error === "string"
        ? body.error
        : "invalid_response";
  const message =
    typeof body.error_description === "string"
      ? body.error_description
      : `The sign-in server answered ${status}.`;
  return new MemaxError(message, code, status, undefined, retryAfter);
}

/** The memax CLI's OAuth client id; only it signs in with a device code. */
const DEVICE_CLIENT_ID = "memax-cli";
const DEVICE_GRANT = "urn:ietf:params:oauth:grant-type:device_code";

export class AuthResource {
  constructor(
    private readonly req: RequestFn,
    private readonly apiUrl: string,
    private readonly form?: FormFn,
  ) {}

  /**
   * Ask for a device code (RFC 8628 §3.1), for signing the memax CLI in
   * where no browser can open. Show the person `userCode` and
   * `verificationUri` (or open `verificationUriComplete`), then poll with
   * {@link pollDeviceSignIn} every `interval` seconds until it ends.
   * Throws MemaxError `rate_limited` (too many codes from this address),
   * `invalid_client`, or the network's errors.
   */
  async startDeviceSignIn(
    options: DeviceSignInOptions = {},
  ): Promise<DeviceSignIn> {
    const { status, body, retryAfter } = await this.oauthForm(
      "/oauth/device_authorization",
      {
        client_id: DEVICE_CLIENT_ID,
        client_version: options.clientVersion,
        device_name: options.deviceName,
        device_os: options.deviceOs,
        space: options.space,
      },
      options.signal,
    );
    if (status !== 200 || typeof body.device_code !== "string") {
      throw oauthFailure(status, body, retryAfter);
    }
    return {
      deviceCode: body.device_code,
      userCode: String(body.user_code ?? ""),
      verificationUri: String(body.verification_uri ?? ""),
      verificationUriComplete:
        typeof body.verification_uri_complete === "string"
          ? body.verification_uri_complete
          : undefined,
      expiresIn: Number(body.expires_in ?? 600),
      interval: Math.max(1, Number(body.interval ?? 5)),
    };
  }

  /**
   * Poll once for a device code's session (RFC 8628 §3.4). `pending` and
   * `slow_down` mean keep polling (`slow_down`: 5 seconds longer between
   * polls from now on); `signed_in` carries the person's CLI session, which
   * the server issues once; `denied`, `expired` and `invalid` end it.
   */
  async pollDeviceSignIn(
    deviceCode: string,
    options: { signal?: AbortSignal } = {},
  ): Promise<DeviceSignInPoll> {
    const { status, body, retryAfter } = await this.oauthForm(
      "/oauth/token",
      {
        grant_type: DEVICE_GRANT,
        client_id: DEVICE_CLIENT_ID,
        device_code: deviceCode,
      },
      options.signal,
    );
    if (status === 200 && typeof body.access_token === "string") {
      return {
        status: "signed_in",
        tokens: {
          access_token: body.access_token,
          refresh_token: String(body.refresh_token ?? ""),
          expires_in: Number(body.expires_in ?? 3600),
        },
      };
    }
    switch (body.error) {
      case "authorization_pending":
        return { status: "pending" };
      case "slow_down":
        return { status: "slow_down" };
      case "access_denied":
        return { status: "denied" };
      case "expired_token":
        return { status: "expired" };
      case "invalid_grant":
        return { status: "invalid" };
    }
    throw oauthFailure(status, body, retryAfter);
  }

  private async oauthForm(
    path: string,
    fields: Record<string, string | undefined>,
    signal?: AbortSignal,
  ): Promise<FormResult> {
    if (!this.form) {
      throw new MemaxError(
        "This client can't send OAuth forms.",
        "invalid_request",
        0,
      );
    }
    return this.form(path, fields, signal);
  }

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
        standalone: options.standalone,
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
