package v2api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

const (
	deviceLookup  = "/v2/device-authorizations:lookup"
	deviceApprove = "/v2/device-authorizations:approve"
	deviceDeny    = "/v2/device-authorizations:deny"
)

type deviceView struct {
	UserCode      string     `json:"user_code"`
	State         string     `json:"state"`
	ClientID      string     `json:"client_id"`
	ClientVersion string     `json:"client_version"`
	DeviceName    string     `json:"device_name"`
	DeviceOS      string     `json:"device_os"`
	Space         string     `json:"space"`
	Address       string     `json:"address"`
	RequestedAt   time.Time  `json:"requested_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	DecidedAt     *time.Time `json:"decided_at"`
	SignedInAt    *time.Time `json:"signed_in_at"`
}

// newDeviceEnv is the web env with device sign-in on.
func newDeviceEnv(t *testing.T) (*env, *deviceauth.Store) {
	t.Helper()
	v, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	var store *deviceauth.Store
	e := newEnvWith(t, func(e *env) []v2api.Option {
		store = deviceauth.New(e.pool, []byte(testSecret))
		return []v2api.Option{v2api.WithWebSurface(v), v2api.WithDevices(store)}
	})
	return e, store
}

func newCode(t *testing.T, s *deviceauth.Store) *deviceauth.Issued {
	t.Helper()
	got, err := s.Create(context.Background(), deviceauth.Start{
		ClientID: deviceauth.ClientCLI, ClientVersion: "2.0.0", DeviceName: "ziyang-mbp", DeviceOS: "darwin",
		Space: "memax-v2", IP: "203.0.113.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestDeviceConfirmedOnTheWeb: a person on the web reads a waiting code
// (what the device says, and where it asked from), confirms it, and the
// CLI's poll collects their session; a retry answers as it is, and the
// other decision is 409 with how it ended.
func TestDeviceConfirmedOnTheWeb(t *testing.T) {
	t.Parallel()
	e, store := newDeviceEnv(t)
	zz := e.user("zz")
	code := newCode(t, store)
	web := e.webSession(zz)
	body := map[string]string{"user_code": code.UserCode}

	var got deviceView
	e.do(call{method: "POST", path: deviceLookup, token: web, body: map[string]string{"user_code": " " + code.UserCode[:4] + code.UserCode[5:] + " "},
		header: map[string]string{"Idempotency-Key": ""}}).ok(http.StatusOK, &got)
	if got.UserCode != code.UserCode || got.State != "pending" || got.ClientID != "memax-cli" || got.ClientVersion != "2.0.0" ||
		got.DeviceName != "ziyang-mbp" || got.DeviceOS != "macOS" || got.Space != "memax-v2" || got.Address != "203.0.113.4" ||
		got.ExpiresAt.Sub(got.RequestedAt) != deviceauth.Lifetime {
		t.Fatalf("lookup: %+v", got)
	}

	e.do(call{method: "POST", path: deviceApprove, token: web, body: body, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &got)
	if got.State != "approved" || got.DecidedAt == nil {
		t.Fatalf("approve: %+v", got)
	}
	user, err := store.Poll(context.Background(), deviceauth.ClientCLI, code.DeviceCode)
	if err != nil || user != zz {
		t.Fatalf("the CLI collects %s, %v", user, err)
	}
	e.do(call{method: "POST", path: deviceLookup, token: web, body: body, header: map[string]string{"Idempotency-Key": ""}}).ok(http.StatusOK, &got)
	if got.State != "signed_in" || got.SignedInAt == nil {
		t.Fatalf("after the CLI collected: %+v", got)
	}
	// Confirming again is a retry; declining now is too late.
	e.do(call{method: "POST", path: deviceApprove, token: web, body: body, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &got)
	if got.State != "signed_in" {
		t.Fatalf("approve again: %+v", got)
	}
	e.do(call{method: "POST", path: deviceDeny, token: web, body: body, sign: webSigned(zz, tamper{})}).
		fails(http.StatusConflict, "invalid_transition")

	// "It doesn't match": the CLI hears access_denied.
	other := newCode(t, store)
	e.do(call{method: "POST", path: deviceDeny, token: web, body: map[string]string{"user_code": other.UserCode},
		sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &got)
	if got.State != "denied" {
		t.Fatalf("deny: %+v", got)
	}
	if _, err := store.Poll(context.Background(), deviceauth.ClientCLI, other.DeviceCode); !errors.Is(err, deviceauth.ErrDenied) {
		t.Fatalf("poll a declined code: %v", err)
	}
	e.do(call{method: "POST", path: deviceApprove, token: web, body: map[string]string{"user_code": other.UserCode},
		sign: webSigned(zz, tamper{})}).fails(http.StatusConflict, "invalid_transition")

	// Someone else can't see either, as if they didn't exist.
	jy := e.user("jy")
	e.do(call{method: "POST", path: deviceLookup, token: e.webSession(jy), body: body,
		header: map[string]string{"Idempotency-Key": ""}}).fails(http.StatusNotFound, "not_found")

	// An expired code says so.
	late := newCode(t, store)
	e.exec(`UPDATE device_authorizations SET expires_at = now() - interval '1 second', created_at = now() - interval '11 minutes'
		WHERE status = 'pending'`)
	e.do(call{method: "POST", path: deviceLookup, token: web, body: map[string]string{"user_code": late.UserCode},
		header: map[string]string{"Idempotency-Key": ""}}).ok(http.StatusOK, &got)
	if got.State != "expired" {
		t.Fatalf("expired lookup: %+v", got)
	}
	e.do(call{method: "POST", path: deviceApprove, token: web, body: map[string]string{"user_code": late.UserCode},
		sign: webSigned(zz, tamper{})}).fails(http.StatusConflict, "invalid_transition")
}

// TestOnlyAPersonOnTheWebConfirmsADevice: the CLI's own session (signed
// through the proxy or not), a web token without the proxy's signature,
// an agent's key and an impersonation are all refused; a CLI session may
// read the code, so the page can say who asks.
func TestOnlyAPersonOnTheWebConfirmsADevice(t *testing.T) {
	t.Parallel()
	e, store := newDeviceEnv(t)
	zz := e.user("zz")
	code := newCode(t, store)
	body := map[string]string{"user_code": code.UserCode}
	approve := func(token string, sign func(*http.Request, []byte)) *resp {
		return e.do(call{method: "POST", path: deviceApprove, token: token, body: body, sign: sign})
	}

	// The session a device code mints is a CLI one: with it, no device is
	// ever confirmed, signed through the web proxy or not.
	cli := e.cliSession(zz)
	for _, sign := range []func(*http.Request, []byte){nil, webSigned(zz, tamper{})} {
		if code := policyCode(t, approve(cli, sign)); code != "device_needs_web" {
			t.Fatalf("a CLI session: %s, want device_needs_web", code)
		}
	}
	if code := policyCode(t, approve(e.webSession(zz), nil)); code != "device_needs_web" {
		t.Fatalf("a web token without the proxy's signature: %s", code)
	}
	if code := policyCode(t, e.do(call{method: "POST", path: deviceDeny, token: cli, body: body})); code != "device_needs_web" {
		t.Fatalf("decline from the CLI: %s", code)
	}
	var got deviceView
	e.do(call{method: "POST", path: deviceLookup, token: cli, body: body, header: map[string]string{"Idempotency-Key": ""}}).ok(http.StatusOK, &got)

	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	if code := policyCode(t, approve(key, nil)); code != "device_by_person" {
		t.Fatalf("an API key: %s", code)
	}
	if code := policyCode(t, e.do(call{method: "POST", path: deviceLookup, token: key, body: body,
		header: map[string]string{"Idempotency-Key": ""}})); code != "device_by_person" {
		t.Fatalf("an API key reading: %s", code)
	}
	grant, _ := e.grant(zz, "claude-code", []string{"memory:read", "memory:write"})
	if code := policyCode(t, approve(grant, webSigned(zz, tamper{}))); code != "device_by_person" {
		t.Fatalf("an OAuth grant: %s", code)
	}
	imp, err := auth.SignImpersonationToken(zz.String(), uuid.NewString(), []byte(testSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	approve(imp, webSigned(zz, tamper{})).fails(http.StatusForbidden, "impersonation_read_only")
	e.do(call{method: "POST", path: deviceApprove, token: e.webSession(zz), body: body, sign: webSigned(zz, tamper{}),
		header: map[string]string{"Idempotency-Key": ""}, invalid: true}).fails(http.StatusBadRequest, "idempotency_key_required")

	// Nothing above confirmed it.
	if _, err := store.Poll(context.Background(), deviceauth.ClientCLI, code.DeviceCode); !errors.Is(err, deviceauth.ErrPending) {
		t.Fatalf("after the refusals: %v, want still pending", err)
	}
}

// TestGuessingDeviceCodesWaits: codes that don't exist, named too often,
// make the person wait, even for a real code.
func TestGuessingDeviceCodesWaits(t *testing.T) {
	t.Parallel()
	e, store := newDeviceEnv(t)
	zz := e.user("zz")
	web := e.webSession(zz)
	lookup := func(code string) *resp {
		return e.do(call{method: "POST", path: deviceLookup, token: web, body: map[string]string{"user_code": code},
			header: map[string]string{"Idempotency-Key": ""}})
	}
	guesses := []string{"BCDF-0000", "BCDF-0001", "BCDF-0002", "BCDF-0003", "BCDF-0004",
		"BCDF-0005", "BCDF-0006", "BCDF-0007", "not a code", "BCDF-0009"}
	for _, g := range guesses {
		lookup(g).fails(http.StatusNotFound, "not_found")
	}
	real := newCode(t, store)
	r := lookup(real.UserCode)
	r.fails(http.StatusTooManyRequests, "rate_limited")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	// Confirming waits too: guessing a code to confirm is the same guess.
	e.do(call{method: "POST", path: deviceApprove, token: web, body: map[string]string{"user_code": real.UserCode},
		sign: webSigned(zz, tamper{})}).fails(http.StatusTooManyRequests, "rate_limited")
	// Guesses count by address as well as by person: jy, asking from the
	// same address, waits too.
	jy := e.user("jy")
	e.do(call{method: "POST", path: deviceLookup, token: e.webSession(jy), body: map[string]string{"user_code": real.UserCode},
		header: map[string]string{"Idempotency-Key": ""}}).fails(http.StatusTooManyRequests, "rate_limited")
}
