# Web-surface assurance: threat model

`human_web` is the assurance a person's Keep carries when Memax can tell
the person was on the web app, not an agent using their login. Plan 25
requires it for keeping quarantined (external) proposals, for keeping
decisions in team spaces (D15), and this work adds it for raising what an
agent may do, confirming a device's sign-in code and signing out another
session. Everything else a person does through the CLI or an agent is
recorded as `client_attested`.

`human_web_verified` is one step above: the person also answered a passkey
re-check (WebAuthn, user verification required) bound to that very
request. A person with a passkey makes the decisions that need them with
it; see "The passkey re-check" below. The ladder is `client_attested` <
`human_web` < `human_web_verified` (`policy.Assurance`), and receipts,
gate answers, Activity and the export say which.

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
  servers) and act as the person on the web. For a person with a passkey,
  the re-check below stops it at every decision that needs them; for one
  without, it stands.
- **An agent driving the person's real browser** (computer use, a browser
  automation server attached to their profile) looks exactly like the
  person. Only user verification can tell them apart: the passkey
  re-check asks the person to unlock their authenticator for each such
  decision. A person who unlocks whatever their browser asks, without
  reading what the page says, still lets it through.
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

## The passkey re-check

A person can add passkeys in Settings › Account (`internal/passkeys`,
migration 054). Once they have one, the decisions that need a person ask
for it again, on that request (`policy/assurance_test.go` lists them):

- keeping a quarantined (external) proposal, settling a conflict about
  one, and restoring a faded one;
- remembering, editing, keeping or forgetting a decision in a team space,
  and answering a team space's gate (D15);
- raising what an agent may do, and resuming a paused one;
- every Forget: a memory, a note, a space, the account. A passkey holder
  forgets on the web only, since the CLI can't answer;
- removing a passkey, disconnecting GitHub or Google, and adding a
  passkey or a sign-in method when the sign-in isn't fresh (below).

Signing another session out and confirming a device's code stay at
`human_web`. `policy.Decide*` decides it in one place: the actor carries
`Passkey` (they have one) and `Verified` (this request carries an answer
that verified). With a passkey and no answer, the decision is `refuse
needs_passkey`; with a verified answer it applies, and the receipt says
`human_web_verified`. Without a passkey nothing changes: `human_web` still
suffices, and the answer suggests one (`policy.suggest: passkey`, shown
once as a nudge).

### The flow

1. The page sends the command through `/api/proxy` as before.
2. The API refuses it, 403 `needs_passkey`, with a challenge in
   `details.passkey` (WebAuthn request options: the person's credentials,
   `userVerification: required`, the RP ID).
3. The page asks the browser (`navigator.credentials.get`) and sends the
   **same request** again: same method, path, query, body,
   `Idempotency-Key` and `If-Match`, plus the assertion in
   `X-Memax-Passkey` (base64url JSON). `memax-sdk` does this itself when
   given a `passkeyCheck` handler, once per request; the proxy passes the
   header through and signs the request as before.
4. `principalFor` verifies the assertion before anything runs; the command
   then goes through policy as usual, with `Verified` set.

### What an answer is bound to

The challenge row (`v2.passkey_challenges`) holds the person, the session
(the access token's `sid`), and the SHA-256 of the request:
`memax-passkey-check/v1`, the method, the escaped path and query, the
`Idempotency-Key`, the `If-Match` and the body's SHA-256, one per line. An
answer verifies only when all of these hold:

- the challenge is the one in the assertion's client data, unexpired
  (5 minutes; the database refuses more than 10), and unused: it is marked
  used in the transaction that checks it (`SELECT … FOR UPDATE`), so two
  answers racing for one can't both pass;
- it was issued to this person, for this session (a session signed out
  since is refused), for this request's hash. Another memory, body, key,
  version or method is `other_request`; another session of the same
  person is `other_session`;
- the credential is one of the person's, the origin is one of
  `WEBAUTHN_RP_ORIGINS`, the RP ID hash matches, the UP and UV flags are
  set, the signature verifies (go-webauthn), and the signature counter
  didn't go backwards (`cloned`).

A refused answer is 403 `passkey_invalid` with the reason in
`details.passkey_failure`, and nothing runs. A bogus answer doesn't spend
the challenge (it never gets past verification), so it can't be used to
deny the person their own check. An answer that verified is spent whether
the command then applies or not; a retry after a lost response either
replays the applied command (idempotency answers before policy) or asks
again.

### Only the web reaches it

The CLI and agents never get a challenge: a needs_passkey refusal there
says to make the decision on memax.app, and an `X-Memax-Passkey` header on
anything but a signed web request is refused (403 `permission_denied`).
WebAuthn binds an assertion to the page's origin, so an authenticator
won't answer for memax.app anywhere else, and the CLI can't produce one.
So `human_web_verified` needs both the web surface (above) and the
person's authenticator, unlocked.

### Rollout: per person, from their first passkey

There is no switch. Passkeys are on for a deployment when
`WEBAUTHN_RP_ID` or `APP_BASE_URL` names the relying party, and the
re-check is on for each person from their first passkey. Requiring one
from everyone was rejected: a person whose browser or device can't make a
passkey (older Linux desktops, managed machines, some password managers)
would lose decisions they make today, and recovery would need support.
Per person, the people who care get the stronger guarantee at once,
nobody loses anything, and receipts show which guarantee each decision
had. The nudge after a decision made without one, Security's "What your
Keep counts as" and Settings › Account ask for one.

### Adding, removing and recovery

- **Adding** a passkey or a sign-in method needs a fresh sign-in (within
  10 minutes, `passkeys.EnrollWindow`) or a passkey the person already
  has. Otherwise anyone holding the web session's cookies could add their
  own passkey and pass every re-check after.
- **Removing** one, or disconnecting GitHub or Google, needs the passkey
  when the person has one; otherwise a cookie thief would remove it and
  turn the re-check off. V1's link and unlink routes refuse people with a
  passkey for the same reason, and so do V1's space delete and account
  wipe (which forget through the ledger): those people use V2's Account
  and Forget, which ask.
- **Lost every passkey:** sign in again (GitHub, Google, an email code),
  add a new passkey within the window, then remove the old one with it.
  Nothing is lost, and receipts made with the old one stay as they were.
- **D15:** a team space's decisions need `human_web` from every member,
  and `human_web_verified` from members with a passkey. One member's
  passkey changes nothing for the others; a member who loses theirs
  recovers as above, and meanwhile makes none of those decisions.
- **Signing in with a passkey** is a discoverable assertion (no user named
  first); the credential's user handle names the person, checked against
  `v2.passkey_owner` (the one read across people, `SECURITY DEFINER`). It
  makes a web session like any other sign-in, and is rate limited per
  address (60 a minute).

### Storage

`v2.passkeys` (credential id, public key, counter, backup flags, AAGUID,
name) and `v2.passkey_challenges` are under RLS keyed on `app.person_id`:
a statement sees only that person's rows. `memax_v2` may not delete them;
removing goes through `v2.remove_passkeys` and expired challenges through
`v2.clear_passkey_challenges`, both `SECURITY DEFINER` and scoped to the
person. Forgetting the account removes every passkey and signs every
session out.

## Attacks the re-check defeats

| Attacker | Result |
| --- | --- |
| Software with the web cookies (from the disk), through the proxy | 403 `needs_passkey`: it has no authenticator to answer |
| The same, answering with an assertion captured for another decision | `other_request`: the hash names one request |
| Replaying an answer that worked | `used` |
| An answer from another of the person's sessions | `other_session` |
| Holding an answer back | `expired` after 5 minutes |
| An agent driving the person's browser | The authenticator asks the person to unlock it for each decision |
| The CLI sending `X-Memax-Passkey` | 403 `permission_denied`: only a signed web request is checked |
| A cookie thief removing the passkey, or adding their own | Removing needs the passkey; adding needs it or a fresh sign-in |
| A copied credential | Its counter goes backwards: `cloned`, refused |

## Residual risks of the re-check

- **Recovery is as strong as the weakest sign-in.** Whoever can sign in
  fresh as the person (their GitHub, their inbox) can add a passkey of
  their own within 10 minutes, then remove the person's with it. The new
  passkey and its session show in Account; Memax doesn't yet email the
  person when a passkey is added.
- **The fresh window.** Cookies taken within 10 minutes of a sign-in let
  the holder add a passkey without having one. A shorter window trades
  against people who add one right after signing in.
- **A person who unlocks whatever the browser asks.** User verification
  proves the person was there, not that they read the page.
- **V1 surfaces** (the V1 web app and `/v1`) don't ask. They write V1's
  record, not V2's; the V1 routes that reach V2 (space delete, account
  wipe, linking and unlinking a sign-in method) refuse passkey holders.
- **Rate limits** on passkey sign-in are per address, and every sign-in
  through the web deployment shares its address (as above).
