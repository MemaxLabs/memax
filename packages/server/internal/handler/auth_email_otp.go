package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Email OTP — a third sign-in path alongside GitHub + Google OAuth.
//
// Two-step flow:
//   - POST /v1/auth/email/request {email, redirect_uri?, invite_token?}
//     → server stores a hashed 6-digit code keyed by canonical email,
//       enqueues a transactional email via the existing email pipeline.
//     → response is generic (200 + expires_in) regardless of whether
//       the email maps to an eligible registration — matches the
//       OAuth callbacks, which defer all gating to the post-auth step.
//   - POST /v1/auth/email/verify {email, code}
//     → server looks up the most recent active row, hashes the
//       attempt, compares, and on success runs the identical
//       loginOrCreateUser path the OAuth callbacks use. Registration
//       mode + invite consumption work cohesively because the gate
//       is shared.
//
// Security model:
//   - Plaintext code never persisted: row stores sha256(code || ":"
//     || canonical_email || ":" || JWT_SECRET) hex. The JWT secret is
//     the pepper, so a leaked DB snapshot alone can't be brute-forced.
//   - 6 attempts cap per code, then verify returns code_locked and
//     the row stays consumed-by-attempts so a fresh request is needed.
//   - 10-minute TTL on every code.
//   - Per-IP RPM bound at the route via ratelimit.IPEmailOTPRequest;
//     per-email RPM bound here in the handler (3/hour). The two
//     layers together kill both single-source spam and distributed
//     inbox bombing of a target email.
//   - Constant-time hash compare prevents timing oracles.

const (
	emailOTPLength         = 6
	emailOTPTTL            = 10 * time.Minute
	emailOTPMaxAttempts    = 6
	emailOTPPerEmailWindow = time.Hour
	emailOTPPerEmailMax    = 3
	emailOTPPerIPWindow    = time.Hour
	emailOTPPerIPMax       = 10
	emailOTPProvider       = "email"
)

// emailOTPRequest is the public request body for POST /v1/auth/email/request.
type emailOTPRequest struct {
	Email       string `json:"email"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	InviteToken string `json:"invite_token,omitempty"`
}

// emailOTPRequestResponse is what RequestEmailOTP echoes back. We
// intentionally do not include whether the email is registered — the
// response shape is the same for all inputs.
type emailOTPRequestResponse struct {
	Status    string `json:"status"`             // "sent"
	ExpiresIn int    `json:"expires_in"`         // seconds, == emailOTPTTL
	Email     string `json:"email"`              // echoed canonical email, useful for the UI
	Cooldown  int    `json:"cooldown,omitempty"` // suggested seconds before resend; advisory
	DevCode   string `json:"dev_code,omitempty"` // present ONLY when MEMAX_EMAIL_OTP_DEV_RETURN=1 — local dev convenience
}

// emailOTPVerifyRequest is the public request body for POST /v1/auth/email/verify.
type emailOTPVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// RequestEmailOTP issues a sign-in code for the given email and
// enqueues the OTP email. Response is uniform — no signal on
// existence or eligibility. Gating happens at Verify time so the
// codepath stays identical to the OAuth callbacks.
//
// POST /v1/auth/email/request
func (h *AuthHandler) RequestEmailOTP(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
			Error: &model.Error{Code: "auth_disabled", Message: "Email sign-in is unavailable: no store configured."},
		})
		return
	}
	if h.enqueueEmail == nil {
		// Without an enqueue path the code would land in a row but
		// the user would never receive it. Fail fast on the configured
		// dimension so ops sees the cause.
		writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
			Error: &model.Error{Code: "auth_disabled", Message: "Email sign-in is unavailable: email delivery not configured."},
		})
		return
	}

	var req emailOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse request body.")
		return
	}

	canonical, ok := canonicalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address.")
		return
	}

	// Validate redirect URI through the same allowlist used by the
	// OAuth start endpoints. Keeps the open-redirect surface uniform.
	clientRedirect := strings.TrimSpace(req.RedirectURI)
	if !h.isAllowedRedirect(clientRedirect) {
		writeError(w, http.StatusBadRequest, "invalid_redirect", "Redirect URI is not allowed.")
		return
	}

	// Invite token, if present, was passed as a separate field or
	// (matching OAuth) embedded as ?invite=... in the redirect URI.
	// Accept either source so the web/CLI surfaces can pick whichever
	// is cleaner for their UX.
	inviteToken := strings.TrimSpace(req.InviteToken)
	if inviteToken == "" {
		inviteToken = extractInviteToken(clientRedirect)
	}

	ctx := r.Context()
	clientIP := otpClientIP(r)
	userAgent := truncateOTPUserAgent(r.UserAgent())

	// Per-email rate limit. 3/hour matches a legitimate user who
	// retries twice plus one resend, while making inbox-bomb
	// campaigns expensive against any one target.
	if since := time.Now().Add(-emailOTPPerEmailWindow); true {
		count, err := h.store.CountRecentEmailOTPCodesForEmail(ctx, canonical, since)
		if err != nil {
			slog.Error("email otp per-email rate check failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "Failed to issue code.")
			return
		}
		if count >= emailOTPPerEmailMax {
			writeJSON(w, http.StatusTooManyRequests, model.ApiResponse{
				Error: &model.Error{Code: "rate_limited", Message: "Too many sign-in codes requested for this email. Try again in an hour."},
			})
			return
		}
	}
	// Per-IP DB-backed limit. Layered on top of ratelimit.IPEmailOTPRequest
	// because the route-level limiter is in-memory per-process; with two
	// API replicas a 5 RPM cap becomes 10. The DB count is global and
	// covers windows longer than the in-memory bucket's bookkeeping.
	if clientIP != "" {
		since := time.Now().Add(-emailOTPPerIPWindow)
		count, err := h.store.CountRecentEmailOTPCodesForIP(ctx, clientIP, since)
		if err != nil {
			slog.Error("email otp per-ip rate check failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "Failed to issue code.")
			return
		}
		if count >= emailOTPPerIPMax {
			writeJSON(w, http.StatusTooManyRequests, model.ApiResponse{
				Error: &model.Error{Code: "rate_limited", Message: "Too many sign-in codes requested. Try again later."},
			})
			return
		}
	}

	code, err := generateEmailOTPCode()
	if err != nil {
		slog.Error("email otp code generation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to issue code.")
		return
	}
	now := time.Now()
	hashed := hashEmailOTPCode(code, canonical, h.jwtSecret)
	row := model.EmailOTPCode{
		Email:          canonical,
		CodeHash:       hashed,
		Purpose:        "login",
		MaxAttempts:    emailOTPMaxAttempts,
		ClientRedirect: clientRedirect,
		InviteToken:    inviteToken,
		RequestIP:      clientIP,
		UserAgent:      userAgent,
		CreatedAt:      now,
		ExpiresAt:      now.Add(emailOTPTTL),
	}
	if err := h.store.CreateEmailOTPCode(ctx, row); err != nil {
		slog.Error("email otp persist failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to issue code.")
		return
	}

	// Enqueue email through the same River queue used for waitlist /
	// hub invites. Failure here is loud — the row was already
	// created, so a queue error means we'd hand back a code that
	// won't arrive. Return 502 to surface that to clients.
	vars := map[string]string{
		"Code":             code,
		"ExpiresInMinutes": fmt.Sprintf("%d", int(emailOTPTTL/time.Minute)),
		"RequestContext":   otpRequestContext(clientIP),
	}
	if err := h.enqueueEmail("email_otp", canonical, vars); err != nil {
		slog.Error("email otp enqueue failed", "error", err)
		writeJSON(w, http.StatusBadGateway, model.ApiResponse{
			Error: &model.Error{Code: "email_send_failed", Message: "Could not deliver the sign-in code. Please try again."},
		})
		return
	}

	slog.Info("email otp issued",
		"email", canonical,
		"client_ip", clientIP,
		"has_invite", inviteToken != "",
		"redirect_present", clientRedirect != "")

	resp := emailOTPRequestResponse{
		Status:    "sent",
		ExpiresIn: int(emailOTPTTL / time.Second),
		Email:     canonical,
		Cooldown:  30,
	}
	if h.emailOTPDevReturn() {
		resp.DevCode = code
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// VerifyEmailOTP consumes a sign-in code and runs the shared
// loginOrCreateUser path. Registration mode + invite consumption
// flow through unchanged.
//
// POST /v1/auth/email/verify
func (h *AuthHandler) VerifyEmailOTP(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
			Error: &model.Error{Code: "auth_disabled", Message: "Email sign-in is unavailable: no store configured."},
		})
		return
	}

	var req emailOTPVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse request body.")
		return
	}

	canonical, ok := canonicalizeEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address.")
		return
	}
	submitted := strings.TrimSpace(req.Code)
	if !validEmailOTPCode(submitted) {
		writeError(w, http.StatusBadRequest, "invalid_code_format", "Enter the 6-digit code from your email.")
		return
	}

	ctx := r.Context()
	row, err := h.store.GetActiveEmailOTPCode(ctx, canonical)
	if err != nil {
		if errors.Is(err, store.ErrEmailOTPCodeNotFound) {
			writeJSON(w, http.StatusBadRequest, model.ApiResponse{
				Error: &model.Error{Code: "code_expired", Message: "Your sign-in code has expired or was already used. Please request a new one."},
			})
			return
		}
		slog.Error("email otp lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to verify code.")
		return
	}

	// Attempt cap: once the user has used all their tries, refuse
	// further attempts and force a new code request. The cap is
	// inclusive — emailOTPMaxAttempts counts all submissions
	// including the current one, so we check >= here so the user
	// gets exactly that many chances total.
	if row.Attempts >= row.MaxAttempts {
		writeJSON(w, http.StatusTooManyRequests, model.ApiResponse{
			Error: &model.Error{Code: "code_locked", Message: "Too many attempts. Please request a new sign-in code."},
		})
		return
	}

	attemptHash := hashEmailOTPCode(submitted, canonical, h.jwtSecret)
	// Constant-time compare. The hashes are hex-encoded fixed-length
	// (sha256 → 64 chars) so a byte-length difference is impossible
	// under normal operation; defensively compare regardless.
	if !constantTimeEqualString(attemptHash, row.CodeHash) {
		newAttempts, incErr := h.store.IncrementEmailOTPCodeAttempts(ctx, row.ID)
		if incErr != nil {
			slog.Error("email otp attempt increment failed", "error", incErr)
		}
		remaining := row.MaxAttempts - newAttempts
		if remaining < 0 {
			remaining = 0
		}
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{
				Code:    "code_invalid",
				Message: fmt.Sprintf("That code doesn't match. %d %s left.", remaining, pluralAttempts(remaining)),
			},
		})
		return
	}

	// Consume the row up-front so two concurrent verifies don't both
	// reach the login path. After this point any further attempt
	// against this code will hit code_expired.
	if err := h.store.ConsumeEmailOTPCode(ctx, row.ID); err != nil {
		slog.Error("email otp consume failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to consume code.")
		return
	}

	// Run the shared login path so registration mode, invite
	// consumption, hub-invite-as-authorization, auto-link, and
	// personal-hub bootstrap all behave identically to OAuth.
	// EmailVerified=true: control of the inbox is the proof.
	pu := providerUser{
		Provider:      emailOTPProvider,
		ProviderID:    canonical, // canonical email is stable per-account
		Email:         canonical,
		EmailVerified: true,
		Name:          "",
		AvatarURL:     "",
	}
	user, loginErr := h.loginOrCreateUser(ctx, pu, loginOpts{
		InviteToken:    row.InviteToken,
		ClientRedirect: row.ClientRedirect,
		// ProviderOrgMember stays false — an email-OTP user has no
		// provider-side org signal. In org_gated mode this means
		// only EXISTING users can sign in via email; new email-only
		// registrations are blocked exactly as the gate intends.
		ProviderOrgMember: false,
	})
	if loginErr != nil {
		switch {
		case errors.Is(loginErr, ErrRegistrationRequired):
			writeJSON(w, http.StatusForbidden, model.ApiResponse{
				Error: &model.Error{Code: "registration_required", Message: "This email isn't allowed to register. Ask for an invite from your admin."},
			})
			return
		case errors.Is(loginErr, ErrInvalidInvite):
			writeJSON(w, http.StatusForbidden, model.ApiResponse{
				Error: &model.Error{Code: "invalid_invite", Message: "Your invite is invalid or expired. Ask for a new one."},
			})
			return
		case errors.Is(loginErr, ErrInviteEmailMismatch):
			writeJSON(w, http.StatusForbidden, model.ApiResponse{
				Error: &model.Error{Code: "invite_email_mismatch", Message: "This invite was sent to a different email. Sign in with the email that received it."},
			})
			return
		default:
			slog.Error("email otp login failed", "error", loginErr, "email", canonical)
			writeError(w, http.StatusInternalServerError, "login_failed", "Failed to sign in. Please try again.")
			return
		}
	}

	tokens, err := h.issueTokens(user.ID)
	if err != nil {
		slog.Error("email otp token issuance failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to issue tokens.")
		return
	}

	track(user.ID, "api.auth.login", map[string]any{
		"provider": emailOTPProvider,
		"email":    user.Email,
		"name":     user.Name,
	})

	// Mirror OAuth's two-mode response: when a redirect was carried
	// through, mint an auth_code so the web client can complete login
	// via /v1/auth/exchange (same code path that powers the OAuth
	// callback page). Otherwise return tokens directly — used by
	// non-redirect clients like the CLI.
	if row.ClientRedirect != "" {
		authCode := generateToken()
		if _, err := h.pool.Exec(ctx,
			`INSERT INTO auth_codes (code, user_id, expires_at) VALUES ($1, $2, $3)`,
			authCode, user.ID, time.Now().Add(60*time.Second)); err != nil {
			slog.Error("failed to store auth code for email otp", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "Failed to issue auth code.")
			return
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"status":   "ok",
			"code":     authCode,
			"redirect": appendQueryParam(row.ClientRedirect, "code", authCode),
			"user_id":  user.ID,
		}})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tokens})
}

// --- helpers ---

// canonicalizeEmail normalizes whitespace + case and validates RFC 5322
// shape. Returns (canonical, true) on success or ("", false) otherwise.
// Plus-addressing is preserved (we lowercase the local part rather than
// stripping +tags) because some providers like Outlook do not treat
// plus-tags as aliases; collapsing them would conflate distinct
// mailboxes.
func canonicalizeEmail(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", false
	}
	// mail.ParseAddress accepts "Display <user@host>" but for sign-in
	// we want bare addresses only.
	canonical := strings.ToLower(strings.TrimSpace(addr.Address))
	if canonical == "" {
		return "", false
	}
	// Sanity check: an "@" must remain after parse, with non-empty
	// local + domain. Defense in depth — mail.ParseAddress is permissive.
	at := strings.LastIndex(canonical, "@")
	if at <= 0 || at == len(canonical)-1 {
		return "", false
	}
	return canonical, true
}

// generateEmailOTPCode returns a uniformly random N-digit numeric
// code as a zero-padded decimal string. Uses crypto/rand via
// big.Int.Rand so the distribution is unbiased.
func generateEmailOTPCode() (string, error) {
	// 10^N upper bound (exclusive). big.Int math keeps this exact.
	upper := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(emailOTPLength)), nil)
	n, err := rand.Int(rand.Reader, upper)
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	// %06d (or %0Nd if emailOTPLength changes) — pad with leading zeros.
	return fmt.Sprintf("%0*d", emailOTPLength, n), nil
}

// validEmailOTPCode reports whether s is exactly emailOTPLength
// decimal digits. Rejects anything else BEFORE the DB hit so a
// brute-forcer can't burn DB rows with junk.
func validEmailOTPCode(s string) bool {
	if len(s) != emailOTPLength {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hashEmailOTPCode produces a stable hash for storage/comparison. The
// JWT secret acts as a pepper so a leaked DB snapshot alone is not
// sufficient to brute-force codes via a precomputed table.
func hashEmailOTPCode(code, email string, pepper []byte) string {
	h := sha256.New()
	h.Write([]byte(code))
	h.Write([]byte{':'})
	h.Write([]byte(email))
	h.Write([]byte{':'})
	h.Write(pepper)
	return hex.EncodeToString(h.Sum(nil))
}

// constantTimeEqualString compares two hex hashes in constant time
// once their lengths match. Length divergence here means a bug — both
// inputs are sha256-hex (64 chars) — so the early length check is not
// a real side channel.
func constantTimeEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// otpClientIP extracts the canonical client IP for OTP audit. Mirrors
// the ratelimit package's logic but stays local — the OTP handler
// records IP into a DB row, which is a different concern from the
// in-memory rate-limit bucket (per-IP enforcement happens at the
// route layer). Conservative: never trust headers unless we're behind
// a known proxy mode. For now, RemoteAddr only — operators on Fly /
// behind XFF can change this in one place later.
func otpClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// truncateOTPUserAgent caps the recorded user agent at a sensible
// length so a hostile client can't bloat the row.
func truncateOTPUserAgent(ua string) string {
	const max = 256
	ua = strings.TrimSpace(ua)
	if len(ua) > max {
		return ua[:max]
	}
	return ua
}

// otpRequestContext renders the human-readable "Requested from X"
// line shown in the email body. We avoid leaking precise IPs to the
// user (they're typically meaningless) and instead surface a short
// classifier so a security-savvy reader can spot anomalies (e.g.
// "from a new device"). For now, generic copy.
func otpRequestContext(ip string) string {
	if ip == "" {
		return "an unknown device"
	}
	return "the device you used to request this code"
}

// emailOTPDevReturn reports whether the request endpoint should
// include the plaintext code in its response. Gated by an env var so
// it never leaks in production. Useful for local dev / handler tests
// where no email delivery is wired.
func (h *AuthHandler) emailOTPDevReturn() bool {
	v := strings.TrimSpace(os.Getenv("MEMAX_EMAIL_OTP_DEV_RETURN"))
	return v == "1" || strings.EqualFold(v, "true")
}

func pluralAttempts(n int) string {
	if n == 1 {
		return "attempt"
	}
	return "attempts"
}
