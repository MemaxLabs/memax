## Debug Session: OAuth Redirect Loop on Staging (2026-04-10)

Spent 2 hours debugging an infinite redirect loop on staging after deploying the MCP OAuth approval flow. Users would click "Login with GitHub" and get bounced between `/auth/github/callback` and `/auth/github` indefinitely, never landing on the dashboard.

### Symptoms

- Browser shows rapid URL changes in the address bar
- Network tab shows 302 → 302 → 302 chain (20+ redirects before Chrome gives up)
- Cookie `memax_session` is being set but immediately cleared on the next redirect
- Only on staging — local dev works fine

### Investigation Steps

**Step 1: Check callback URL registration**

```bash
$ curl -sI "https://staging-api.memaxlabs.com/auth/github/callback" \
  -H "Cookie: memax_session=test" | head -5

HTTP/2 302
location: /auth/github
set-cookie: memax_session=; Max-Age=0; Path=/; HttpOnly; Secure; SameSite=Lax
```

The callback is clearing the session cookie and redirecting back to `/auth/github`. That's the loop.

**Step 2: Check why the callback clears the session**

```bash
$ curl -sv "https://staging-api.memaxlabs.com/auth/github/callback?code=test_code&state=abc123" 2>&1 | grep -E "(< HTTP|< location|< set-cookie)"

< HTTP/2 302
< location: /auth/github
< set-cookie: memax_session=; Max-Age=0; Path=/; HttpOnly; Secure; SameSite=Lax
```

The callback handler is failing silently and redirecting to login instead of showing an error.

**Step 3: Check server logs**

```bash
$ fly logs -c fly.server.toml --region sjc | grep "auth/github/callback"

2026-04-10T14:23:17Z [info] handler=auth.callback error="oauth state mismatch: expected 'https://staging.memax.app/auth/callback' got 'https://staging.memax.app/auth/callback/'"
```

Found it. Trailing slash mismatch in the OAuth state parameter.

### Root Cause

The `OAUTH_GITHUB_REDIRECT_URL` env var on staging was set to `https://staging.memax.app/auth/callback/` (with trailing slash), but the OAuth state validation in `auth.go` compared it against the request's `redirect_uri` parameter which Next.js sends WITHOUT a trailing slash.

```go
// internal/handler/auth.go:187
func (h *AuthHandler) validateOAuthState(state, expectedRedirect string) error {
    // ...
    if decoded.RedirectURI != expectedRedirect {  // strict string comparison
        return fmt.Errorf("oauth state mismatch: expected '%s' got '%s'",
            expectedRedirect, decoded.RedirectURI)
    }
    return nil
}
```

### Fix

Two-part fix:

1. **Normalize trailing slashes** in the redirect URL comparison:

```go
func normalizeURL(u string) string {
    return strings.TrimRight(u, "/")
}

func (h *AuthHandler) validateOAuthState(state, expectedRedirect string) error {
    // ...
    if normalizeURL(decoded.RedirectURI) != normalizeURL(expectedRedirect) {
        return fmt.Errorf("oauth state mismatch: expected '%s' got '%s'",
            expectedRedirect, decoded.RedirectURI)
    }
    return nil
}
```

2. **Fixed the staging env var** to remove the trailing slash:

```bash
fly secrets set OAUTH_GITHUB_REDIRECT_URL="https://staging.memax.app/auth/callback" \
  -c fly.server.toml
```

### Lesson Learned

- Always normalize URLs before comparison — trailing slashes, scheme, port differences are common
- OAuth state validation failures should return a 400 error page, not silently redirect to login (which creates the loop)
- Add integration tests that verify the full OAuth round-trip with exact URLs, not just mocked state
