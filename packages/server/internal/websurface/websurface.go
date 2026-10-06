// Package websurface verifies that a /v2 request came from a person using
// the Memax web app, so a Keep made there can carry assurance human_web
// (plan 25 §5.12, D15) while the same person's CLI login can't.
//
// # The mechanism
//
// Two independent facts must hold, and the API checks both:
//
//  1. The session was issued to the web app. When a login completes, the
//     server records its surface from where the one-time code is
//     delivered: the web app's origin (APP_BASE_URL) is "web", a loopback
//     redirect or a token returned in the response is "cli" (migration
//     030). Every access token the session mints, refreshes included,
//     carries that surface as a claim, signed with JWT_SECRET. `memax
//     login` gets a "cli" session; nothing a CLI or agent does can turn its
//     token into a "web" one.
//
//  2. The request came through the web app's server-side proxy
//     (packages/web/src/app/api/proxy). The proxy signs each /v2 request
//     it forwards with WEB_SURFACE_SECRET, shared only by the web
//     deployment and the API: an HMAC-SHA256 over the method, the request
//     target (path and query), a timestamp, a nonce, the session's user id,
//     the Idempotency-Key and If-Match headers and the SHA-256 of the body
//     (see Request). The API refuses a signature that is wrong, more than
//     60 seconds from its clock, for another user, or seen before.
//
// Only then is the request's surface "web" (human_web). A request that
// carries no signature is client_attested. One that carries a signature
// that doesn't verify is refused outright: nothing legitimate sends one.
//
// # Threat model
//
// A local agent with the person's CLI token (the case D15 exists for).
// It can call the API directly, but can't produce a valid signature
// without WEB_SURFACE_SECRET, which never leaves the two servers. It can
// also send its token through the public web proxy, which will sign the
// request, but the token's surface is "cli", so the API still treats it as
// client_attested. Fact 1 is what defeats this: the proxy alone can't tell
// whose browser a bearer token came from. It can't mint a "web" token
// either: those are issued only to a code redirected to the web app's
// origin, which lands in the browser, not with the agent.
//
// A stolen signed request. Signatures are bound to the method, target,
// body, user, Idempotency-Key and If-Match, live 60 seconds, and each
// nonce is accepted once per API process. Replayed to another API machine
// inside the window, a command with the same Idempotency-Key is an
// idempotent replay that writes nothing new. Traffic between the proxy
// and the API is TLS (behind Cloudflare and Fly).
//
// A leaked WEB_SURFACE_SECRET. Whoever holds it can sign; they still need
// a "web" session token for the user. Rotate the secret on both sides.
//
// Residual risks, which this mechanism does not address:
//
//   - Browser XSS on memax.app. Script in the page can call the proxy as
//     the person, and today it can also read the session tokens, which
//     the web app keeps in localStorage, and replay them through the proxy
//     from anywhere for their lifetime (refresh tokens: 30 days).
//   - A local agent reading the browser's storage on disk. localStorage
//     is a plaintext LevelDB under the browser profile, so an agent with
//     the person's shell can lift a "web" token and send it through the
//     proxy.
//   - An agent driving the person's real browser (computer use, a browser
//     automation server attached to their profile) is indistinguishable
//     from the person. Only a user-verification step (a passkey with UV)
//     at Keep time would tell them apart.
//   - Someone who can sign in as the person (their GitHub account, their
//     inbox for the email code) gets a web session of their own.
//
// The recommended follow-up is a backend-for-frontend: keep the web
// session in an httpOnly, Secure, SameSite cookie that only the proxy
// reads, and have the proxy attach the bearer token itself. Page script
// then never sees a token, which removes token theft by XSS (script can
// still act through the proxy while the page is open), and browsers store
// cookies encrypted at rest, unlike localStorage, which raises the bar
// for a local agent. The API side of this package doesn't change: the
// proxy keeps signing, and fact 1 keeps the CLI out.
//
// # Configuration
//
// WEB_SURFACE_SECRET is read once at startup and must be at least 32
// characters (openssl rand -hex 32). Unset means disabled: New returns
// nil, every request is client_attested, and keeps that need a person on
// the web are refused with the reason. The web app needs the same secret.
package websurface

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The headers the web app's proxy sets. Clients can't set them through
// the proxy, which forwards only an allowlist of headers.
const (
	HeaderSurface   = "X-Memax-Surface"
	HeaderTimestamp = "X-Memax-Surface-Timestamp"
	HeaderNonce     = "X-Memax-Surface-Nonce"
	HeaderUser      = "X-Memax-Surface-User"
	HeaderSignature = "X-Memax-Surface-Signature"
)

var headers = []string{HeaderSurface, HeaderTimestamp, HeaderNonce, HeaderUser, HeaderSignature}

const (
	// Web is the one surface the proxy claims.
	Web = "web"
	// version prefixes the signature ("v1=…") and the signed string, so a
	// new scheme can run beside this one during a rotation.
	version = "v1"
	// MaxSkew is how far a signature's timestamp may be from the API's
	// clock, either way.
	MaxSkew = 60 * time.Second
	// MinSecretLength is the shortest WEB_SURFACE_SECRET New accepts.
	MinSecretLength = 32
	// MaxBody is the largest body a signed request may carry (/v2's own
	// limit).
	MaxBody = 1 << 20
)

// Request is what a signature covers. Target is the request target as
// sent: the escaped path, plus "?" and the raw query when there is one.
// BodySHA256 is the lowercase hex SHA-256 of the body (of nothing, for a
// request without one).
type Request struct {
	Method         string
	Target         string
	Timestamp      int64
	Nonce          string
	UserID         string
	IdempotencyKey string
	IfMatch        string
	BodySHA256     string
}

// canonical is the signed string: one field per line, in a fixed order.
// No field can hold a newline (headers can't, nor can a request target).
func (r Request) canonical() string {
	return strings.Join([]string{
		"memax-web-surface/" + version,
		strings.ToUpper(r.Method),
		r.Target,
		strconv.FormatInt(r.Timestamp, 10),
		r.Nonce,
		r.UserID,
		r.IdempotencyKey,
		r.IfMatch,
		r.BodySHA256,
	}, "\n")
}

// Sign returns the signature header value for r: "v1=" and the base64url
// HMAC-SHA256 of the canonical string. The web proxy implements the same
// function; the tests on both sides share a vector.
func Sign(secret []byte, r Request) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(r.canonical()))
	return version + "=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// BodyHash is the lowercase hex SHA-256 of a body.
func BodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ErrNotClaimed: the request doesn't claim the web surface at all.
var ErrNotClaimed = errors.New("websurface: the request doesn't claim the web surface")

// Reasons a claim is refused.
const (
	ReasonMalformed    = "malformed"
	ReasonBadSignature = "bad_signature"
	ReasonStale        = "stale"
	ReasonReplayed     = "replayed"
	ReasonWrongUser    = "wrong_user"
	ReasonBodyTooLarge = "body_too_large"
)

// Error is a claim that doesn't verify. Reason is one of the Reason
// constants; it is safe to log (it never includes the signature).
type Error struct{ Reason string }

func (e *Error) Error() string { return "websurface: web surface not verified: " + e.Reason }

// Claimed reports whether the request carries any of the web-surface
// headers.
func Claimed(r *http.Request) bool {
	for _, h := range headers {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// Verifier checks web-surface signatures. A nil *Verifier means the
// mechanism is disabled; callers check for nil.
type Verifier struct {
	key    []byte
	now    func() time.Time
	nonces *nonceCache
}

// Option configures a Verifier.
type Option func(*Verifier)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(v *Verifier) { v.now = now } }

// New returns a Verifier for the shared secret, or nil when the secret is
// empty (disabled). A secret shorter than MinSecretLength is an error.
func New(secret string, opts ...Option) (*Verifier, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("websurface: WEB_SURFACE_SECRET must be at least %d characters (openssl rand -hex 32)", MinSecretLength)
	}
	v := &Verifier{key: []byte(secret), now: time.Now, nonces: newNonceCache(2 * MaxSkew)}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

// Verify checks that r was signed by the web app's proxy for userID. It
// returns ErrNotClaimed when r carries no web-surface headers, an *Error
// when the claim doesn't verify, and nil when it does. It reads the body
// (at most MaxBody) to check its hash and puts it back for the handler.
func (v *Verifier) Verify(r *http.Request, userID string) error {
	if !Claimed(r) {
		return ErrNotClaimed
	}
	bad := func(reason string) error { return &Error{Reason: reason} }
	h := r.Header
	if h.Get(HeaderSurface) != Web || h.Get(HeaderTimestamp) == "" || h.Get(HeaderNonce) == "" ||
		h.Get(HeaderUser) == "" || h.Get(HeaderSignature) == "" {
		return bad(ReasonMalformed)
	}
	if h.Get(HeaderUser) != userID {
		return bad(ReasonWrongUser)
	}
	ts, err := strconv.ParseInt(h.Get(HeaderTimestamp), 10, 64)
	if err != nil {
		return bad(ReasonMalformed)
	}
	if d := v.now().Sub(time.Unix(ts, 0)); d > MaxSkew || d < -MaxSkew {
		return bad(ReasonStale)
	}
	nonce := h.Get(HeaderNonce)
	if raw, err := base64.RawURLEncoding.DecodeString(nonce); err != nil || len(raw) < 16 || len(raw) > 64 {
		return bad(ReasonMalformed)
	}
	sig, ok := strings.CutPrefix(h.Get(HeaderSignature), version+"=")
	if !ok {
		return bad(ReasonMalformed)
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(got) != sha256.Size {
		return bad(ReasonMalformed)
	}
	body, err := readBody(r)
	if err != nil {
		return bad(ReasonBodyTooLarge)
	}
	want := Sign(v.key, Request{
		Method: r.Method, Target: target(r), Timestamp: ts, Nonce: nonce, UserID: userID,
		IdempotencyKey: h.Get("Idempotency-Key"), IfMatch: h.Get("If-Match"), BodySHA256: BodyHash(body),
	})
	wantRaw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(want, version+"="))
	if !hmac.Equal(got, wantRaw) {
		return bad(ReasonBadSignature)
	}
	// Only authentic nonces reach the cache, so it can't be flooded.
	if !v.nonces.add(nonce, v.now()) {
		return bad(ReasonReplayed)
	}
	return nil
}

// target is the request target the proxy signed: the escaped path and the
// raw query.
func target(r *http.Request) string {
	t := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		t += "?" + r.URL.RawQuery
	}
	return t
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBody {
		return nil, errors.New("body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// nonceCache remembers the nonces of verified signatures for ttl, twice
// MaxSkew: a signature is accepted for at most MaxSkew either side of its
// timestamp, so by the time an entry is forgotten its signature is stale.
type nonceCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	seen      map[string]time.Time // nonce → when it may be forgotten
	nextSweep time.Time
}

// maxNonces bounds the cache: far beyond the web app's request rate over
// two minutes. Past it, new signatures are refused rather than letting
// the cache grow without limit.
const maxNonces = 1_000_000

func newNonceCache(ttl time.Duration) *nonceCache {
	return &nonceCache{ttl: ttl, seen: map[string]time.Time{}}
}

// add records nonce and reports whether it was new.
func (c *nonceCache) add(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if until, ok := c.seen[nonce]; ok && now.Before(until) {
		return false
	}
	if !now.Before(c.nextSweep) {
		for n, until := range c.seen {
			if !now.Before(until) {
				delete(c.seen, n)
			}
		}
		c.nextSweep = now.Add(c.ttl / 4)
	}
	if len(c.seen) >= maxNonces {
		return false
	}
	c.seen[nonce] = now.Add(c.ttl)
	return true
}
