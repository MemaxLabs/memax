# Web-surface assurance: threat model

`human_web` is the assurance a person's Keep carries when Memax can tell
the person was on the web app, not an agent using their login. Plan 25
requires it for keeping quarantined (external) proposals, for keeping
decisions in team spaces (D15), and this work adds it for raising what an
agent may do. Everything else a person does through the CLI or an agent is
recorded as `client_attested`.

## How the API tells

Both of these must hold. Neither is enough alone.

1. **The session was issued to the web app.** When a login completes, the
   API records its surface from where the one-time code goes: the web
   app's origin (`APP_BASE_URL`) is `web`; a loopback redirect
   (`memax login`) or a token returned in the response is `cli`
   (migration 030). Every access token the session mints, refreshes
   included, carries the surface as a signed claim.
2. **The web app's proxy signed the request.** `/api/proxy` signs each
   `/v2` request for a `web` session with `WEB_SURFACE_SECRET`, which only
   the web deployment and the API hold: HMAC-SHA256 over the method, path
   and query, a timestamp, a nonce, the user id, `Idempotency-Key`,
   `If-Match` and the body's SHA-256. The API refuses a signature that is
   wrong, more than 60 seconds off, for another user, or already seen
   (constant-time comparison; nonces cached per process).

A request with no signature is `client_attested`. A request with a
signature that doesn't verify is refused (403 `surface_unverified`).

## Why the proxy's signature alone isn't enough

The proxy is a public endpoint that forwards whatever bearer token it is
given. A local agent holding the person's CLI token could send it through
`https://memax.app/api/proxy/v2/...` and get a valid signature. The token's
`cli` surface is what stops that: the proxy doesn't sign it, and the API
wouldn't accept it as the web if it did. Conversely, a `web` token without
the proxy's signature (used directly against the API) is `client_attested`,
so a token lifted from the browser can't be used from a script against the
API either; it has to go through the proxy.

## Attacks this defeats

| Attacker | Result |
| --- | --- |
| A local agent with the person's CLI token, calling the API | No signature: `client_attested` |
| The same agent, sending the CLI token through the web proxy | Not signed (`cli` session): `client_attested` |
| Forging `X-Memax-Surface-*` headers | Refused: needs `WEB_SURFACE_SECRET` |
| `X-Memax-Via: web` | Rejected (400): clients may only declare `api`, `cli`, `mcp` |
| Replaying a captured signed request | Refused within the window on the same machine; on another machine it repeats the same Idempotency-Key, so it writes nothing new |
| Changing the method, path, query, body, user, key or If-Match of a signed request | Signature no longer verifies |

## Residual risks

- **Browser XSS on memax.app.** Script in the page can act through the
  proxy as the person. Today it can also read the session tokens from
  `localStorage` and replay them through the proxy from anywhere for the
  refresh token's life (30 days).
- **Reading the browser's storage on disk.** `localStorage` is a plaintext
  LevelDB in the browser profile. An agent with the person's shell can lift
  a `web` token and send it through the proxy.
- **An agent driving the person's real browser** (computer use, a browser
  automation server attached to their profile) looks exactly like the
  person. Only a user-verification step at Keep time (a passkey with UV)
  can tell them apart.
- **Someone who can sign in as the person** (their GitHub account, their
  inbox for the email code) gets a web session of their own.
- **A leaked `WEB_SURFACE_SECRET`** lets the holder sign, but they still
  need a `web` token for the user. Rotate it on both sides.
- **Sessions from before migration 030** have no surface and stay
  `client_attested` until the person signs in again.

## Recommended follow-up: httpOnly cookies (a BFF)

Move the web session out of `localStorage` into an httpOnly, Secure,
SameSite cookie that only the Next.js server reads, and have `/api/proxy`
attach the bearer token itself. Page script never sees a token, which ends
token theft by XSS (script can still act through the proxy while the page
is open), and browsers encrypt cookies at rest, which raises the bar for a
local agent reading the disk. The API side doesn't change: the proxy keeps
signing, and the `cli` surface keeps the CLI out. With the token only in
the proxy, the API could then also refuse a `web` token presented without
a signature.

After that, the strong guarantee for D15 and quarantine is a WebAuthn
user-verification step-up on those Keeps.
