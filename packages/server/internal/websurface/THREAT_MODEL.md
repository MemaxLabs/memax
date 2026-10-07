# Web-surface assurance: threat model

`human_web` is the assurance a person's Keep carries when Memax can tell
the person was on the web app, not an agent using their login. Plan 25
requires it for keeping quarantined (external) proposals, for keeping
decisions in team spaces (D15), and this work adds it for raising what an
agent may do, confirming a device's sign-in code and signing out another
session. Everything else a person does through the CLI or an agent is
recorded as `client_attested`.

## How the API tells

Both of these must hold. Neither is enough alone.

1. **The session was issued to the web app.** When a login completes, the
   API records its surface from where the one-time code goes: the web
   app's origin (`APP_BASE_URL`) is `web`; a loopback redirect
   (`memax login`) or a token returned in the response is `cli`
   (migration 030). A device-code sign-in is `cli` too. Every access token
   the session mints, refreshes included, carries the surface as a signed
   claim, and names its session (`sid`, migration 049).
2. **The web app's proxy signed the request.** `/api/proxy` signs each
   `/v2` request for a `web` session with `WEB_SURFACE_SECRET`, which only
   the web deployment and the API hold: HMAC-SHA256 over the method, path
   and query, a timestamp, a nonce, the user id, `Idempotency-Key`,
   `If-Match` and the body's SHA-256. The API refuses a signature that is
   wrong, more than 60 seconds off, for another user, or already seen
   (constant-time comparison; nonces cached per process).

A request with no signature is `client_attested`. A request with a
signature that doesn't verify is refused (403 `surface_unverified`).

## Where the web session lives (the BFF)

The web app's server is the only holder of the web session (a
backend-for-frontend, `packages/web/src/lib/bff`):

- **Cookies only the server reads.** `/api/auth/exchange` trades the
  login's one-time code for the session and stores the access token and
  the refresh token in cookies named `__Host-memax_session` and
  `__Host-memax_refresh`: `HttpOnly`, `Secure`, `SameSite=Strict`,
  `Path=/`, no `Domain`. The `__Host-` prefix makes them host-only, so a
  sibling subdomain can't set or shadow them. The answer to the page says
  only that the sign-in worked. Over plain http (local development and the
  Playwright and workerd runs on localhost) the names drop the prefix and
  the cookies drop `Secure`; each mode reads only its own names.
- **The proxy attaches the token.** `/api/proxy` reads the access token
  from its cookie, never from the page (an `Authorization` header the page
  sends is dropped), refreshes it when it is missing or within 30 seconds
  of expiring (and once more if the API refuses it), and sets the rotated
  cookies on its response. Refreshes are single-flight per refresh token
  in each server process; refreshes that race across processes (Workers
  isolates) get the same next token from the API's grace window (below).
- **No token reaches page script.** The proxy refuses the API's token
  endpoints (`/v1/auth/refresh|exchange|impersonate`, `/oauth/token`,
  `/oauth/revoke`, `/oauth/device_authorization`, `/oauth/register`), and
  if the email code's verify answers tokens (a code requested without a
  redirect, the CLI's way) it signs them out and answers an error. The
  page learns the session's surface from `/api/auth/me`. Tokens an older
  version kept in `localStorage` are deleted unread.
- **CSRF.** Two layers, either enough alone against another site: the
  session cookies are `SameSite=Strict`, so a browser never sends them
  with a request another site starts; and every cookie-backed route checks
  Fetch Metadata: a state-changing request must carry
  `Sec-Fetch-Site: same-origin` (or, from a browser too old to send it, an
  `Origin` that is ours), and requests another site or a sibling subdomain
  starts are refused, reads included (403 `csrf_refused`). A double-submit
  token was not chosen: it needs every fetch in both web trees to carry
  it and adds nothing against another site that these two don't stop.
- **Sign-out** revokes the session on the API (`POST /oauth/revoke` with
  its refresh token, so every copy stops working) and clears every cookie,
  including an operator's own session kept aside while they impersonate
  someone. Impersonation never puts a token in reach of the page either.
- **Where the browser is.** Sign-ins and refreshes tell the API the
  browser's address, city and user agent in `X-Memax-Client-*` headers
  signed with `WEB_SURFACE_SECRET`, for the sessions list. The API ignores
  them unsigned, and they never decide what a request may do.

The API's side of `human_web` doesn't change: the proxy keeps signing, and
the `cli` surface keeps the CLI out.

## Refresh tokens

Every session's refresh token (web, CLI, device code, MCP OAuth) is 256
random bits, stored only as its SHA-256 (`internal/sessions`). A copy of
the database holds no token anyone can present. Each refresh retires the
token it was given and issues the next one; a session is a refresh-token
family. A retired token presented again within 60 seconds of its
retirement yields the session's current token (sealed under a key derived
from the retired token and wiped once the window passes), so tabs and
processes that refreshed together end up with the same token. After the
window it revokes the whole session: two clients hold it, and one of them
shouldn't. A person lists their sessions and signs any of them out
(`/v2/sessions`); signing one out ends its refresh token at once and its
last access token within the hour. Signing out any session but the one in
hand needs `human_web`, so an agent with the CLI login can't sign the
person out of the web.

## Why the proxy's signature alone isn't enough

The proxy is a public endpoint. A local agent holding the person's CLI
token could put it in a cookie and send it through
`https://memax.app/api/proxy/v2/...`. The token's `cli` surface is what
stops that: the proxy doesn't sign it, and the API wouldn't accept it as
the web if it did. Conversely, a `web` token without the proxy's
signature (used directly against the API) is `client_attested`.

## Attacks this defeats

| Attacker | Result |
| --- | --- |
| A local agent with the person's CLI token, calling the API | No signature: `client_attested` |
| The same agent, sending the CLI token through the web proxy | Not signed (`cli` session): `client_attested` |
| Script injected into memax.app (XSS) reading the session | Nothing to read: the tokens are `HttpOnly`, and no answer the page gets carries one |
| Another site making the person's browser call the proxy (CSRF) | No cookies go (`SameSite=Strict`), and Fetch Metadata refuses it |
| A sibling subdomain setting or shadowing the session cookie | `__Host-` cookies are host-only; plain-named cookies aren't read over https |
| A stolen refresh token, used after its owner refreshed | The session is revoked (reuse detection); both copies stop working |
| A copy of the database | Refresh tokens are hashes; the sealed successors open only with a token, and are wiped after a minute |
| Forging `X-Memax-Surface-*` or `X-Memax-Client-*` headers | Refused or ignored: they need `WEB_SURFACE_SECRET`, and the proxy drops a client's own |
| `X-Memax-Via: web` | Rejected (400): clients may only declare `api`, `cli`, `mcp` |
| Replaying a captured signed request | Refused within the window on the same machine; on another machine it repeats the same Idempotency-Key, so it writes nothing new |
| Changing the method, path, query, body, user, key or If-Match of a signed request | Signature no longer verifies |

## Residual risks

- **Browser XSS on memax.app** can still act through the proxy as the
  person while the page is open. It can no longer take the tokens away and
  replay them later from elsewhere.
- **A local agent with OS-level access** can read the browser's cookie
  store on disk. Browsers encrypt cookies at rest (with the OS keychain on
  macOS and Windows; on Linux the protection is often weaker), which
  raises the bar over the plain-text `localStorage` of before, but an
  agent running as the person can usually decrypt them. With the cookies,
  a non-browser client can send them to the proxy with a matching
  `Origin` (Fetch Metadata and `SameSite` are enforced by browsers, not
  servers) and act as the person on the web. The next step, a passkey
  re-check at Keep time, is what defeats this.
- **An agent driving the person's real browser** (computer use, a browser
  automation server attached to their profile) looks exactly like the
  person. Only a user-verification step at Keep time (a passkey with UV)
  can tell them apart.
- **Someone who can sign in as the person** (their GitHub account, their
  inbox for the email code) gets a web session of their own. They show up
  in the person's sessions list, and can be signed out there.
- **A refresh token stolen and used first, within the grace window,**
  shares the session with its owner until their refreshes diverge by more
  than a minute; then reuse detection revokes it.
- **Access tokens are stateless.** A signed-out or revoked session's last
  access token works until it expires, within the hour.
- **Sessions from the `localStorage` era** keep working server-side until
  they expire (at most 30 days after their sign-in) unless signed out; the
  web app deletes their tokens from the browser unread, and the person can
  sign them out from the sessions list. An operator can end them all at
  once after the cutover with
  `UPDATE sessions SET revoked_at = now(), revoked_reason = 'revoked' WHERE kind = 'web' AND created_at < '<cutover>' AND revoked_at IS NULL`.
- **Plain-text refresh tokens from before migration 049** survive in dead
  tuples until autovacuum, and in WAL and backups for their retention.
- **A leaked `WEB_SURFACE_SECRET`** lets the holder sign, but they still
  need a `web` token for the user. Rotate it on both sides. On
  Cloudflare Workers it is a Worker secret, which CI uploads from Doppler
  with each version and nobody can read back; it must never sit in
  `packages/web/.env*`, because OpenNext copies those files into the
  Worker's bundle. The browser reaches the Worker, which calls the API
  over HTTPS like any client, and forwards only the allowlisted headers
  (it drops any `X-Memax-Surface-*` or `X-Memax-Client-*` a client sends;
  Cloudflare adds `CF-Worker`, which the API ignores).
- **Rate limits** on the token endpoints are per address, and every web
  refresh comes from the web deployment's address. The signed client
  address could key them instead; not done yet.
- **Sessions from before migration 030** have no surface and stay
  `client_attested` until the person signs in again.

## Next: a passkey re-check at Keep

With the session only in the proxy, the strong guarantee for D15 and
quarantine is a WebAuthn user-verification step-up on those Keeps. It can
build on what is here: `/api/proxy` is the one place every web request
passes (it can carry a fresh assertion beside the signature), the access
token's `sid` names the session a re-check belongs to, and
`policy.Decide*` already asks for `human_web` in the places a re-check
should be required. The API could then also refuse a `web` token presented
without the proxy's signature; today that would gain little, because
anyone holding the cookies can go through the proxy.
