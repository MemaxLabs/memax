## Debugging: Login Session Expiry Issue

Found a bug where users were getting logged out unexpectedly after 15 minutes on staging. Root cause: the session cookie `max-age` was set to 900 seconds (15 min) instead of 86400 (24 hours). This was a typo in the auth middleware configuration.

Fix applied in `packages/server/internal/handler/auth.go`:
- Changed `MaxAge: 900` to `MaxAge: 86400`
- Also fixed the `SameSite` attribute from `Strict` to `Lax` to allow OAuth redirect flows

The login redirect was working correctly — the issue was purely session duration. Users would complete OAuth, get a valid session, but then lose it after 15 minutes and see the login page again.

Verified fix on staging by monitoring session cookies in browser DevTools. Sessions now persist for 24 hours as expected.
