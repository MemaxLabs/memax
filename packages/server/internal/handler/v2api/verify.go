package v2api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Verify (plan 25 §5.9, epic 3.1): a person's answer to a stale flag, and
// why the memory is stale (VerifyStale).

type verifyRequest struct {
	Answer     ledger.VerifyAnswer `json:"answer"`
	Statement  string              `json:"statement"`
	StaleAfter *time.Time          `json:"stale_after"`
	// Forget's, for answer forget: what goes with it, and the tombstone's note.
	Carries []string `json:"carries"`
	Note    string   `json:"note"`
	commandFields
}

// verifyResult is a command result, plus the tombstone and what went with
// it when the answer was forget.
type verifyResult struct {
	Outcome   ledger.Outcome    `json:"outcome"`
	Policy    policy.Decision   `json:"policy"`
	Memory    *ledger.Memory    `json:"memory"`
	Receipts  []ledger.Receipt  `json:"receipts"`
	Tombstone *ledger.Tombstone `json:"tombstone,omitempty"`
	Memories  []ledger.Memory   `json:"memories,omitempty"`
}

// POST /v2/memories/{ref}:verify
//
// still_true and update are the ledger's Verify; forget is Forget itself,
// with every one of its rules (who may, D15, the passkey re-check,
// carries), so answering it here and at :forget is the same command.
func (h *Handler) verifyMemory(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, ok, e := ifMatch(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if !ok {
		writeError(w, &apiError{status: http.StatusPreconditionRequired, code: codePreconditionRequired,
			message: `Send If-Match with the version you checked (the memory's ETag, such as "3"), so a verify never answers for words you haven't read.`})
		return
	}
	var req verifyRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if req.Answer == ledger.VerifyForget {
		if req.Statement != "" || req.StaleAfter != nil || req.Reason != "" {
			writeError(w, invalidRequest("answer", "Forget it takes only carries and a note for the tombstone."))
			return
		}
	} else if req.Note != "" || len(req.Carries) > 0 {
		writeError(w, invalidRequest("answer", "Only Forget it takes carries and a note."))
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	var cmd ledger.Command
	if req.Answer == ledger.VerifyForget {
		cmd = &ledger.Forget{Meta: p.meta(scope, key, commandFields{OccurredAt: req.OccurredAt, SessionRef: req.SessionRef}),
			Memory: ref, ExpectedVersion: version, Note: req.Note, Carries: req.Carries}
	} else {
		cmd = &ledger.Verify{Meta: p.meta(scope, key, req.commandFields), Memory: ref, ExpectedVersion: version,
			Answer: req.Answer, Statement: req.Statement, StaleAfter: req.StaleAfter}
	}
	res, err := h.ledger.Apply(r.Context(), cmd)
	var ce *ledger.ForgetCarriesError
	switch {
	case errors.As(err, &ce):
		writeError(w, &apiError{status: http.StatusConflict, code: codeForgetCarries, message: ce.Error(),
			details: &errorDetails{Ref: ce.Ref, Carries: nonNil(ce.Carries)}})
		return
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return
	case res.Outcome == ledger.OutcomeRefused:
		h.writeRefusal(w, r, res.Policy)
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	setETag(w, res.Memory)
	out := verifyResult{Outcome: res.Outcome, Policy: res.Policy, Memory: res.Memory, Receipts: nonNil(res.Receipts),
		Tombstone: res.Tombstone}
	if res.Tombstone != nil {
		out.Memories = nonNil(res.Memories)
	}
	writeData(w, http.StatusOK, out)
}

// GET /v2/memories/{ref}/stale
func (h *Handler) getStaleReport(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	rep, err := h.ledger.GetStaleReport(r.Context(), scope, ref)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(rep.Version)))
	writeData(w, http.StatusOK, rep)
	if g, ok := p.scope.Grant(rep.SpaceID); ok {
		h.recordRead(p, g, ledger.ReadGet, []uuid.UUID{rep.MemoryID})
	}
}
