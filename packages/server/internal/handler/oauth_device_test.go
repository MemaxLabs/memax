package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

type deviceClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *deviceClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *deviceClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type deviceRig struct {
	t     *testing.T
	auth  *AuthHandler
	oauth *MCPOAuthHandler
	store *deviceauth.Store
	clock *deviceClock
}

func newDeviceRig(t *testing.T) *deviceRig {
	t.Helper()
	_, pool := testdb.Acquire(t)
	secret := []byte("oauth-device-test-secret-0123456789")
	clock := &deviceClock{t: time.Now()}
	store := deviceauth.New(pool, secret).WithClock(clock.now)
	authH := &AuthHandler{pool: pool, jwtSecret: secret}
	o := &MCPOAuthHandler{authH: authH, baseURL: "https://api.memax.app", appBaseURL: "https://memax.app"}
	o.SetDeviceAuth(store, func(*http.Request) string { return "203.0.113.4" })
	return &deviceRig{t: t, auth: authH, oauth: o, store: store, clock: clock}
}

func (d *deviceRig) user() uuid.UUID {
	d.t.Helper()
	id := uuid.New()
	if _, err := d.auth.pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`,
		id, id.String()[:8]+"@device.test"); err != nil {
		d.t.Fatal(err)
	}
	return id
}

// form posts a form to h and decodes the JSON answer.
func (d *deviceRig) form(h http.HandlerFunc, values url.Values) (int, map[string]any, http.Header) {
	d.t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h(rec, r)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		d.t.Fatalf("decode %d: %v: %s", rec.Code, err, rec.Body.String())
	}
	return rec.Code, body, rec.Header()
}

func (d *deviceRig) authorize() (deviceCode, userCode string) {
	d.t.Helper()
	status, body, header := d.form(d.oauth.DeviceAuthorization, url.Values{
		"client_id": {"memax-cli"}, "client_version": {"2.0.0"}, "device_name": {"ziyang-mbp"},
		"device_os": {"darwin"}, "space": {"memax-v2"},
	})
	if status != http.StatusOK {
		d.t.Fatalf("device authorization: %d %v", status, body)
	}
	if header.Get("Cache-Control") != "no-store" {
		d.t.Errorf("Cache-Control %q", header.Get("Cache-Control"))
	}
	userCode, _ = body["user_code"].(string)
	deviceCode, _ = body["device_code"].(string)
	if !regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[0-9]{4}$`).MatchString(userCode) || len(deviceCode) < 43 {
		d.t.Fatalf("codes %q %q", userCode, deviceCode)
	}
	want := map[string]any{
		"verification_uri":          "https://memax.app/device",
		"verification_uri_complete": "https://memax.app/device?code=" + url.QueryEscape(userCode),
		"expires_in":                float64(600),
		"interval":                  float64(5),
	}
	for k, v := range want {
		if body[k] != v {
			d.t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	return deviceCode, userCode
}

func (d *deviceRig) poll(deviceCode string) (int, map[string]any) {
	d.t.Helper()
	status, body, header := d.form(d.oauth.Token, url.Values{
		"grant_type": {deviceauth.GrantType}, "client_id": {"memax-cli"}, "device_code": {deviceCode},
	})
	if header.Get("Cache-Control") != "no-store" {
		d.t.Errorf("token answer Cache-Control %q", header.Get("Cache-Control"))
	}
	return status, body
}

func (d *deviceRig) wantError(deviceCode, code string) {
	d.t.Helper()
	status, body := d.poll(deviceCode)
	if status != http.StatusBadRequest || body["error"] != code {
		d.t.Fatalf("poll: %d %v, want 400 %s", status, body, code)
	}
}

// TestDeviceGrantIssuesOneCLISession walks RFC 8628 from the CLI's side:
// pending, slow_down, then once confirmed one session whose tokens say
// cli (refreshed too), then invalid_grant.
func TestDeviceGrantIssuesOneCLISession(t *testing.T) {
	t.Parallel()
	d := newDeviceRig(t)
	zz := d.user()
	deviceCode, userCode := d.authorize()

	d.wantError(deviceCode, "authorization_pending")
	d.wantError(deviceCode, "slow_down") // at once: too fast
	d.clock.add(11 * time.Second)
	d.wantError(deviceCode, "authorization_pending")

	if _, err := d.store.Approve(context.Background(), userCode, zz); err != nil {
		t.Fatal(err)
	}
	status, body := d.poll(deviceCode)
	if status != http.StatusOK || body["token_type"] != "Bearer" || body["refresh_token"] == "" || body["expires_in"] != float64(3600) {
		t.Fatalf("collect: %d %v", status, body)
	}
	claims, err := auth.VerifyAccessToken(body["access_token"].(string), d.auth.jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Sub != zz.String() || claims.Surface != auth.SurfaceCLI || claims.AgentName != "" || claims.GrantID != "" {
		t.Fatalf("a device's session is %+v, want the person's, surface cli", claims)
	}
	var surface string
	if err := d.auth.pool.QueryRow(context.Background(), `SELECT surface FROM sessions WHERE refresh_token = $1`,
		body["refresh_token"]).Scan(&surface); err != nil || surface != auth.SurfaceCLI {
		t.Fatalf("session surface %q (%v)", surface, err)
	}

	// A refreshed token stays the CLI's.
	raw, _ := json.Marshal(map[string]any{"refresh_token": body["refresh_token"]})
	rec := httptest.NewRecorder()
	d.auth.Refresh(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(string(raw))))
	var env struct {
		Data model.TokenPair `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if c, err := auth.VerifyAccessToken(env.Data.AccessToken, d.auth.jwtSecret); err != nil || c.Surface != auth.SurfaceCLI {
		t.Fatalf("refreshed: %+v, %v", c, err)
	}

	// The auth middleware carries the cli surface, which /v2 never counts
	// as the web (internal/websurface).
	var grant GrantContext
	mw := RequireAuth(d.auth.jwtSecret, nil, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { grant = GetGrant(r) }))
	r := httptest.NewRequest(http.MethodGet, "/v2/spaces", nil)
	r.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
	mw.ServeHTTP(httptest.NewRecorder(), r)
	if grant.Surface != auth.SurfaceCLI || grant.UserID != zz.String() {
		t.Fatalf("grant %+v", grant)
	}

	// One session per code.
	d.clock.add(time.Minute)
	d.wantError(deviceCode, "invalid_grant")
	if n := d.count(`SELECT count(*) FROM sessions WHERE user_id = $1`, zz); n != 1 {
		t.Fatalf("%d sessions from one code", n)
	}
}

func TestDeviceGrantErrors(t *testing.T) {
	t.Parallel()
	d := newDeviceRig(t)
	zz := d.user()

	denied, deniedUser := d.authorize()
	if _, err := d.store.Deny(context.Background(), deniedUser, zz); err != nil {
		t.Fatal(err)
	}
	d.wantError(denied, "access_denied")

	expired, _ := d.authorize()
	d.clock.add(deviceauth.Lifetime)
	d.wantError(expired, "expired_token")

	d.wantError("no-such-code", "invalid_grant")
	if n := d.count(`SELECT count(*) FROM sessions WHERE user_id = $1`, zz); n != 0 {
		t.Fatalf("%d sessions without a confirmed code", n)
	}

	// Only the memax CLI uses the grant.
	status, body, _ := d.form(d.oauth.DeviceAuthorization, url.Values{"client_id": {"claude-desktop"}})
	if status != http.StatusUnauthorized || body["error"] != "invalid_client" {
		t.Fatalf("another client: %d %v", status, body)
	}
	status, body, _ = d.form(d.oauth.Token, url.Values{
		"grant_type": {deviceauth.GrantType}, "client_id": {"claude-desktop"}, "device_code": {denied},
	})
	if status != http.StatusUnauthorized || body["error"] != "invalid_client" {
		t.Fatalf("another client polling: %d %v", status, body)
	}

	// Codes are limited per address, across machines.
	for range deviceauth.PerIPMax - 2 {
		d.authorize()
	}
	status, body, header := d.form(d.oauth.DeviceAuthorization, url.Values{"client_id": {"memax-cli"}})
	if status != http.StatusTooManyRequests || body["error"] != "slow_down" || header.Get("Retry-After") == "" {
		t.Fatalf("one code too many: %d %v", status, body)
	}
}

func (d *deviceRig) count(sql string, args ...any) int {
	d.t.Helper()
	var n int
	if err := d.auth.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		d.t.Fatal(err)
	}
	return n
}

// TestDeviceGrantMetadata: the server metadata advertises the device
// endpoint and grant only when device sign-in is on.
func TestDeviceGrantMetadata(t *testing.T) {
	t.Parallel()
	meta := func(o *MCPOAuthHandler) map[string]any {
		rec := httptest.NewRecorder()
		o.AuthorizationServerMetadata(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	on := &MCPOAuthHandler{baseURL: "https://api.memax.app", device: &deviceauth.Store{}}
	m := meta(on)
	grants, _ := m["grant_types_supported"].([]any)
	if m["device_authorization_endpoint"] != "https://api.memax.app/oauth/device_authorization" ||
		!slices.Contains(grants, any(deviceauth.GrantType)) {
		t.Fatalf("on: %v", m)
	}
	off := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	m = meta(off)
	if _, ok := m["device_authorization_endpoint"]; ok {
		t.Fatalf("off: %v", m)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(url.Values{
		"grant_type": {deviceauth.GrantType}, "client_id": {"memax-cli"}, "device_code": {"x"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	off.Token(rec, r)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Fatalf("off, the grant: %d %s", rec.Code, rec.Body.String())
	}
}
