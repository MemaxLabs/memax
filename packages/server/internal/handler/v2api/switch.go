package v2api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Switch to V2 (plan 25 §10, epic 2.8), notes (N-) and V1's Dream runs.

// GET /v2/spaces/{space}/switch
func (h *Handler) getSpaceSwitch(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	st, err := h.ledger.SwitchStatus(r.Context(), p.scope, g.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, st)
}

type switchRequest struct {
	To         string           `json:"to"`
	Kind       policy.SpaceKind `json:"kind"`
	Repository *string          `json:"repository"`
}

// POST /v2/spaces/{space}:switch
func (h *Handler) switchSpace(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req switchRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	var st *ledger.SpaceSwitch
	var err error
	switch req.To {
	case "", "v2":
		st, err = h.ledger.StartSwitch(r.Context(), p.actor, p.via, p.scope, g.SpaceID,
			ledger.SwitchOptions{Kind: req.Kind, Repository: req.Repository, Key: key})
	case "v1":
		st, err = h.ledger.SwitchBack(r.Context(), p.actor, p.via, p.scope, g.SpaceID, key)
	default:
		writeError(w, invalidRequest("to", "Use v2, or v1 to switch back."))
		return
	}
	if err != nil {
		writeError(w, h.spaceError(r, err))
		return
	}
	status := http.StatusOK
	if st.Background {
		status = http.StatusAccepted
	}
	writeData(w, status, st)
}

// spaceError maps CreateSpace's, StartSwitch's and SwitchBack's errors.
func (h *Handler) spaceError(r *http.Request, err error) *apiError {
	var refused *ledger.SpaceRefusedError
	var kind *ledger.SpaceKindError
	switch {
	case errors.As(err, &refused):
		return refusal(refused.Decision)
	case errors.As(err, &kind):
		return &apiError{status: http.StatusConflict, code: codeSpaceKind, message: kind.Error(),
			details: &errorDetails{Field: "kind"}}
	case errors.Is(err, ledger.ErrSlugTaken):
		return &apiError{status: http.StatusConflict, code: codeSlugTaken, message: err.Error(),
			details: &errorDetails{Field: "slug"}}
	}
	return h.fromLedger(r, err)
}

// GET /v2/spaces/{space}/v1-dream-runs
func (h *Handler) listV1DreamRuns(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	runs, err := h.ledger.ListV1DreamRuns(r.Context(), p.scope, g.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, struct {
		Items []ledger.V1DreamRun `json:"items"`
	}{Items: nonNil(runs)})
}

// GET /v2/spaces/{space}/notes
func (h *Handler) searchNotes(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, invalidRequest("limit", "Send a limit from 1 to 200."))
			return
		}
		limit = min(n, 200)
	}
	notes, err := h.ledger.SearchNotes(r.Context(), p.scope.Narrow(g.SpaceID), ledger.NoteQuery{
		SpaceIDs: nil, Text: r.URL.Query().Get("q"), Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, struct {
		Items []ledger.Note `json:"items"`
	}{Items: nonNil(notes)})
}

// GET /v2/spaces/{space}/notes/{note}
func (h *Handler) getNote(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	n, err := h.ledger.GetNote(r.Context(), p.scope, g.SpaceID, r.PathValue("note"))
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, n)
}

// GET /v2/spaces/{space}/notes/{note}/forget-preview
func (h *Handler) previewForgetNote(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	v, err := h.ledger.PreviewForgetNote(r.Context(), p.actor, p.via, p.scope, g.SpaceID, r.PathValue("note"))
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, v)
}

type forgetNoteRequest struct {
	Note    string   `json:"note"`
	Carries []string `json:"carries"`
}

type noteForgetResult struct {
	Outcome   ledger.Outcome    `json:"outcome"`
	Policy    policy.Decision   `json:"policy"`
	Receipts  []ledger.Receipt  `json:"receipts"`
	Tombstone *ledger.Tombstone `json:"tombstone"`
	Memories  []ledger.Memory   `json:"memories"`
}

// POST /v2/spaces/{space}/notes/{note}:forget
func (h *Handler) forgetNote(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req forgetNoteRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	meta := p.meta(p.scope.Narrow(g.SpaceID), key, commandFields{})
	res, err := h.ledger.Apply(r.Context(), &ledger.ForgetNote{Meta: meta, SpaceID: g.SpaceID, Note: r.PathValue("note"),
		Remark: req.Note, Carries: req.Carries})
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
		writeError(w, refusal(res.Policy))
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeData(w, http.StatusOK, noteForgetResult{Outcome: res.Outcome, Policy: res.Policy, Receipts: nonNil(res.Receipts),
		Tombstone: res.Tombstone, Memories: nonNil(res.Memories)})
}
