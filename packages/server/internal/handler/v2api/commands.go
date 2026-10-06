package v2api

import (
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// POST /v2/spaces/{space}/memories
//
// A person remembers; an agent proposes. Either way policy decides the
// outcome (a viewer's remember is proposed, an agent at Write would be
// kept), not the client.
func (h *Handler) remember(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req rememberRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	meta := p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields)
	nm := req.newMemory(sp.SpaceID)
	var cmd ledger.Command = &ledger.Propose{Meta: meta, NewMemory: nm}
	if p.actor.Kind == policy.ActorPerson {
		cmd = &ledger.Remember{Meta: meta, NewMemory: nm}
	}
	res, err := h.ledger.Apply(r.Context(), cmd)
	if err == nil && res.Memory != nil {
		w.Header().Set("Location", "/v2/memories/"+res.Memory.ID.String())
	}
	h.writeResult(w, r, http.StatusCreated, res, err)
}

// POST /v2/memories/{ref}:keep
func (h *Handler) keep(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, func(m ledger.Meta, ref string, version int) ledger.Command {
		return &ledger.Keep{Meta: m, Memory: ref, ExpectedVersion: version}
	})
}

// POST /v2/memories/{ref}:reject
func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, func(m ledger.Meta, ref string, version int) ledger.Command {
		return &ledger.Reject{Meta: m, Memory: ref, ExpectedVersion: version}
	})
}

// review is keep and reject: an optional If-Match and an optional body.
func (h *Handler) review(w http.ResponseWriter, r *http.Request, build func(ledger.Meta, string, int) ledger.Command) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, _, e := ifMatch(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), build(p.meta(scope, key, req.commandFields), ref, version))
	h.writeResult(w, r, http.StatusOK, res, err)
}

// POST /v2/memories/{ref}:edit
func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
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
			message: `Send If-Match with the version you started from (the memory's ETag, such as "3"), so an edit never overwrites words you haven't seen.`})
		return
	}
	var req editRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.Edit{
		Meta: p.meta(scope, key, req.commandFields), Memory: ref, ExpectedVersion: version,
		Statement: req.Statement, Section: req.Section, Keep: req.Keep,
	})
	h.writeResult(w, r, http.StatusOK, res, err)
}

func (h *Handler) commandStart(r *http.Request) (*principal, string, *apiError) {
	p, e := h.principalFor(r)
	if e != nil {
		return nil, "", e
	}
	key, e := p.command(r)
	if e != nil {
		return nil, "", e
	}
	return p, key, nil
}

// writeResult answers a command: the result, or the refusal as 403 with
// the policy decision. A replay answers exactly as the original did, plus
// Idempotent-Replayed.
func (h *Handler) writeResult(w http.ResponseWriter, r *http.Request, status int, res ledger.Result, err error) {
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Outcome == ledger.OutcomeRefused {
		writeError(w, refusal(res.Policy))
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	setETag(w, res.Memory)
	writeData(w, status, commandResult{
		Outcome: res.Outcome, Policy: res.Policy, Memory: res.Memory, Receipts: nonNil(res.Receipts),
	})
}
