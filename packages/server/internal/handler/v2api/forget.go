package v2api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Forget (rule 7), its tombstones, agents' forget requests and the
// notices agents are told (plan 25 §5.13).

type forgetRequest struct {
	Note       string     `json:"note"`
	Carries    []string   `json:"carries"`
	OccurredAt *time.Time `json:"occurred_at"`
	SessionRef string     `json:"session_ref"`
}

type forgetResult struct {
	Outcome   ledger.Outcome    `json:"outcome"`
	Policy    policy.Decision   `json:"policy"`
	Memory    *ledger.Memory    `json:"memory"`
	Receipts  []ledger.Receipt  `json:"receipts"`
	Tombstone *ledger.Tombstone `json:"tombstone"`
	Memories  []ledger.Memory   `json:"memories"`
}

type forgetRequestResult struct {
	Outcome       ledger.Outcome        `json:"outcome"`
	Policy        policy.Decision       `json:"policy"`
	Memory        *ledger.Memory        `json:"memory"`
	Receipts      []ledger.Receipt      `json:"receipts"`
	ForgetRequest *ledger.ForgetRequest `json:"forget_request"`
}

// POST /v2/memories/{ref}:forget
func (h *Handler) forgetMemory(w http.ResponseWriter, r *http.Request) {
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
			message: `Send If-Match with the version you saw (the memory's ETag, such as "3"), so nobody forgets words they haven't read.`})
		return
	}
	var req forgetRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	meta := p.meta(scope, key, commandFields{OccurredAt: req.OccurredAt, SessionRef: req.SessionRef})
	res, err := h.ledger.Apply(r.Context(), &ledger.Forget{Meta: meta, Memory: ref, ExpectedVersion: version,
		Note: req.Note, Carries: req.Carries})
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
	writeData(w, http.StatusOK, forgetResult{Outcome: res.Outcome, Policy: res.Policy, Memory: res.Memory,
		Receipts: nonNil(res.Receipts), Tombstone: res.Tombstone, Memories: nonNil(res.Memories)})
}

// POST /v2/memories/{ref}:request-forget
func (h *Handler) requestForget(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
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
	res, err := h.ledger.Apply(r.Context(), &ledger.RequestForget{Meta: p.meta(scope, key, req.commandFields), Memory: ref})
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
	writeData(w, http.StatusOK, forgetRequestResult{Outcome: res.Outcome, Policy: res.Policy, Memory: res.Memory,
		Receipts: nonNil(res.Receipts), ForgetRequest: res.ForgetRequest})
}

// POST /v2/memories/{ref}:decline-forget
func (h *Handler) declineForget(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, func(m ledger.Meta, ref string, _ int) ledger.Command {
		return &ledger.DeclineForget{Meta: m, Memory: ref}
	})
}

// GET /v2/memories/{ref}/tombstone
func (h *Handler) getTombstone(w http.ResponseWriter, r *http.Request) {
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
	t, err := h.ledger.GetTombstone(r.Context(), scope, ref)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, t)
}

// GET /v2/memories/{ref}/forget-preview
func (h *Handler) previewForget(w http.ResponseWriter, r *http.Request) {
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
	v, err := h.ledger.PreviewForget(r.Context(), scope, p.actor, p.via, ref)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, v)
}

// GET /v2/spaces/{space}/tombstones
func (h *Handler) listTombstones(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	page, err := h.ledger.ListTombstones(r.Context(), p.scope.Narrow(sp.SpaceID), ledger.TombstoneQuery{
		SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, page)
}

type noticeList struct {
	Notices []ledger.Notice `json:"notices"`
}

// GET /v2/notices
func (h *Handler) listNotices(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	out := noticeList{Notices: []ledger.Notice{}}
	if p.actor.Kind == policy.ActorAgent && p.connection != nil {
		ns, err := h.ledger.PendingNotices(r.Context(), p.scope, p.connection.ID)
		if err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
		out.Notices = nonNil(ns)
	}
	writeData(w, http.StatusOK, out)
}

type ackNoticesRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

type ackNoticesResult struct {
	Acknowledged int `json:"acknowledged"`
}

// POST /v2/notices:ack
func (h *Handler) ackNotices(w http.ResponseWriter, r *http.Request) {
	p, _, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req ackNoticesRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 100 {
		writeError(w, invalidRequest("ids", "Send 1 to 100 notice ids."))
		return
	}
	out := ackNoticesResult{}
	if p.actor.Kind == policy.ActorAgent && p.connection != nil {
		n, err := h.ledger.AckNotices(r.Context(), p.scope, p.connection.ID, req.IDs, string(p.via))
		if err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
		out.Acknowledged = n
	}
	writeData(w, http.StatusOK, out)
}
