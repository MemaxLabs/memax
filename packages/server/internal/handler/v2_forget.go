package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
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
func forgetV2Space(w http.ResponseWriter, r *http.Request, f V2Forgetter, userID, hubID string, retire bool) bool {
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
	if _, err := f.ForgetSpaceForV1(r.Context(), person, space, retire); err != nil {
		writeForgetError(w, err, "hub_id", hubID)
		return false
	}
	return true
}

// forgetV2Account forgets the V2 record of the spaces a person owns before
// V1 wipes their data (DELETE /v1/account/data). It answers false after
// writing the error response.
func forgetV2Account(w http.ResponseWriter, r *http.Request, f V2Forgetter, userID string) bool {
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
