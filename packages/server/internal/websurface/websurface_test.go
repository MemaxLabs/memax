package websurface_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// The shared test vector. packages/web/src/app/api/proxy/[...path]/
// route.test.ts signs the same request and must get the same signature,
// so the proxy and the API can't drift.
const (
	vectorSecret    = "memax-web-surface-test-secret-0123456789"
	vectorMethod    = "POST"
	vectorTarget    = "/v2/memories/M-0001:keep?space=memax-v2"
	vectorTimestamp = 1791100800
	vectorNonce     = "AAECAwQFBgcICQoLDA0ODw" // bytes 0…15
	vectorUser      = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	vectorKey       = "key-1"
	vectorIfMatch   = `"1"`
	vectorBody      = `{"reason":"checked"}`
	vectorSignature = "v1=HrupTTGJYu02EBeLUDGE3eyoT3NH1FR0V04NII2-Qwk"
)

func TestSignMatchesTheSharedVector(t *testing.T) {
	t.Parallel()
	req := websurface.Request{
		Method: vectorMethod, Target: vectorTarget, Timestamp: vectorTimestamp, Nonce: vectorNonce,
		UserID: vectorUser, IdempotencyKey: vectorKey, IfMatch: vectorIfMatch, BodySHA256: websurface.BodyHash([]byte(vectorBody)),
	}
	got := websurface.Sign([]byte(vectorSecret), req)
	// The canonical string, spelled out independently of the package.
	canonical := strings.Join([]string{"memax-web-surface/v1", vectorMethod, vectorTarget, strconv.Itoa(vectorTimestamp),
		vectorNonce, vectorUser, vectorKey, vectorIfMatch, websurface.BodyHash([]byte(vectorBody))}, "\n")
	mac := hmac.New(sha256.New, []byte(vectorSecret))
	mac.Write([]byte(canonical))
	if want := "v1=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("Sign = %s, want %s", got, want)
	}
	if got != vectorSignature {
		t.Errorf("Sign = %s; the shared vector says %s", got, vectorSignature)
	}
	if h := websurface.BodyHash(nil); h != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("the empty body hashes to %s", h)
	}
}

const secret = "a-web-surface-secret-of-at-least-32-chars"

type signed struct {
	method, target, body string
	user, key, ifMatch   string
	at                   time.Time
	nonce                string
	secret               string
}

// request builds a request the way the proxy signs it, then applies edit
// (to tamper with it after signing).
func request(s signed, edit func(*http.Request)) *http.Request {
	if s.method == "" {
		s.method = "POST"
	}
	if s.target == "" {
		s.target = "/v2/memories/M-0001:keep?space=memax-v2"
	}
	if s.nonce == "" {
		id := uuid.New()
		s.nonce = base64.RawURLEncoding.EncodeToString(id[:])
	}
	if s.secret == "" {
		s.secret = secret
	}
	var body io.Reader
	if s.body != "" {
		body = strings.NewReader(s.body)
	}
	r := httptest.NewRequest(s.method, s.target, body)
	if s.key != "" {
		r.Header.Set("Idempotency-Key", s.key)
	}
	if s.ifMatch != "" {
		r.Header.Set("If-Match", s.ifMatch)
	}
	r.Header.Set(websurface.HeaderSurface, "web")
	r.Header.Set(websurface.HeaderTimestamp, strconv.FormatInt(s.at.Unix(), 10))
	r.Header.Set(websurface.HeaderNonce, s.nonce)
	r.Header.Set(websurface.HeaderUser, s.user)
	r.Header.Set(websurface.HeaderSignature, websurface.Sign([]byte(s.secret), websurface.Request{
		Method: s.method, Target: s.target, Timestamp: s.at.Unix(), Nonce: s.nonce, UserID: s.user,
		IdempotencyKey: s.key, IfMatch: s.ifMatch, BodySHA256: websurface.BodyHash([]byte(s.body)),
	}))
	if edit != nil {
		edit(r)
	}
	return r
}

func TestVerify(t *testing.T) {
	t.Parallel()
	now := time.Unix(1791100800, 0)
	v, err := websurface.New(secret, websurface.WithClock(func() time.Time { return now }))
	if err != nil || v == nil {
		t.Fatalf("New: %v %v", v, err)
	}
	user := uuid.NewString()
	base := signed{user: user, key: "k1", ifMatch: `"2"`, body: `{"reason":"looked at it"}`, at: now}
	with := func(f func(*signed)) signed { s := base; f(&s); return s }

	cases := []struct {
		name   string
		req    *http.Request
		user   string
		reason string // "" = verified; "none" = not claimed
	}{
		{"valid", request(base, nil), user, ""},
		{"valid, no body", request(with(func(s *signed) { s.body, s.key, s.ifMatch, s.method = "", "", "", "GET" }), nil), user, ""},
		{"valid at the edge of the window", request(with(func(s *signed) { s.at = now.Add(-websurface.MaxSkew) }), nil), user, ""},
		{"valid from a clock slightly ahead", request(with(func(s *signed) { s.at = now.Add(30 * time.Second) }), nil), user, ""},
		{"missing", httptest.NewRequest("POST", "/v2/spaces", nil), user, "none"},
		{"wrong secret", request(with(func(s *signed) { s.secret = "another-secret-also-at-least-32-chars" }), nil), user, websurface.ReasonBadSignature},
		{"garbage signature", request(base, func(r *http.Request) { r.Header.Set(websurface.HeaderSignature, "v1=bm9wZQ") }), user, websurface.ReasonMalformed},
		{"unknown version", request(base, func(r *http.Request) {
			r.Header.Set(websurface.HeaderSignature, strings.Replace(r.Header.Get(websurface.HeaderSignature), "v1=", "v2=", 1))
		}), user, websurface.ReasonMalformed},
		{"stale", request(with(func(s *signed) { s.at = now.Add(-websurface.MaxSkew - time.Second) }), nil), user, websurface.ReasonStale},
		{"from the future", request(with(func(s *signed) { s.at = now.Add(websurface.MaxSkew + time.Second) }), nil), user, websurface.ReasonStale},
		{"wrong user", request(base, nil), uuid.NewString(), websurface.ReasonWrongUser},
		{"user header swapped", request(base, func(r *http.Request) { r.Header.Set(websurface.HeaderUser, "someone") }), "someone", websurface.ReasonBadSignature},
		{"another method", request(base, func(r *http.Request) { r.Method = "PATCH" }), user, websurface.ReasonBadSignature},
		{"another path", request(base, func(r *http.Request) { r.URL.Path = "/v2/memories/M-0002:keep" }), user, websurface.ReasonBadSignature},
		{"another query", request(base, func(r *http.Request) { r.URL.RawQuery = "space=elsewhere" }), user, websurface.ReasonBadSignature},
		{"another body", request(base, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"reason":"x"}`)) }), user, websurface.ReasonBadSignature},
		{"another idempotency key", request(base, func(r *http.Request) { r.Header.Set("Idempotency-Key", "k2") }), user, websurface.ReasonBadSignature},
		{"another If-Match", request(base, func(r *http.Request) { r.Header.Set("If-Match", `"3"`) }), user, websurface.ReasonBadSignature},
		{"a timestamp that isn't one", request(base, func(r *http.Request) { r.Header.Set(websurface.HeaderTimestamp, "soon") }), user, websurface.ReasonMalformed},
		{"a short nonce", request(with(func(s *signed) { s.nonce = "AAEC" }), nil), user, websurface.ReasonMalformed},
		{"no surface header", request(base, func(r *http.Request) { r.Header.Del(websurface.HeaderSurface) }), user, websurface.ReasonMalformed},
		{"another surface", request(base, func(r *http.Request) { r.Header.Set(websurface.HeaderSurface, "cli") }), user, websurface.ReasonMalformed},
		{"only the surface header", func() *http.Request {
			r := httptest.NewRequest("GET", "/v2/spaces", nil)
			r.Header.Set(websurface.HeaderSurface, "web")
			return r
		}(), user, websurface.ReasonMalformed},
		{"a body over the limit", request(with(func(s *signed) { s.body = strings.Repeat("x", websurface.MaxBody+1) }), nil), user, websurface.ReasonBodyTooLarge},
	}
	for _, c := range cases {
		err := v.Verify(c.req, c.user)
		var we *websurface.Error
		switch {
		case c.reason == "" && err != nil:
			t.Errorf("%s: %v, want verified", c.name, err)
		case c.reason == "none" && !errors.Is(err, websurface.ErrNotClaimed):
			t.Errorf("%s: %v, want not claimed", c.name, err)
		case c.reason != "" && c.reason != "none" && (!errors.As(err, &we) || we.Reason != c.reason):
			t.Errorf("%s: %v, want %s", c.name, err, c.reason)
		}
	}

	// The handler still gets the body.
	r := request(base, nil)
	r.Header.Set(websurface.HeaderNonce, base64.RawURLEncoding.EncodeToString([]byte("a-fresh-nonce-123")))
	r.Header.Set(websurface.HeaderSignature, websurface.Sign([]byte(secret), websurface.Request{
		Method: "POST", Target: base.path(), Timestamp: now.Unix(), Nonce: r.Header.Get(websurface.HeaderNonce), UserID: user,
		IdempotencyKey: "k1", IfMatch: `"2"`, BodySHA256: websurface.BodyHash([]byte(base.body)),
	}))
	if err := v.Verify(r, user); err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r.Body); string(b) != base.body {
		t.Errorf("body after Verify = %q", b)
	}
}

func (s signed) path() string {
	if s.target == "" {
		return "/v2/memories/M-0001:keep?space=memax-v2"
	}
	return s.target
}

func TestReplayIsRefused(t *testing.T) {
	t.Parallel()
	now := time.Unix(1791100800, 0)
	clock := now
	v, _ := websurface.New(secret, websurface.WithClock(func() time.Time { return clock }))
	user := uuid.NewString()
	s := signed{user: user, at: now, nonce: base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef"))}
	if err := v.Verify(request(s, nil), user); err != nil {
		t.Fatalf("first: %v", err)
	}
	var we *websurface.Error
	if err := v.Verify(request(s, nil), user); !errors.As(err, &we) || we.Reason != websurface.ReasonReplayed {
		t.Errorf("replay: %v", err)
	}
	// Still refused at the end of the window; after it, stale.
	clock = now.Add(websurface.MaxSkew)
	if err := v.Verify(request(s, nil), user); !errors.As(err, &we) || we.Reason != websurface.ReasonReplayed {
		t.Errorf("replay at the end of the window: %v", err)
	}
	clock = now.Add(websurface.MaxSkew + time.Second)
	if err := v.Verify(request(s, nil), user); !errors.As(err, &we) || we.Reason != websurface.ReasonStale {
		t.Errorf("replay after the window: %v", err)
	}
	// A forged signature doesn't burn a nonce.
	forged := signed{user: user, at: now, nonce: base64.RawURLEncoding.EncodeToString([]byte("fedcba9876543210")), secret: "a-guess-that-is-at-least-32-chars-long"}
	clock = now
	_ = v.Verify(request(forged, nil), user)
	forged.secret = ""
	if err := v.Verify(request(forged, nil), user); err != nil {
		t.Errorf("a nonce first seen on a forgery was burnt: %v", err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	if v, err := websurface.New("  "); v != nil || err != nil {
		t.Errorf("empty secret: %v %v, want disabled", v, err)
	}
	if v, err := websurface.New("short"); v != nil || err == nil {
		t.Errorf("short secret: %v %v, want an error", v, err)
	}
	if !websurface.Claimed(request(signed{user: "u", at: time.Now()}, nil)) || websurface.Claimed(httptest.NewRequest("GET", "/", nil)) {
		t.Error("Claimed")
	}
}
