## Authentication Architecture — Token Lifecycle

Our auth system uses a three-layer token model:

1. **OAuth2 grants** -- GitHub and Google providers issue authorization codes, exchanged for access + refresh tokens server-side.
2. **JWT access tokens** -- Short-lived (1 hour), signed with RS256. Claims include user_id, hub_memberships[], and scopes[]. Verified on every API request.
3. **API keys** -- Long-lived bearer tokens for CI/CD and agent integrations. Stored as bcrypt hashes in the `api_keys` table. Support scope restrictions (read-only, write, admin).

Token refresh flow: When the JWT expires, the SDK automatically uses the refresh token (30-day TTL) to obtain a new access token without re-prompting the user. If the refresh token is also expired, the user must re-authenticate via OAuth.

Key implementation detail: The auth middleware extracts hub_memberships from the JWT claims and injects them into the request context. Store methods then filter queries by these hub IDs — this is how we enforce hub-level access control without hitting the database on every request.

Rate limiting for auth endpoints: Login attempts are capped at 10/minute per IP to prevent brute force. Failed attempts trigger exponential backoff.
