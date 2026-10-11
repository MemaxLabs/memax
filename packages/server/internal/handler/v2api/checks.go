package v2api

import (
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Stale checks (epic 3.1, step 3): a space's conditions for its machines
// to check, and a machine's report of what it found (ledger/checks.go).

type checkEvidenceInput struct {
	Change     string `json:"change"`
	Path       string `json:"path"`
	RenamedTo  string `json:"renamed_to"`
	Constraint string `json:"constraint"`
}

type checkResultInput struct {
	Memory    string              `json:"memory"`
	Condition string              `json:"condition"`
	Hash      string              `json:"hash"`
	Outcome   string              `json:"outcome"`
	Evidence  *checkEvidenceInput `json:"evidence"`
}

type checkRunRequest struct {
	Repository string             `json:"repository"`
	Branch     string             `json:"branch"`
	Commit     string             `json:"commit"`
	CommitAt   time.Time          `json:"commit_at"`
	CheckedAt  *time.Time         `json:"checked_at"`
	Trigger    string             `json:"trigger"`
	Agent      string             `json:"agent"`
	Device     string             `json:"device"`
	Complete   bool               `json:"complete"`
	Results    []checkResultInput `json:"results"`
}

type checkRunResult struct {
	Run     *ledger.CheckRun `json:"run"`
	Flagged []string         `json:"flagged"`
}

// GET /v2/spaces/{space}/conditions
func (h *Handler) listConditions(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	list, err := h.ledger.ListConditions(r.Context(), p.scope, sp.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, list)
}

// POST /v2/spaces/{space}/check-runs
func (h *Handler) reportChecks(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req checkRunRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	rep := ledger.CheckReport{Actor: p.actor, Scope: p.scope, Via: p.via, SpaceID: sp.SpaceID, IdempotencyKey: key,
		Repository: req.Repository, Branch: req.Branch, Commit: req.Commit, CommitAt: req.CommitAt,
		Trigger: ledger.CheckTrigger(req.Trigger), Agent: req.Agent, Device: req.Device, Complete: req.Complete}
	if req.CheckedAt != nil {
		rep.CheckedAt = *req.CheckedAt
	}
	for _, in := range req.Results {
		res := ledger.CheckResult{Memory: in.Memory, Condition: in.Condition, Hash: in.Hash, Outcome: in.Outcome}
		if in.Evidence != nil {
			res.Evidence = &ledger.CheckEvidence{Change: ledger.CheckChange(in.Evidence.Change), Path: in.Evidence.Path,
				RenamedTo: in.Evidence.RenamedTo, Constraint: in.Evidence.Constraint}
		}
		rep.Results = append(rep.Results, res)
	}
	res, err := h.ledger.ReportChecks(r.Context(), rep)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Run == nil {
		h.writeRefusal(w, r, res.Policy)
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeData(w, http.StatusCreated, checkRunResult{Run: res.Run, Flagged: res.Flagged})
}
