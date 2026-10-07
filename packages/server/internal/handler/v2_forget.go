package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
)

// V2Forgetter is the V2 record's side of V1's deletes (plan 25 §5.13): a
// hub with V2 records is forgotten and retired through the ledger before
// V1 deletes it, and a person's V2 record is forgotten before V1 wipes
// their data. *ledger.Ledger implements it; nil means V2 is off and V1
// deletes as it always did.
type V2Forgetter interface {
	SpaceHasRecord(ctx context.Context, spaceID uuid.UUID) (bool, error)
	ForgetSpaceForV1(ctx context.Context, person, spaceID uuid.UUID, retire bool) (bool, error)
	HasAccountRecord(ctx context.Context, person uuid.UUID) (bool, error)
	ForgetAccountRecord(ctx context.Context, person uuid.UUID, via policy.Via) (ledger.AccountForget, error)
}

// SetV2Forgetter wires the V2 record into hub deletion.
func (h *HubsHandler) SetV2Forgetter(f V2Forgetter) { h.v2 = f }

// PasskeyHolders says whether a person has a passkey (internal/passkeys).
// A person with one makes the decisions that need them with it, on the web
// (policy's assurance.go), and V1's routes can't ask for it: forgetting a
// V2 record through V1, and linking or unlinking a sign-in method, are
// refused for them and pointed at the web app. Nil means passkeys are off.
type PasskeyHolders interface {
	Has(ctx context.Context, person uuid.UUID) (bool, error)
}

// SetPasskeys turns that refusal on for V1's space delete.
func (h *HubsHandler) SetPasskeys(p PasskeyHolders) { h.passkeys = p }

// SetPasskeys turns that refusal on for V1's data wipe.
func (h *MemoriesHandler) SetPasskeys(p PasskeyHolders) { h.passkeys = p }

// refusedForPasskey answers 403 for a person with a passkey, whose change
// V1 can't check, and reports whether it did.
func refusedForPasskey(w http.ResponseWriter, r *http.Request, p PasskeyHolders, person uuid.UUID, where string) bool {
	if p == nil {
		return false
	}
	has, err := p.Has(r.Context(), person)
	if err != nil {
		slog.Error("v1: couldn't tell whether a person has a passkey", "error", err, "user_id", person.String())
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Memax couldn't check your passkeys. Try again in a moment.")
		return true
	}
	if has {
		writeError(w, http.StatusForbidden, "needs_passkey",
			"You have a passkey, so this asks for it. "+where+" on memax.app, where you can confirm with it.")
		return true
	}
	return false
}

// SetSpaceModes lets V1's push say when it saves into a space on V2
// (X-Memax-Warning: space_on_v2): the memory becomes a note there.
func (h *MemoriesHandler) SetSpaceModes(m *spacemode.Resolver) { h.modes = m }

// SetV2Forgetter wires the V2 record into DELETE /v1/account/data.
func (h *MemoriesHandler) SetV2Forgetter(f V2Forgetter) { h.v2 = f }

// isPersonSession reports whether the request carries a person's own
// session, not an API key, an OAuth grant or an agent token: only a
// person forgets a V2 record (policy.Decide refuses keys too; this answers
// before anything is touched).
func isPersonSession(r *http.Request) bool {
	g := GetGrant(r)
	return g.PrincipalType != "api_key" && g.PrincipalType != "oauth_grant" && g.AgentName == ""
}

// forgetV2Space forgets (and, deleting the hub, retires) a hub's V2
// record. It answers false after writing the error response; true means
// V1 may go on (the hub held no V2 record, or it is forgotten now).
func forgetV2Space(w http.ResponseWriter, r *http.Request, f V2Forgetter, pk PasskeyHolders, userID, hubID string, retire bool) bool {
	if f == nil {
		return true
	}
	person, err1 := uuid.Parse(userID)
	space, err2 := uuid.Parse(hubID)
	if err1 != nil || err2 != nil {
		return true
	}
	held, err := f.SpaceHasRecord(r.Context(), space)
	if err != nil {
		slog.Error("v2 forget: reading the space's record failed", "error", err, "hub_id", hubID)
		writeError(w, http.StatusInternalServerError, "forget_failed",
			"Couldn't check this space's record, so nothing was deleted. Try again.")
		return false
	}
	if !held {
		return true
	}
	if !isPersonSession(r) {
		writeError(w, http.StatusForbidden, "forget_needs_person",
			"This space holds a V2 record, which only its owner can forget. Delete it at memax.app.")
		return false
	}
	if refusedForPasskey(w, r, pk, person, "Forget this space's record") {
		return false
	}
	if _, err := f.ForgetSpaceForV1(r.Context(), person, space, retire); err != nil {
		writeForgetError(w, err, "hub_id", hubID)
		return false
	}
	return true
}

// forgetV2Account forgets the V2 record of the spaces a person owns before
// V1 wipes their data (DELETE /v1/account/data). It answers false after
// writing the error response.
func forgetV2Account(w http.ResponseWriter, r *http.Request, f V2Forgetter, pk PasskeyHolders, userID string) bool {
	if f == nil {
		return true
	}
	person, err := uuid.Parse(userID)
	if err != nil {
		return true
	}
	held, err := f.HasAccountRecord(r.Context(), person)
	if err != nil {
		slog.Error("v2 forget: reading the account's record failed", "error", err, "owner_id", userID)
		writeError(w, http.StatusInternalServerError, "forget_failed",
			"Couldn't check your record, so nothing was deleted. Try again.")
		return false
	}
	if !held {
		return true
	}
	if !isPersonSession(r) {
		writeError(w, http.StatusForbidden, "forget_needs_person",
			"Your spaces hold a V2 record, which only you can forget. Forget it at memax.app.")
		return false
	}
	if refusedForPasskey(w, r, pk, person, "Forget your record in Settings › Account") {
		return false
	}
	if _, err := f.ForgetAccountRecord(r.Context(), person, policy.ViaAPI); err != nil {
		writeForgetError(w, err, "owner_id", userID)
		return false
	}
	return true
}

func writeForgetError(w http.ResponseWriter, err error, logKey, logValue string) {
	var refused *ledger.RefusedError
	if errors.As(err, &refused) {
		writeError(w, http.StatusForbidden, "forget_refused", refused.Error())
		return
	}
	slog.Error("v2 forget failed", "error", err, logKey, logValue)
	writeError(w, http.StatusInternalServerError, "forget_failed",
		"Couldn't forget the V2 record, so nothing more was deleted. Try again: what was forgotten stays forgotten.")
}
