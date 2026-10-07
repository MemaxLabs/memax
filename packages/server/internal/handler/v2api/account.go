package v2api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
)

// Settings › Account (plan 25 §5.15, epic 2.6): you, how you sign in, your
// passkeys, signing in with one, and forgetting your account. Who may do
// what is policy.DecideAccount. None of it is part of a space's record, so
// it writes no receipt, except forgetting the account, whose Forgets do
// (through the ledger's ForgetAccountAs).

// Accounts is the identity side, in V1's tables: *handler.AuthHandler.
type Accounts interface {
	AccountProfile(ctx context.Context, user uuid.UUID) (name, email string, ids []handler.AccountIdentity, err error)
	SetAccountName(ctx context.Context, user uuid.UUID, name string) error
	AccountLinkURL(ctx context.Context, user uuid.UUID, provider, redirect string) (string, error)
	AllowedRedirect(redirect string) bool
	AccountUnlink(ctx context.Context, user uuid.UUID, provider string) error
	WebSignInCode(ctx context.Context, user uuid.UUID) (string, time.Duration, error)
}

// WithAccounts serves /v2/me/account and the sign-in methods from a; nil
// (the default) answers 503 there.
func WithAccounts(a Accounts) Option { return func(h *Handler) { h.accounts = a } }

// The wire shapes (v2.yaml).
type signInMethod struct {
	Method      string     `json:"method"`
	Connected   bool       `json:"connected"`
	Account     string     `json:"account,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
}

type passkeyView struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Provider       string     `json:"provider,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	BackupEligible bool       `json:"backup_eligible"`
	Synced         bool       `json:"synced"`
	Transports     []string   `json:"transports"`
}

func toPasskey(p passkeys.Passkey) passkeyView {
	return passkeyView{ID: p.ID, Name: p.Name, Provider: p.Provider, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt,
		BackupEligible: p.BackupEligible, Synced: p.BackedUp, Transports: nonNil(p.Transports)}
}

type accountSession struct {
	ID         *uuid.UUID    `json:"id,omitempty"`
	Surface    sessions.Kind `json:"surface,omitempty"`
	SignedInAt *time.Time    `json:"signed_in_at,omitempty"`
	FreshUntil *time.Time    `json:"fresh_until,omitempty"`
}

type accountView struct {
	ID            uuid.UUID      `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	Initials      string         `json:"initials"`
	SignInMethods []signInMethod `json:"sign_in_methods"`
	Passkeys      []passkeyView  `json:"passkeys"`
	PasskeyCheck  bool           `json:"passkey_check"`
	Session       accountSession `json:"session"`
}

type passkeyList struct {
	Items []passkeyView `json:"items"`
}

type passkeyRegistration struct {
	Options   *passkeys.CreationOptions `json:"options"`
	ExpiresAt time.Time                 `json:"expires_at"`
}

type accountForgotten struct {
	Spaces         int `json:"spaces"`
	Agents         int `json:"agents"`
	TeamSpacesKept int `json:"team_spaces_kept"`
	Sessions       int `json:"sessions"`
	Passkeys       int `json:"passkeys"`
}

type passkeySignIn struct {
	Code      string `json:"code"`
	ExpiresIn int    `json:"expires_in"`
}

// Initials are a person's stamp on receipts, as the web app's frame
// makes them: the first letters of their first and last names ("Ziyang
// Zeng" → ZZ), or a single name's first two letters, or their email's.
func Initials(name, email string) string {
	words := strings.Fields(name)
	var out []rune
	switch {
	case len(words) >= 2:
		out = []rune{firstRune(words[0]), firstRune(words[len(words)-1])}
	case len(words) == 1:
		out = []rune(words[0])
	default:
		out = []rune(strings.Split(email, "@")[0])
	}
	if len(out) > 2 {
		out = out[:2]
	}
	return strings.ToUpper(string(out))
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// accountActor is the principal as DecideAccount sees it.
func accountActor(p *principal) policy.Actor {
	return policy.Actor{Kind: p.actor.Kind, Credential: p.actor.Credential, Via: p.via,
		Passkey: p.actor.Passkey, Verified: p.actor.Verified}
}

// sessionOf is the session this request comes from, when its token names
// one that is still live.
func (h *Handler) sessionOf(r *http.Request, person uuid.UUID) (*sessions.Session, error) {
	sid, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	if sid == uuid.Nil || h.sessions == nil {
		return nil, nil
	}
	s, err := h.sessions.Get(r.Context(), person, sid)
	if errors.Is(err, sessions.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.RevokedAt != nil {
		return nil, nil
	}
	return s, nil
}

// freshUntil is until when s counts as a fresh sign-in, or nil once it
// doesn't.
func (h *Handler) freshUntil(s *sessions.Session) *time.Time {
	if s == nil {
		return nil
	}
	until := s.CreatedAt.Add(passkeys.EnrollWindow)
	if !h.now().Before(until) {
		return nil
	}
	return &until
}

// accountStart resolves the person and decides act. command requires an
// Idempotency-Key. It answers false after writing the response.
func (h *Handler) accountStart(w http.ResponseWriter, r *http.Request, act policy.AccountAction, command bool) (*principal, bool) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return nil, false
	}
	if command {
		if _, e := p.command(r); e != nil {
			writeError(w, e)
			return nil, false
		}
	}
	fresh := false
	if act == policy.AccountAddPasskey || act == policy.AccountConnectSignIn {
		s, err := h.sessionOf(r, p.actor.ID)
		if err != nil {
			writeError(w, h.fromLedger(r, err))
			return nil, false
		}
		fresh = h.freshUntil(s) != nil
	}
	if d := policy.DecideAccount(accountActor(p), act, fresh); d.Effect == policy.EffectRefuse {
		h.writeRefusal(w, r, d)
		return nil, false
	}
	return p, true
}

func (h *Handler) accountsOff(w http.ResponseWriter) bool {
	if h.accounts == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Accounts aren't configured on this server."})
		return true
	}
	return false
}

func (h *Handler) passkeysOff(w http.ResponseWriter) bool {
	if h.passkeys == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Passkeys aren't configured on this server (WEBAUTHN_RP_ID or APP_BASE_URL)."})
		return true
	}
	return false
}

// accountView reads the person's account.
func (h *Handler) account(r *http.Request, person uuid.UUID) (*accountView, *apiError) {
	name, email, ids, err := h.accounts.AccountProfile(r.Context(), person)
	if err != nil {
		return nil, h.fromLedger(r, err)
	}
	out := &accountView{ID: person, Name: name, Email: email, Initials: Initials(name, email),
		SignInMethods: []signInMethod{}, Passkeys: []passkeyView{}}
	if out.Name == "" {
		out.Name = strings.Split(email, "@")[0]
	}
	for _, method := range []string{"github", "google"} {
		m := signInMethod{Method: method}
		for _, id := range ids {
			if id.Provider == method {
				m.Connected, m.Account = true, id.Account
				if !id.LinkedAt.IsZero() {
					at := id.LinkedAt
					m.ConnectedAt = &at
				}
			}
		}
		out.SignInMethods = append(out.SignInMethods, m)
	}
	out.SignInMethods = append(out.SignInMethods, signInMethod{Method: "email", Connected: email != "", Account: email})
	if h.passkeys != nil {
		list, err := h.passkeys.List(r.Context(), person)
		if err != nil {
			return nil, h.fromLedger(r, err)
		}
		for _, p := range list {
			out.Passkeys = append(out.Passkeys, toPasskey(p))
		}
	}
	out.PasskeyCheck = len(out.Passkeys) > 0
	s, err := h.sessionOf(r, person)
	if err != nil {
		return nil, h.fromLedger(r, err)
	}
	if s != nil {
		id, at := s.ID, s.CreatedAt
		out.Session = accountSession{ID: &id, Surface: s.Kind, SignedInAt: &at, FreshUntil: h.freshUntil(s)}
	}
	return out, nil
}

// GET /v2/me/account
func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	if h.accountsOff(w) {
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountView, false)
	if !ok {
		return
	}
	out, e := h.account(r, p.actor.ID)
	if e != nil {
		writeError(w, e)
		return
	}
	writeData(w, http.StatusOK, out)
}

type updateAccountRequest struct {
	Name *string `json:"name"`
}

// cleanName trims a name and checks it: 1 to max characters, no control
// characters.
func cleanName(raw string, max int) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > max {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

// PATCH /v2/me/account
func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	if h.accountsOff(w) {
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountRename, true)
	if !ok {
		return
	}
	var req updateAccountRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	name, valid := "", req.Name != nil
	if valid {
		name, valid = cleanName(*req.Name, 80)
	}
	if !valid {
		writeError(w, invalidRequest("name", "Send a name of 1 to 80 characters."))
		return
	}
	if err := h.accounts.SetAccountName(r.Context(), p.actor.ID, name); err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out, e := h.account(r, p.actor.ID)
	if e != nil {
		writeError(w, e)
		return
	}
	writeData(w, http.StatusOK, out)
}

type forgetAccountRequest struct {
	Confirm string `json:"confirm"`
}

// POST /v2/me/account:forget
func (h *Handler) forgetAccount(w http.ResponseWriter, r *http.Request) {
	if h.accountsOff(w) {
		return
	}
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if _, e := p.command(r); e != nil {
		writeError(w, e)
		return
	}
	var req forgetAccountRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	// The typed email first, so a slip doesn't cost a passkey check.
	if p.actor.Kind == policy.ActorPerson {
		_, email, _, err := h.accounts.AccountProfile(r.Context(), p.actor.ID)
		if err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
		if email == "" || !strings.EqualFold(strings.TrimSpace(req.Confirm), strings.TrimSpace(email)) {
			writeError(w, invalidRequest("confirm", "Type your account's email exactly to forget your account."))
			return
		}
	}
	if d := policy.DecideAccount(accountActor(p), policy.AccountForget, false); d.Effect == policy.EffectRefuse {
		h.writeRefusal(w, r, d)
		return
	}
	person := p.actor.ID
	forgot, err := h.ledger.ForgetAccountAs(r.Context(), p.actor, p.via)
	var refused *ledger.RefusedError
	if errors.As(err, &refused) {
		h.writeRefusal(w, r, refused.Decision)
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := accountForgotten{Spaces: len(forgot.Spaces), Agents: len(forgot.Disconnected), TeamSpacesKept: len(forgot.Kept)}
	if h.passkeys != nil {
		if out.Passkeys, err = h.passkeys.RemoveAll(r.Context(), person); err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
	}
	if h.sessions != nil {
		current, _ := uuid.Parse(handler.GetGrant(r).SessionID)
		if out.Sessions, err = h.sessions.RevokeOthers(r.Context(), person, current); err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
		if current != uuid.Nil {
			if _, err := h.sessions.Revoke(r.Context(), person, current, sessions.ReasonSignedOut); err == nil {
				out.Sessions++
			} else if !errors.Is(err, sessions.ErrNotFound) {
				writeError(w, h.fromLedger(r, err))
				return
			}
		}
	}
	h.log.InfoContext(r.Context(), "v2: a person forgot their account", "user_id", person.String(),
		"spaces", out.Spaces, "agents", out.Agents, "sessions", out.Sessions, "passkeys", out.Passkeys,
		"assurance", string(accountActor(p).Assurance()))
	writeData(w, http.StatusOK, out)
}

// signInProvider is the {method} of a sign-in method route.
func signInProvider(r *http.Request) (string, *apiError) {
	switch m := r.PathValue("method"); m {
	case "github", "google":
		return m, nil
	}
	return "", &apiError{status: http.StatusNotFound, code: codeNotFound,
		message: "Connect GitHub or Google here; your email always signs you in, and passkeys have their own place."}
}

type connectSignInRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type signInRedirect struct {
	URL string `json:"url"`
}

// POST /v2/me/sign-in-methods/{method}:connect
func (h *Handler) connectSignInMethod(w http.ResponseWriter, r *http.Request) {
	if h.accountsOff(w) {
		return
	}
	provider, e := signInProvider(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountConnectSignIn, true)
	if !ok {
		return
	}
	var req connectSignInRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if !h.accounts.AllowedRedirect(req.RedirectURI) {
		writeError(w, invalidRequest("redirect_uri", "Send a redirect_uri on the web app."))
		return
	}
	url, err := h.accounts.AccountLinkURL(r.Context(), p.actor.ID, provider, req.RedirectURI)
	if errors.Is(err, handler.ErrProviderUnavailable) {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "That sign-in provider isn't configured on this server."})
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, signInRedirect{URL: url})
}

// POST /v2/me/sign-in-methods/{method}:disconnect
func (h *Handler) disconnectSignInMethod(w http.ResponseWriter, r *http.Request) {
	if h.accountsOff(w) {
		return
	}
	provider, e := signInProvider(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountDisconnectSignIn, true)
	if !ok {
		return
	}
	_, _, ids, err := h.accounts.AccountProfile(r.Context(), p.actor.ID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	linked := false
	for _, id := range ids {
		linked = linked || id.Provider == provider
	}
	if !linked {
		writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "That provider isn't a way you sign in."})
		return
	}
	if err := h.accounts.AccountUnlink(r.Context(), p.actor.ID, provider); handler.IsLastIdentity(err) {
		writeError(w, &apiError{status: http.StatusConflict, code: codeInvalidTransition,
			message: "That's your last way to sign in with a provider. Connect another first."})
		return
	} else if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out, e := h.account(r, p.actor.ID)
	if e != nil {
		writeError(w, e)
		return
	}
	writeData(w, http.StatusOK, out)
}

// GET /v2/me/passkeys
func (h *Handler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountView, false)
	if !ok {
		return
	}
	list, err := h.passkeys.List(r.Context(), p.actor.ID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := passkeyList{Items: make([]passkeyView, 0, len(list))}
	for _, pk := range list {
		out.Items = append(out.Items, toPasskey(pk))
	}
	writeData(w, http.StatusOK, out)
}

// passkeyFailure is a ceremony's refusal: 403 passkey_invalid (or what the
// reason means to the endpoint).
func passkeyFailure(status int, err error, limitConflicts bool) *apiError {
	reason := passkeys.ReasonOf(err)
	switch {
	case reason == passkeys.ReasonLimit && limitConflicts:
		return &apiError{status: http.StatusConflict, code: codeInvalidTransition,
			message: "You have 20 passkeys, the most an account holds. Remove one you no longer use first."}
	case reason == passkeys.ReasonLimit:
		return &apiError{status: status, code: codePasskeyInvalid, details: &errorDetails{PasskeyFailure: reason},
			message: "You have 20 passkeys, the most an account holds. Remove one you no longer use first."}
	case reason == passkeys.ReasonExists:
		return &apiError{status: status, code: codePasskeyInvalid, details: &errorDetails{PasskeyFailure: reason},
			message: "That passkey is on your account already."}
	}
	return &apiError{status: status, code: codePasskeyInvalid, message: passkeyFailureMessage(reason),
		details: &errorDetails{PasskeyFailure: reason}}
}

// POST /v2/me/passkey-registrations
func (h *Handler) startPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountAddPasskey, true)
	if !ok {
		return
	}
	sid, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	opts, expires, err := h.passkeys.BeginRegistration(r.Context(), p.actor.ID, sid)
	if passkeys.ReasonOf(err) != "" {
		writeError(w, passkeyFailure(http.StatusForbidden, err, true))
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, passkeyRegistration{Options: opts, ExpiresAt: expires})
}

type addPasskeyRequest struct {
	Credential json.RawMessage `json:"credential"`
	Name       string          `json:"name"`
}

// POST /v2/me/passkeys
func (h *Handler) addPasskey(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	// Starting the registration decided who may add one; finishing it is
	// bound to that session's challenge, and happens on the web.
	p, ok := h.accountStart(w, r, policy.AccountRenamePasskey, true)
	if !ok {
		return
	}
	var req addPasskeyRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if len(req.Credential) == 0 {
		writeError(w, invalidRequest("credential", "Send the browser's answer as credential."))
		return
	}
	if utf8.RuneCountInString(req.Name) > passkeys.MaxNameRunes {
		writeError(w, invalidRequest("name", "Name a passkey in 64 characters or fewer."))
		return
	}
	sid, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	pk, err := h.passkeys.FinishRegistration(r.Context(), p.actor.ID, sid, req.Credential, req.Name)
	if passkeys.ReasonOf(err) != "" {
		writeError(w, passkeyFailure(http.StatusForbidden, err, false))
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	h.log.InfoContext(r.Context(), "v2: a passkey was added", "user_id", p.actor.ID.String(), "passkey_id", pk.ID.String(),
		"provider", pk.Provider)
	writeData(w, http.StatusCreated, toPasskey(*pk))
}

type renamePasskeyRequest struct {
	Name *string `json:"name"`
}

func passkeyPath(r *http.Request) (uuid.UUID, *apiError) {
	id, err := uuid.Parse(r.PathValue("passkey"))
	if err != nil {
		return uuid.Nil, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "No passkey like that is yours. List them with GET /v2/me/passkeys."}
	}
	return id, nil
}

var passkeyNotFound = &apiError{status: http.StatusNotFound, code: codeNotFound,
	message: "No passkey like that is yours. List them with GET /v2/me/passkeys."}

// PATCH /v2/me/passkeys/{passkey}
func (h *Handler) renamePasskey(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	id, e := passkeyPath(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountRenamePasskey, true)
	if !ok {
		return
	}
	var req renamePasskeyRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	name, valid := "", req.Name != nil
	if valid {
		name, valid = passkeys.CleanName(*req.Name)
	}
	if !valid {
		writeError(w, invalidRequest("name", "Name a passkey in 1 to 64 characters."))
		return
	}
	pk, err := h.passkeys.Rename(r.Context(), p.actor.ID, id, name)
	if errors.Is(err, passkeys.ErrNotFound) {
		writeError(w, passkeyNotFound)
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, toPasskey(*pk))
}

// POST /v2/me/passkeys/{passkey}:remove
func (h *Handler) removePasskey(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	id, e := passkeyPath(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p, ok := h.accountStart(w, r, policy.AccountRemovePasskey, true)
	if !ok {
		return
	}
	pk, err := h.passkeys.Remove(r.Context(), p.actor.ID, id)
	if errors.Is(err, passkeys.ErrNotFound) {
		writeError(w, passkeyNotFound)
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	h.log.InfoContext(r.Context(), "v2: a passkey was removed", "user_id", p.actor.ID.String(), "passkey_id", pk.ID.String())
	writeData(w, http.StatusOK, toPasskey(*pk))
}

// POST /v2/passkey-sign-ins (public)
func (h *Handler) startPasskeySignIn(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) {
		return
	}
	opts, expires, err := h.passkeys.BeginSignIn(r.Context())
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, passkeyChallenge{Options: opts, ExpiresAt: expires})
}

type finishPasskeySignInRequest struct {
	Credential json.RawMessage `json:"credential"`
}

// POST /v2/passkey-sign-ins:finish (public)
func (h *Handler) finishPasskeySignIn(w http.ResponseWriter, r *http.Request) {
	if h.passkeysOff(w) || h.accountsOff(w) {
		return
	}
	var req finishPasskeySignInRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if len(req.Credential) == 0 {
		writeError(w, invalidRequest("credential", "Send the browser's answer as credential."))
		return
	}
	person, pk, err := h.passkeys.FinishSignIn(r.Context(), req.Credential)
	if reason := passkeys.ReasonOf(err); reason != "" {
		h.log.InfoContext(r.Context(), "v2: refused a passkey sign-in", "reason", string(reason))
		e := passkeyFailure(http.StatusUnauthorized, err, false)
		if reason == passkeys.ReasonNoCredential {
			e.message = "That passkey isn't on a Memax account. Sign in another way, then add it in Settings › Account."
		}
		writeError(w, e)
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	code, ttl, err := h.accounts.WebSignInCode(r.Context(), person)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	h.log.InfoContext(r.Context(), "v2: a person signed in with a passkey", "user_id", person.String(), "passkey_id", pk.ID.String())
	writeData(w, http.StatusOK, passkeySignIn{Code: code, ExpiresIn: int(ttl / time.Second)})
}
