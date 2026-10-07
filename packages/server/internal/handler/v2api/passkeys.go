package v2api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
)

// The passkey re-check (plan 25 §5.15; internal/passkeys,
// internal/websurface/THREAT_MODEL.md).
//
// A person with a passkey makes the decisions that need them with it:
// policy refuses such a request with needs_passkey, and on the web this
// package answers 403 needs_passkey with a challenge bound to the person,
// their session (the access token's sid) and the request itself
// (actionHash: method, target, Idempotency-Key, If-Match and the body's
// SHA-256). The web app asks for the passkey and sends the very same
// request again with the answer in X-Memax-Passkey. principalFor verifies
// it before anything runs: for this person, from this session, for this
// request, unexpired, unused, user verified. Only then is the actor
// Verified, and a person's Keep on the web human_web_verified. An answer
// is spent whether the command then goes through or not; a retry after a
// lost response asks again, unless the command was applied, in which case
// the idempotent replay answers before policy is asked.
//
// Elsewhere (the CLI, an in-agent answer) there is no challenge: the
// refusal says to do it on the web, and an assertion such a client sends
// is refused, so the CLI never reaches human_web_verified.

// HeaderPasskey carries the answer to a re-check: the
// AuthenticationResponseJSON, base64url.
const HeaderPasskey = "X-Memax-Passkey"

// maxAssertion bounds the header (a real one is about 1 KiB).
const maxAssertion = 16 << 10

// WithPasskeys turns passkeys on: sign-in, Settings › Account's passkeys,
// and the re-check. nil (the default) leaves every decision at human_web.
func WithPasskeys(s *passkeys.Service) Option { return func(h *Handler) { h.passkeys = s } }

// reqState is what one request learns on its way through: the principal
// and, on the web, the hash a re-check would be bound to. serve puts it in
// the request's context, so a refusal written later can issue the
// challenge without the handler threading it through.
type reqState struct {
	p      *principal
	action string
}

type reqStateKey struct{}

func withState(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), reqStateKey{}, &reqState{}))
}

func stateOf(r *http.Request) *reqState {
	s, _ := r.Context().Value(reqStateKey{}).(*reqState)
	return s
}

// actionHash names the request a re-check confirms: the lowercase hex
// SHA-256 of its method, target (escaped path and raw query), the
// Idempotency-Key and If-Match headers and the body's SHA-256, one per
// line. The body is read and put back for the handler.
func actionHash(r *http.Request) (string, error) {
	var body []byte
	if r.Body != nil && r.Body != http.NoBody {
		rest := r.Body
		b, err := io.ReadAll(io.LimitReader(rest, maxBody+1))
		// Whatever was read goes back in front of the rest, so the handler
		// reads (and limits) the body as it would have.
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(b), rest), rest}
		if err != nil {
			return "", err
		}
		if len(b) > maxBody {
			return "", errors.New("body too large")
		}
		body = b
	}
	target := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	bodySum := sha256.Sum256(body)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"memax-passkey-check/v1", strings.ToUpper(r.Method), target,
		r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match"), hex.EncodeToString(bodySum[:]),
	}, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// hasPasskey is the person's passkey lookup, started beside resolving
// their scope.
type hasPasskey struct {
	has bool
	err error
}

// startHasPasskey looks up, off the request's path, whether a person has a
// passkey, for a request that may change something (a GET never needs the
// re-check). nil when there is nothing to look up.
func (h *Handler) startHasPasskey(r *http.Request, person uuid.UUID, isAgent bool) <-chan hasPasskey {
	if h.passkeys == nil || isAgent || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	ch := make(chan hasPasskey, 1)
	go func() {
		has, err := h.passkeys.Has(r.Context(), person)
		ch <- hasPasskey{has, err}
	}()
	return ch
}

// passkeyFacts sets what the re-check needs on a person's principal:
// whether they have a passkey, the request's hash on the web, and Verified
// when the request carries an answer that verifies.
func (h *Handler) passkeyFacts(r *http.Request, person uuid.UUID, p *principal, has <-chan hasPasskey) *apiError {
	if has != nil {
		res := <-has
		if res.err != nil {
			h.log.ErrorContext(r.Context(), "v2: couldn't tell whether a person has a passkey", "user_id", person.String(), "error", res.err)
			return &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable, retryAfter: 2,
				message: "Memax couldn't check your passkeys just now. Try again in a moment.", details: &errorDetails{RetryAfter: 2}}
		}
		p.actor.Passkey = res.has
	}
	st := stateOf(r)
	if st != nil {
		st.p = p
	}
	raw := strings.TrimSpace(r.Header.Get(HeaderPasskey))
	if h.passkeys == nil || (raw == "" && (p.via != policy.ViaWeb || r.Method == http.MethodGet)) {
		return nil
	}
	action, err := actionHash(r)
	if raw == "" {
		// A request that can't be named (a body past the limit) just can't
		// be re-checked; its handler refuses the body anyway.
		if err == nil && st != nil {
			st.action = action
		}
		return nil
	}
	if err != nil {
		return invalidRequest("body", "The request body is too large.")
	}
	if st != nil {
		st.action = action
	}
	if p.via != policy.ViaWeb {
		return &apiError{status: http.StatusForbidden, code: codePermissionDenied,
			message: "A passkey check counts only on memax.app. Make this change there."}
	}
	invalid := func(reason passkeys.Reason, msg string) *apiError {
		return &apiError{status: http.StatusForbidden, code: codePasskeyInvalid, message: msg,
			details: &errorDetails{PasskeyFailure: reason}}
	}
	if len(raw) > maxAssertion {
		return invalid(passkeys.ReasonMalformed, "That passkey answer is too long. Try again.")
	}
	assertion, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
	if err != nil {
		return invalid(passkeys.ReasonMalformed, "That passkey answer isn't readable. Try again.")
	}
	sid, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	if sid == uuid.Nil {
		return invalid(passkeys.ReasonSessionNeeded, "Sign in again to confirm with your passkey: this session has no name Memax can bind it to.")
	}
	if h.sessions != nil {
		s, err := h.sessions.Get(r.Context(), person, sid)
		switch {
		case errors.Is(err, sessions.ErrNotFound) || (err == nil && s.RevokedAt != nil):
			return invalid(passkeys.ReasonSessionEnded, "This session was signed out. Sign in again.")
		case err != nil:
			return h.fromLedger(r, err)
		}
	}
	used, err := h.passkeys.VerifyCheck(r.Context(), passkeys.Binding{Person: person, Session: sid, Action: action}, assertion)
	if reason := passkeys.ReasonOf(err); reason != "" {
		h.log.WarnContext(r.Context(), "v2: refused a passkey check", "reason", string(reason), "user_id", person.String(),
			"method", r.Method, "path", r.URL.Path)
		return invalid(reason, passkeyFailureMessage(reason))
	}
	if err != nil {
		return h.fromLedger(r, err)
	}
	p.actor.Verified = true
	h.log.InfoContext(r.Context(), "v2: a passkey check verified", "user_id", person.String(), "passkey_id", used.ID.String(),
		"method", r.Method, "path", r.URL.Path)
	return nil
}

// passkeyFailureMessage says what to do about a refused answer.
func passkeyFailureMessage(r passkeys.Reason) string {
	switch r {
	case passkeys.ReasonExpired:
		return "That passkey check took too long. Try again."
	case passkeys.ReasonUsed:
		return "That passkey check was already used. Try again, and confirm once more."
	case passkeys.ReasonOtherSession, passkeys.ReasonOtherRequest, passkeys.ReasonUnknown:
		return "That passkey check was for something else. Try again."
	case passkeys.ReasonNoCredential:
		return "That passkey isn't on your account. Use one listed in Settings › Account."
	case passkeys.ReasonNotVerified:
		return "Your passkey didn't verify you (a fingerprint, face or PIN). Try again and unlock it."
	case passkeys.ReasonCloned:
		return "That passkey's counter went backwards, which a copied key does. Use another passkey, and remove this one in Settings › Account."
	case passkeys.ReasonSessionEnded, passkeys.ReasonSessionNeeded:
		return "Sign in again to confirm with your passkey."
	}
	return "Your passkey's answer didn't verify. Try again."
}

// passkeyChallenge is the PasskeyChallenge schema.
type passkeyChallenge struct {
	Options   *passkeys.RequestOptions `json:"options"`
	ExpiresAt time.Time                `json:"expires_at"`
}

// writeRefusal answers a policy refusal: 403 refused with the decision,
// or, when the decision asks a person on the web for their passkey, 403
// needs_passkey with a challenge bound to this request.
func (h *Handler) writeRefusal(w http.ResponseWriter, r *http.Request, d policy.Decision) {
	if d.Code == policy.CodeNeedsPasskey {
		if e := h.passkeyCheck(r, d); e != nil {
			writeError(w, e)
			return
		}
	}
	writeError(w, refusal(d))
}

// passkeyCheck issues the challenge for a needs_passkey refusal, or
// answers nil when this request can't be checked (not on the web, no
// session named, passkeys off): the refusal then stands as it is.
func (h *Handler) passkeyCheck(r *http.Request, d policy.Decision) *apiError {
	st := stateOf(r)
	if h.passkeys == nil || st == nil || st.p == nil || st.p.via != policy.ViaWeb || st.action == "" {
		return nil
	}
	sid, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	if sid == uuid.Nil {
		return nil
	}
	opts, expires, err := h.passkeys.BeginCheck(r.Context(), passkeys.Binding{Person: st.p.actor.ID, Session: sid, Action: st.action})
	if passkeys.ReasonOf(err) == passkeys.ReasonNoPasskey {
		return nil
	}
	if err != nil {
		return h.fromLedger(r, err)
	}
	return &apiError{status: http.StatusForbidden, code: codeNeedsPasskey, message: d.Message,
		details: &errorDetails{Policy: &d, Passkey: &passkeyChallenge{Options: opts, ExpiresAt: expires}}}
}

// previewActor is the actor a preview (forget-preview) decides for: it
// writes nothing, so it loads whether the person has a passkey (a GET
// doesn't), and on the web it decides as if they had confirmed with it,
// since the web app asks for the passkey when they go ahead. Elsewhere it
// says what the CLI would get: needs_passkey.
func (h *Handler) previewActor(r *http.Request, p *principal) (ledger.Actor, *apiError) {
	a := p.actor
	if h.passkeys == nil || a.Kind != policy.ActorPerson {
		return a, nil
	}
	has, err := h.passkeys.Has(r.Context(), a.ID)
	if err != nil {
		return a, h.fromLedger(r, err)
	}
	a.Passkey = has
	if has && p.via == policy.ViaWeb {
		a.Verified = true
	}
	return a, nil
}
