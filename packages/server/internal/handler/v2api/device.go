package v2api

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/ratelimit"
)

// A person confirming the memax CLI's device code on the web (CliAuth,
// /device; plan 25 §5.15). The CLI asks for the code and collects its
// session over OAuth (/oauth/device_authorization and /oauth/token,
// internal/handler/oauth_device.go); confirming is a /v2 operation so it
// is spec-checked, typed in the SDK and signed by the web app's proxy:
// only a person on the web (human_web) confirms or declines a code
// (policy.DecideDevice). The codes live in internal/deviceauth, outside
// the record: confirming one writes no receipt.

// WithDevices serves device-code confirmation from store; nil (the
// default) answers 503.
func WithDevices(store *deviceauth.Store) Option {
	return func(h *Handler) { h.devices = store }
}

// Devices is the device-code store, for the OAuth endpoints that issue
// and redeem the codes (nil when device sign-in is off).
func (h *Handler) Devices() *deviceauth.Store {
	if h == nil {
		return nil
	}
	return h.devices
}

// Guessing codes: a person or an address that names this many codes that
// don't exist within the window waits. A code is about 30 bits and lives
// 10 minutes, so this keeps a guess at any live code below one in a
// million a day per account and per address.
const (
	deviceMissLimit  = 10
	deviceMissWindow = 10 * time.Minute
)

type deviceCodeRequest struct {
	UserCode string `json:"user_code"`
}

// deviceAuthorization is the DeviceAuthorization schema.
type deviceAuthorization struct {
	UserCode      string           `json:"user_code"`
	State         deviceauth.State `json:"state"`
	ClientID      string           `json:"client_id"`
	ClientVersion string           `json:"client_version,omitempty"`
	DeviceName    string           `json:"device_name,omitempty"`
	DeviceOS      string           `json:"device_os,omitempty"`
	Space         string           `json:"space,omitempty"`
	Address       string           `json:"address,omitempty"`
	RequestedAt   time.Time        `json:"requested_at"`
	ExpiresAt     time.Time        `json:"expires_at"`
	DecidedAt     *time.Time       `json:"decided_at,omitempty"`
	SignedInAt    *time.Time       `json:"signed_in_at,omitempty"`
}

func toDeviceAuthorization(r *deviceauth.Request, now time.Time) deviceAuthorization {
	return deviceAuthorization{
		UserCode: r.UserCode, State: r.StateAt(now), ClientID: r.ClientID, ClientVersion: r.ClientVersion,
		DeviceName: r.DeviceName, DeviceOS: r.DeviceOS, Space: r.Space, Address: r.IP,
		RequestedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, DecidedAt: r.DecidedAt, SignedInAt: r.ConsumedAt,
	}
}

// POST /v2/device-authorizations:lookup
func (h *Handler) lookupDevice(w http.ResponseWriter, r *http.Request) {
	h.device(w, r, policy.DeviceLookup)
}

// POST /v2/device-authorizations:approve
func (h *Handler) approveDevice(w http.ResponseWriter, r *http.Request) {
	h.device(w, r, policy.DeviceApprove)
}

// POST /v2/device-authorizations:deny
func (h *Handler) denyDevice(w http.ResponseWriter, r *http.Request) {
	h.device(w, r, policy.DeviceDeny)
}

func (h *Handler) device(w http.ResponseWriter, r *http.Request, act policy.DeviceAction) {
	if h.devices == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Device sign-in isn't configured on this server. Run memax login where a browser can open."})
		return
	}
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if act != policy.DeviceLookup {
		if _, e := p.command(r); e != nil {
			writeError(w, e)
			return
		}
	}
	actor := policy.Actor{Kind: p.actor.Kind, Credential: p.actor.Credential, Via: p.via}
	if d := policy.DecideDevice(actor, act); d.Effect == policy.EffectRefuse {
		writeError(w, refusal(d))
		return
	}
	var req deviceCodeRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	code := strings.TrimSpace(req.UserCode)
	if code == "" || len(code) > 32 {
		writeError(w, invalidRequest("user_code", "Send the code your terminal shows, like WQRT-4821."))
		return
	}
	keys := []string{"person:" + p.actor.ID.String()}
	if ip := ratelimit.ClientIP(r); ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	now := h.now()
	if wait, blocked := h.deviceMisses.blocked(keys, now); blocked {
		writeError(w, &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, retryAfter: wait,
			message: "Too many codes that don't exist. Check the code in your terminal and try again in a few minutes.",
			details: &errorDetails{RetryAfter: wait}})
		return
	}

	var (
		found *deviceauth.Request
		err   error
	)
	switch act {
	case policy.DeviceApprove:
		found, err = h.devices.Approve(r.Context(), code, p.actor.ID)
	case policy.DeviceDeny:
		found, err = h.devices.Deny(r.Context(), code, p.actor.ID)
	default:
		found, err = h.devices.Find(r.Context(), code, p.actor.ID)
	}
	var decided *deviceauth.DecidedError
	switch {
	case errors.Is(err, deviceauth.ErrNotFound):
		h.deviceMisses.add(keys, now)
		writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "No code like that is waiting. Check the code in your terminal; codes last 10 minutes."})
		return
	case errors.As(err, &decided):
		writeError(w, &apiError{status: http.StatusConflict, code: codeInvalidTransition,
			message: deviceStateMessage(decided.State), details: &errorDetails{State: decided.State}})
		return
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, toDeviceAuthorization(found, h.devices.Now()))
}

func deviceStateMessage(s deviceauth.State) string {
	switch s {
	case deviceauth.StateExpired:
		return "This code expired. Run memax login in your terminal for a new one."
	case deviceauth.StateDenied:
		return "You declined this code, so nothing was signed in. Run memax login in your terminal for a new one."
	case deviceauth.StateApproved, deviceauth.StateSignedIn:
		return "You already confirmed this code. Your terminal signs in with it."
	}
	return "This code was already decided."
}

// deviceMisses counts codes that named nothing, per key, in process (each
// API machine counts its own; the route's rate limit is global).
type deviceMisses struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

func (m *deviceMisses) blocked(keys []string, now time.Time) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wait := 0
	for _, k := range keys {
		ts := m.prune(k, now)
		if len(ts) >= deviceMissLimit {
			w := int(ts[0].Add(deviceMissWindow).Sub(now)/time.Second) + 1
			wait = max(wait, w)
		}
	}
	return wait, wait > 0
}

func (m *deviceMisses) add(keys []string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil || len(m.seen) > 10_000 {
		m.seen = map[string][]time.Time{}
	}
	for _, k := range keys {
		m.seen[k] = append(m.prune(k, now), now)
	}
}

// prune drops misses older than the window; call with mu held.
func (m *deviceMisses) prune(k string, now time.Time) []time.Time {
	ts := m.seen[k]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= deviceMissWindow {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(m.seen, k)
		return nil
	}
	m.seen[k] = ts
	return ts
}
