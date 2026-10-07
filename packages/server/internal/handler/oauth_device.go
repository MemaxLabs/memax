package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
)

// The device authorization grant's protocol endpoints (RFC 8628; plan 25
// §5.15), beside the rest of the OAuth server: the memax CLI asks for a
// code here and polls the token endpoint with it, while the person
// confirms the code on the web (/v2/device-authorizations:approve,
// internal/handler/v2api). These answer in OAuth's own JSON, not the
// ApiResponse envelope, as /oauth/token does.
//
// They live under /oauth rather than /v2 because they are the OAuth
// server's: the token endpoint is shared with the MCP clients' grants,
// the server metadata advertises both, and a client written to RFC 8628
// finds them there. Only the person's confirmation, which needs a signed-in
// session and the web app's signature, is a /v2 operation.

// SetDeviceAuth turns the device grant on: codes from store, the client's
// address read by clientIP (the rate limiter's, which knows which proxy
// header to believe). A nil store leaves it off.
func (h *MCPOAuthHandler) SetDeviceAuth(store *deviceauth.Store, clientIP func(*http.Request) string) {
	h.device = store
	h.clientIP = clientIP
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceAuthorization serves POST /oauth/device_authorization (RFC 8628
// §3.1–3.2). Form fields: client_id (memax-cli), and what the CLI says
// about itself for the confirmation page: client_version, device_name,
// device_os, space.
func (h *MCPOAuthHandler) DeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if h.device == nil {
		oauthJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "temporarily_unavailable", "error_description": "Device sign-in needs a database. Use memax login in a browser.",
		})
		return
	}
	if err := r.ParseForm(); err != nil {
		deviceError(w, "invalid_request", "Send the request as a form (application/x-www-form-urlencoded).")
		return
	}
	ip := ""
	if h.clientIP != nil {
		ip = h.clientIP(r)
	}
	issued, err := h.device.Create(r.Context(), deviceauth.Start{
		ClientID:      r.PostFormValue("client_id"),
		ClientVersion: r.PostFormValue("client_version"),
		DeviceName:    r.PostFormValue("device_name"),
		DeviceOS:      r.PostFormValue("device_os"),
		Space:         r.PostFormValue("space"),
		IP:            ip,
		UserAgent:     r.UserAgent(),
	})
	switch {
	case errors.Is(err, deviceauth.ErrUnknownClient):
		oauthJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid_client", "error_description": "Only the memax CLI signs in with a device code (client_id memax-cli).",
		})
		return
	case errors.Is(err, deviceauth.ErrRateLimited):
		w.Header().Set("Retry-After", strconv.Itoa(int(deviceauth.PerIPWindow/time.Second)))
		oauthJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "slow_down", "error_description": "Too many sign-in codes from this address. Try again in an hour, or run memax login where a browser can open.",
		})
		return
	case err != nil:
		slog.Error("device authorization failed", "error", err)
		oauthJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "server_error", "error_description": "Memax couldn't issue a code. Try again.",
		})
		return
	}
	resp := deviceAuthorizationResponse{
		DeviceCode: issued.DeviceCode,
		UserCode:   issued.UserCode,
		ExpiresIn:  int(issued.Request.ExpiresAt.Sub(issued.Request.CreatedAt) / time.Second),
		Interval:   issued.Request.Interval,
	}
	if app := h.resolveAppBaseURL(r); app != "" {
		resp.VerificationURI = app + "/device"
		resp.VerificationURIComplete = app + "/device?code=" + url.QueryEscape(issued.UserCode)
	} else {
		resp.VerificationURI = h.resolveBaseURL(r) + "/device"
	}
	slog.Info("device authorization issued", "client_version", issued.Request.ClientVersion,
		"device_os", issued.Request.DeviceOS, "client_ip", ip)
	oauthJSON(w, http.StatusOK, resp)
}

// tokenDeviceCode is the device_code grant of POST /oauth/token (RFC 8628
// §3.4–3.5): the CLI polling with its device code. Once a person confirmed
// the code, it answers that person's CLI session, once; until then
// authorization_pending (or slow_down, polling too fast), and
// access_denied, expired_token or invalid_grant when it never will.
func (h *MCPOAuthHandler) tokenDeviceCode(w http.ResponseWriter, r *http.Request) {
	if h.device == nil {
		oauthJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	user, err := h.device.Poll(r.Context(), r.PostFormValue("client_id"), r.PostFormValue("device_code"))
	switch {
	case errors.Is(err, deviceauth.ErrPending):
		deviceError(w, "authorization_pending", "Waiting for you to confirm the code at memax.app/device.")
		return
	case errors.Is(err, deviceauth.ErrSlowDown):
		deviceError(w, "slow_down", "Polling too fast: wait 5 seconds longer between polls.")
		return
	case errors.Is(err, deviceauth.ErrExpired):
		deviceError(w, "expired_token", "The code expired. Run memax login again for a new one.")
		return
	case errors.Is(err, deviceauth.ErrDenied):
		deviceError(w, "access_denied", "The code was declined in the browser. Nothing was signed in.")
		return
	case errors.Is(err, deviceauth.ErrUnknownClient):
		oauthJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid_client", "error_description": "Only the memax CLI signs in with a device code.",
		})
		return
	case errors.Is(err, deviceauth.ErrUsed):
		deviceError(w, "invalid_grant", "That device code was already used, or isn't one. Run memax login again.")
		return
	case err != nil:
		slog.Error("device token poll failed", "error", err)
		oauthJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "server_error", "error_description": "Memax couldn't check the code. Keep polling.",
		})
		return
	}
	// A person's CLI session: its tokens carry surface cli, so nothing done
	// with them is human_web, however the person confirmed the code.
	tokens, err := h.authH.issueTokens(user.String())
	if err != nil {
		slog.Error("device token issuance failed", "error", err)
		oauthJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "server_error", "error_description": "Memax couldn't issue the session. Run memax login again.",
		})
		return
	}
	track(user.String(), "api.auth.login", map[string]any{"provider": "device"})
	oauthJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"refresh_token": tokens.RefreshToken,
	})
}

// deviceError is a 400 OAuth error, never cached.
func deviceError(w http.ResponseWriter, code, desc string) {
	oauthJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": desc})
}

// oauthJSON writes an OAuth protocol response, never cached (RFC 6749 §5.1).
func oauthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
