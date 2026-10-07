package v2api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// GET /v2/memories/{ref}/conflict
//
// ReviewConflict: both sides and the four answers, with what the caller
// may take.
func (h *Handler) getConflict(w http.ResponseWriter, r *http.Request) {
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
	with := strings.TrimSpace(r.URL.Query().Get("with"))
	if with != "" {
		if _, err := uuid.Parse(with); err != nil {
			if _, _, ok := ledger.ParseRef(with); !ok {
				writeError(w, invalidRequest("with", "Use a display ID like M-0174 or a memory id."))
				return
			}
		}
	}
	v, err := h.ledger.GetConflict(r.Context(), scope, p.actor, p.via, ref, with)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	v.Receipts = nonNil(v.Receipts)
	for i := range v.Options {
		v.Options[i].Effects = nonNil(v.Options[i].Effects)
	}
	writeData(w, http.StatusOK, v)
}

// POST /v2/memories/{ref}:resolve-conflict
func (h *Handler) resolveConflict(w http.ResponseWriter, r *http.Request) {
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
	var req resolveConflictRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.ResolveConflict{
		Meta: p.meta(scope, key, req.commandFields), Memory: ref, Other: req.Other, Choice: req.Choice,
		ExpectedVersion: version, Statement: req.Statement, OtherStatement: req.OtherStatement,
	})
	h.writeMemoriesResult(w, r, res, err)
}

// POST /v2/receipts/{receipt}:undo
//
// Addressed by receipt, not by memory: an undo reverses one command, and
// a command is named exactly by its receipts (every command result and
// Activity row carries them). A conflict resolution or a judge fold
// changes two memories, so a memory-addressed undo would have to guess
// which command was meant, and could undo someone else's later change.
func (h *Handler) undoReceipt(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, err := uuid.Parse(r.PathValue("receipt"))
	if err != nil {
		writeError(w, invalidRequest("receipt", "Use the receipt's id, from a command's result or Activity."))
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.Undo{Meta: p.meta(p.scope, key, req.commandFields), Receipt: id})
	h.writeMemoriesResult(w, r, res, err)
}

// writeMemoriesResult answers a command that changed several memories.
func (h *Handler) writeMemoriesResult(w http.ResponseWriter, r *http.Request, res ledger.Result, err error) {
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Outcome == ledger.OutcomeRefused {
		h.writeRefusal(w, r, res.Policy)
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	setETag(w, res.Memory)
	writeData(w, http.StatusOK, memoriesResult{
		Outcome: res.Outcome, Policy: res.Policy, Memory: res.Memory,
		Memories: nonNil(res.Memories), Receipts: nonNil(res.Receipts),
	})
}
