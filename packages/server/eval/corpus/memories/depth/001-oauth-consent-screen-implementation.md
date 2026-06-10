Implementation notes for the OAuth consent screen, built during the week of March 25.

## Hosting Decision

We host the consent screen as a Next.js page under `/oauth/consent` on the Vercel-deployed web app rather than rendering it from the Go server. This keeps OAuth server-side logic in Go (token issuance, code exchange) while the UX lives in the React codebase where we have the design system. The Go server issues a short-lived consent challenge token and redirects the browser to the web app with the token as a query parameter.

## CSRF Validation

Each consent form submission includes a `csrf_token` tied to the challenge token. The Go server validates both the CSRF token and the consent challenge on the approve/deny POST. We chose double-submit cookie + form field over the Synchronizer Token Pattern because the consent page is stateless from the server's perspective.

## Implementation Steps

1. Go server generates consent challenge (random 32-byte hex), stores in Redis with 10-minute TTL.
2. Browser redirects to `memax.app/oauth/consent?challenge=<token>&client_id=<id>`.
3. Next.js page fetches client metadata from `/v1/oauth/clients/:id` to display app name, icon, requested scopes.
4. User clicks Approve or Deny. Form POSTs to Go server at `/oauth/consent/decision`.
5. Go server validates challenge, CSRF, and consent decision. On approve, issues auth code and redirects to client callback.

## Scope Display

Scopes are grouped into categories: Read (recall, list, search), Write (push, forget), and Admin (hub management). Each scope shows a human-readable description. The "remember this device" checkbox stores consent in a signed cookie valid for 30 days.

## Lessons Learned

- Consent challenge tokens must be single-use. We had a bug where replaying the consent form would issue multiple auth codes.
- The redirect back to the client must preserve the original `state` parameter exactly. URL-encoding differences between Go and JavaScript caused a subtle mismatch that broke Cursor's OAuth flow.
- Loading the client icon from an external URL required adding CSP exceptions for trusted domains.
