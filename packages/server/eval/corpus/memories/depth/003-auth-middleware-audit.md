Security audit notes for the authentication middleware layer. Reviewed April 9, 2026.

## Audit Scope

Reviewed all middleware in `packages/server/internal/middleware/` plus the auth handler at `packages/server/internal/handler/auth.go`. Focus areas: token validation, permission checking, and boundary enforcement.

## Findings

### Positive

1. **Deny-by-default:** The middleware chain requires explicit `AllowUnauthenticated()` opt-in for public routes. All other routes return 401 if no valid token is present. This is correct.

2. **Token validation:** JWT signature verification uses RS256 with key rotation support. Expired tokens are rejected. The `aud` claim is checked against the expected audience. No issues found.

3. **API key hashing:** Keys are SHA-256 hashed before database lookup. Timing-safe comparison is used via `crypto/subtle.ConstantTimeCompare`. Correct.

### Issues Found

4. **Account-level vs hub-level permissions were conflated.** The `RequirePermission("write")` middleware checked the user's account-level write permission but did not verify they had write access to the specific hub being written to. A user with account-level write but only "viewer" role in a hub could push memories to that hub. **Fix:** Split into `RequireAccountPermission()` and `RequireHubPermission()`. The hub permission check reads the user's role from the `hub_members` table.

5. **Missing rate limit on token refresh.** The `/v1/auth/refresh` endpoint had no rate limiting. An attacker with a stolen refresh token could generate unlimited access tokens. **Fix:** Added per-user rate limit of 10 refreshes per minute via Redis sliding window.

6. **Overly broad CORS in staging.** The staging server had `Access-Control-Allow-Origin: *` which would allow any website to make authenticated requests if credentials were included. **Fix:** Restricted to `*.memaxlabs.com` origins.

## Action Items

- [x] Split account/hub permission middleware (Mira, done April 10)
- [x] Add refresh endpoint rate limit (Jiahao, done April 10)
- [ ] Restrict staging CORS (Ziyang, scheduled April 14)
- [ ] Add automated test for hub boundary enforcement (Sarah, scheduled April 15)

## Recommendation

The permission model is sound at the design level. The implementation gap was in the middleware not distinguishing between account-level and hub-level roles. With the fix shipped, the system correctly enforces that a user's hub role determines what they can do within that hub, independent of their account-level permissions.
