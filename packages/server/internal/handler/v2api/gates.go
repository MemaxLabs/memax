package v2api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The decision gate endpoints (epic 1.11). An agent asks, a person answers
// or someone withdraws; the ledger decides who may, and keeps the answer as
// the person's decision in the same transaction.

type gatePage struct {
	Items      []ledger.Gate `json:"items"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// gateResult is a gate command's answer (GateResult in v2.yaml).
type gateResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Gate     *ledger.Gate     `json:"gate"`
	Memory   *ledger.Memory   `json:"memory,omitempty"`
	Receipts []ledger.Receipt `json:"receipts"`
}

type requestDecisionRequest struct {
	Question  string                  `json:"question"`
	Context   string                  `json:"context"`
	Options   []ledger.DecisionOption `json:"options"`
	ExpiresAt *time.Time              `json:"expires_at"`
	commandFields
}

type answerGateRequest struct {
	Option int `json:"option"`
	commandFields
}

// GET /v2/spaces/{space}/gates
func (h *Handler) listGates(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	q := ledger.GateQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit}
	for _, s := range r.URL.Query()["status"] {
		q.Statuses = append(q.Statuses, ledger.GateStatus(s))
	}
	res, err := h.ledger.ListGates(r.Context(), p.scope, q)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, gatePage{Items: nonNil(res.Gates), HasMore: res.HasMore, NextCursor: res.NextCursor})
}

// POST /v2/spaces/{space}/gates
func (h *Handler) requestDecision(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req requestDecisionRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.RequestDecision{
		Meta: p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields), SpaceID: sp.SpaceID,
		Question: req.Question, Context: req.Context, Options: req.Options, ExpiresAt: req.ExpiresAt,
	})
	if err == nil && res.Gate != nil {
		w.Header().Set("Location", "/v2/gates/"+res.Gate.ID.String())
	}
	h.writeGateResult(w, r, http.StatusCreated, res, err)
}

// GET /v2/gates/{ref}
func (h *Handler) getGate(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.gateTarget(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	g, err := h.ledger.GetGate(r.Context(), scope, ref)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	setVersionETag(w, g.Version)
	writeData(w, http.StatusOK, g)
}

// POST /v2/gates/{ref}:answer
func (h *Handler) answerGate(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, _, e := ifMatchVersion(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req answerGateRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.gateTarget(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.AnswerGate{
		Meta: p.meta(scope, key, req.commandFields), Gate: ref, ExpectedVersion: version, Option: req.Option,
	})
	h.writeGateResult(w, r, http.StatusOK, res, err)
}

// POST /v2/gates/{ref}:withdraw
func (h *Handler) withdrawGate(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, _, e := ifMatchVersion(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.gateTarget(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.WithdrawGate{
		Meta: p.meta(scope, key, req.commandFields), Gate: ref, ExpectedVersion: version,
	})
	h.writeGateResult(w, r, http.StatusOK, res, err)
}

// gateTarget resolves the {ref} of /v2/gates/{ref} and its ?space=
// context, as target does for memories: a display ID needs its space.
func (h *Handler) gateTarget(r *http.Request, p *principal) (string, ledger.Scope, *apiError) {
	ref := r.PathValue("ref")
	spaceKey := r.URL.Query().Get("space")
	if _, err := uuid.Parse(ref); err != nil {
		if prefix, _, ok := ledger.ParseRef(ref); !ok || prefix != ledger.PrefixDecision {
			return "", ledger.Scope{}, invalidRequest("ref", "Use a gate's display ID like G-0012, or its id.")
		}
		if spaceKey == "" {
			return "", ledger.Scope{}, &apiError{status: http.StatusBadRequest, code: codeSpaceRequired,
				message: ref + " is unique only within its space. Add ?space= with the space's id or slug, or use the gate's id."}
		}
	}
	if spaceKey == "" {
		return ref, p.scope, nil
	}
	g, apiErr := h.space(r, p, spaceKey)
	if apiErr != nil {
		return "", ledger.Scope{}, apiErr
	}
	return ref, p.scope.Narrow(g.SpaceID), nil
}

// writeGateResult answers a gate command, the way writeResult does for a
// memory command.
func (h *Handler) writeGateResult(w http.ResponseWriter, r *http.Request, status int, res ledger.Result, err error) {
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
	if res.Gate != nil {
		setVersionETag(w, res.Gate.Version)
	}
	writeData(w, status, gateResult{
		Outcome: res.Outcome, Policy: res.Policy, Gate: res.Gate, Memory: res.Memory, Receipts: nonNil(res.Receipts),
	})
}
