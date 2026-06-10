## Claude Code Session: Auth Refactor Planning

Session captured: April 6, 2026
Agent: Claude Code (claude-opus-4-20250514)
User: Ziyang
Duration: ~1.5 hours
Branch: consent-redesign

### Session Summary

This was a planning session where I worked with Claude Code to design the new OAuth consent screen and refactor the auth grant flow. The main goal was to align the consent screen with our design language (liquid glass, spring easing) while improving the security UX.

### Key Discussion Points

**1. Current consent screen problems:**
- The existing consent screen is a plain HTML page served by the Go backend, with zero design system integration
- Users can't distinguish between scope levels (read vs write vs admin)
- No visual indicator of which hubs will be accessible
- Mobile layout is broken -- the scope list overflows

**2. Proposed architecture change:**
We decided to move the consent screen from the Go server to the Next.js web app. Reasons:
- Can reuse the @memaxlabs/ui design system directly
- Better i18n support (important for our Chinese-speaking users)
- Faster iteration on UX without redeploying the Go server

The Go server will still handle the OAuth protocol (authorization code exchange, token issuance) but will redirect to the web app for the consent UI. Flow:

```
Agent -> GET /oauth/authorize -> 302 to memax.app/consent?request_id=xxx
User approves -> POST /oauth/approve (goes to Go server)
Go server -> 302 back to agent with authorization code
```

**3. Consent screen design decisions:**
- Use a centered glass modal (not full-page) with spring entrance animation
- Show requested scopes as a categorized list: "Read memories", "Push memories", "Manage hubs"
- Each scope shows affected hubs with their icons
- Add a "trust level" indicator: "First time" (amber), "Previously approved" (green)
- Include the agent name and icon (e.g., Claude Code logo)

**4. Security considerations discussed:**
- Add PKCE (Proof Key for Code Exchange) for public clients
- Consent decisions should be persisted so users don't re-approve the same agent+scopes
- Add a "revoke all grants" button in settings
- Rate limit consent approvals to prevent abuse

### Action Items

- [ ] Create the consent page component in packages/web/src/app/consent/
- [ ] Add the /oauth/approve endpoint in the Go server that accepts the web app's POST
- [ ] Implement PKCE flow for MCP OAuth
- [ ] Design the scope display component with hub icons
- [ ] Add i18n strings for consent screen (EN + ZH)
- [ ] Write integration tests for the full OAuth flow

### Claude's Suggested File Structure

```
packages/web/src/app/consent/
  page.tsx          -- main consent page (server component for initial data)
  consent-form.tsx  -- client component with approve/deny buttons
  scope-list.tsx    -- scope display with categorized permissions
  trust-badge.tsx   -- first-time vs returning agent indicator
```

### My Notes

Good session. The decision to move consent to the web app feels right -- the Go server shouldn't be in the business of rendering HTML with our design system. The PKCE implementation will be the hardest part since we need to coordinate between the Go server and the web app. Should pair with Jiahao on that.
