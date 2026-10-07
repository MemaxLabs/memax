package v2api

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
)

// Dream's editions (plan 25 §5.10, epic 2.2): read an edition and its
// actions, undo one (or every one of a kind), restore a faded memory, run
// Dream now, a person's Dream settings, and the morning email's one-click
// unsubscribe. Runs happen in the worker (internal/v2dream); here the
// API only reads and applies commands through the ledger, and queues a
// run-now.

// dreamDeps is what run-now needs: the plan's cap (the engine, without a
// model) and the queue it inserts into.
type dreamDeps struct {
	engine *v2dream.Engine
	jobs   ledger.Jobs
	// recent rate-limits run-now per space in this process, below the
	// plan's cap, so a double click queues one run.
	mu     sync.Mutex
	recent map[uuid.UUID]time.Time
	// zones remembers the zone last recorded per person, so a page load
	// writes nothing when it hasn't changed.
	zones sync.Map
}

// WithDream serves run-now (the engine for the plan's caps, jobs to queue
// the run). Without it run-now answers 503; reading editions, undo and
// the settings work regardless.
func WithDream(e *v2dream.Engine, jobs ledger.Jobs) Option {
	return func(h *Handler) { h.dream = &dreamDeps{engine: e, jobs: jobs, recent: map[uuid.UUID]time.Time{}} }
}

// runNowEvery is how often one space may be asked to run now, per process.
const runNowEvery = time.Minute

// editionView is the DreamEdition schema.
type editionView struct {
	ID          uuid.UUID                `json:"id"`
	Ref         string                   `json:"ref"`
	N           int64                    `json:"n"`
	SpaceID     uuid.UUID                `json:"space_id"`
	Slot        time.Time                `json:"slot"`
	Trigger     ledger.DreamTrigger      `json:"trigger"`
	RequestedBy *uuid.UUID               `json:"requested_by,omitempty"`
	Since       *time.Time               `json:"since,omitempty"`
	Until       time.Time                `json:"until"`
	StartedAt   time.Time                `json:"started_at"`
	FinishedAt  time.Time                `json:"finished_at"`
	Seconds     int                      `json:"seconds"`
	NotesRead   int                      `json:"notes_read"`
	NotesBy     []ledger.NoteAuthorCount `json:"notes_by"`
	NoteRefs    []string                 `json:"note_refs"`
	FactRefs    []string                 `json:"fact_refs"`
	Counts      map[string]int           `json:"counts"`
	Undone      int                      `json:"undone"`
	NeedsYou    int                      `json:"needs_you"`
	ReceiptID   uuid.UUID                `json:"receipt_id"`
	Actions     []ledger.DreamAction     `json:"actions,omitempty"`
	Surfaced    []ledger.SurfacedMemory  `json:"surfaced,omitempty"`
}

func toEditionView(e *ledger.DreamEdition, full bool) editionView {
	v := editionView{
		ID: e.ID, Ref: e.Ref, N: e.N, SpaceID: e.SpaceID, Slot: e.Slot, Trigger: e.Trigger, RequestedBy: e.RequestedBy,
		Since: e.Since, Until: e.Until, StartedAt: e.StartedAt, FinishedAt: e.FinishedAt,
		Seconds: int(e.FinishedAt.Sub(e.StartedAt).Round(time.Second) / time.Second), NotesRead: e.NotesRead,
		NotesBy: nonNil(e.NotesBy), NoteRefs: nonNil(e.NoteRefs), FactRefs: nonNil(e.FactRefs),
		Counts: map[string]int{}, Undone: e.Undone, NeedsYou: e.NeedsYou, ReceiptID: e.ReceiptID,
	}
	for _, k := range ledger.DreamActionKinds {
		v.Counts[string(k)] = e.Counts[k]
	}
	if full {
		v.Actions, v.Surfaced = nonNil(e.Actions), nonNil(e.Surfaced)
	}
	return v
}

type editionPage struct {
	Items      []editionView         `json:"items"`
	HasMore    bool                  `json:"has_more"`
	NextCursor string                `json:"next_cursor,omitempty"`
	Schedule   *ledger.DreamSchedule `json:"schedule,omitempty"`
}

type dreamActionPage struct {
	Items      []ledger.DreamAction `json:"items"`
	HasMore    bool                 `json:"has_more"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type dreamUndoResult struct {
	Outcome  ledger.Outcome      `json:"outcome"`
	Policy   policy.Decision     `json:"policy"`
	Action   *ledger.DreamAction `json:"action"`
	Memories []ledger.Memory     `json:"memories"`
	Receipts []ledger.Receipt    `json:"receipts"`
}

type undoEditionRequest struct {
	Kind   ledger.DreamActionKind `json:"kind"`
	Reason string                 `json:"reason"`
}

type dreamUndoRefusal struct {
	ActionID uuid.UUID `json:"action_id"`
	Reason   string    `json:"reason"`
	Message  string    `json:"message"`
	Ref      string    `json:"ref,omitempty"`
}

type undoEditionResult struct {
	Undone  []ledger.DreamAction `json:"undone"`
	Refused []dreamUndoRefusal   `json:"refused"`
}

type dreamRun struct {
	Queued  bool                `json:"queued"`
	Slot    time.Time           `json:"slot"`
	Trigger ledger.DreamTrigger `json:"trigger"`
}

type dreamSettingsRequest struct {
	TimeZone     *string `json:"time_zone"`
	MorningEmail *bool   `json:"morning_email"`
}

// observeZone records a person's X-Timezone, once per change.
func (h *Handler) observeZone(r *http.Request, p *principal) {
	zone := strings.TrimSpace(r.Header.Get("X-Timezone"))
	if zone == "" || p.actor.Kind != policy.ActorPerson || p.impersonated || p.scope.PersonID == uuid.Nil {
		return
	}
	if h.dream != nil {
		if last, ok := h.dream.zones.Load(p.scope.PersonID); ok && last == zone {
			return
		}
	}
	if err := h.ledger.ObserveTimeZone(r.Context(), p.scope, zone); err != nil {
		h.log.WarnContext(r.Context(), "v2: couldn't record the person's time zone", "error", err)
		return
	}
	if h.dream != nil {
		h.dream.zones.Store(p.scope.PersonID, zone)
	}
}

// GET /v2/spaces/{space}/dream/editions
func (h *Handler) listEditions(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	h.observeZone(r, p)
	res, err := h.ledger.ListEditions(r.Context(), p.scope, ledger.EditionQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := editionPage{Items: make([]editionView, 0, len(res.Editions)), HasMore: res.HasMore, NextCursor: res.NextCursor,
		Schedule: res.Schedule}
	for i := range res.Editions {
		out.Items = append(out.Items, toEditionView(&res.Editions[i], false))
	}
	writeData(w, http.StatusOK, out)
}

// GET /v2/spaces/{space}/dream/editions/{edition}
func (h *Handler) getEdition(w http.ResponseWriter, r *http.Request) {
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
	ed, err := h.ledger.GetEdition(r.Context(), p.scope, sp.SpaceID, r.PathValue("edition"))
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, toEditionView(ed, true))
}

// GET /v2/spaces/{space}/dream/editions/{edition}/actions
func (h *Handler) listDreamActions(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.ListDreamActions(r.Context(), p.scope, ledger.DreamActionQuery{SpaceID: sp.SpaceID,
		Edition: r.PathValue("edition"), Kind: ledger.DreamActionKind(r.URL.Query().Get("kind")), Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, dreamActionPage{Items: nonNil(res.Actions), HasMore: res.HasMore, NextCursor: res.NextCursor})
}

// POST /v2/dream/actions/{action}:undo
func (h *Handler) undoDreamAction(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, err := uuid.Parse(r.PathValue("action"))
	if err != nil {
		writeError(w, invalidRequest("action", "Use the action's id, from an edition."))
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.UndoDreamAction{Meta: p.meta(p.scope, key, req.commandFields), Action: id})
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
	writeData(w, http.StatusOK, dreamUndoResult{Outcome: res.Outcome, Policy: res.Policy, Action: res.DreamAction,
		Memories: nonNil(res.Memories), Receipts: nonNil(res.Receipts)})
}

// POST /v2/spaces/{space}/dream/editions/{edition}:undo
//
// Each action is its own command, keyed from the request's key and the
// action, so a retry replays what went through and tries the rest.
func (h *Handler) undoEdition(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req undoEditionRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if !req.Kind.Valid() {
		writeError(w, invalidRequest("kind", "Use fold, propose, dedupe, conflict, stale, fade or brief."))
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	page, err := h.ledger.ListDreamActions(r.Context(), p.scope, ledger.DreamActionQuery{SpaceID: sp.SpaceID,
		Edition: r.PathValue("edition"), Kind: req.Kind, Limit: ledger.MaxPageSize})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := undoEditionResult{Undone: []ledger.DreamAction{}, Refused: []dreamUndoRefusal{}}
	for _, a := range page.Actions {
		if a.Undone != nil {
			continue
		}
		meta := p.meta(p.scope.Narrow(sp.SpaceID), key+":"+a.ID.String(), commandFields{Reason: req.Reason})
		res, err := h.ledger.Apply(r.Context(), &ledger.UndoDreamAction{Meta: meta, Action: a.ID})
		var ue *ledger.UndoError
		switch {
		case errors.As(err, &ue):
			out.Refused = append(out.Refused, dreamUndoRefusal{ActionID: a.ID, Reason: ue.Reason, Message: ue.Error(), Ref: ue.Ref})
		case err != nil:
			writeError(w, h.fromLedger(r, err))
			return
		case res.Outcome == ledger.OutcomeRefused:
			out.Refused = append(out.Refused, dreamUndoRefusal{ActionID: a.ID, Reason: codeRefused, Message: res.Policy.Message})
		case res.DreamAction != nil:
			out.Undone = append(out.Undone, *res.DreamAction)
		}
	}
	writeData(w, http.StatusOK, out)
}

// POST /v2/memories/{ref}:restore
func (h *Handler) restoreMemory(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, func(m ledger.Meta, ref string, version int) ledger.Command {
		return &ledger.Restore{Meta: m, Memory: ref, ExpectedVersion: version}
	})
}

// POST /v2/spaces/{space}/dream:run
func (h *Handler) runDream(w http.ResponseWriter, r *http.Request) {
	p, _, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	if h.dream == nil || h.dream.engine == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Dream can't be run from this server. It runs on its own each night."})
		return
	}
	if p.impersonated {
		writeError(w, &apiError{status: http.StatusForbidden, code: codeImpersonation,
			message: "Impersonation sessions can read the record but not run Dream."})
		return
	}
	scope := p.scope.Narrow(sp.SpaceID)
	// Policy first (only the owner), then the caps.
	args, dec, err := h.ledger.QueueDreamRun(r.Context(), scope, p.actor, p.via, sp.SpaceID, nil)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if dec.Effect == policy.EffectRefuse {
		writeError(w, refusal(dec))
		return
	}
	now := h.now()
	h.dream.mu.Lock()
	last, recent := h.dream.recent[sp.SpaceID]
	if recent && now.Sub(last) < runNowEvery {
		h.dream.mu.Unlock()
		wait := int((runNowEvery - now.Sub(last)).Seconds()) + 1
		writeError(w, &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, retryAfter: wait,
			message: "Dream is already on its way for this space. Give it a minute.", details: &errorDetails{RetryAfter: wait}})
		return
	}
	h.dream.recent[sp.SpaceID] = now
	h.dream.mu.Unlock()
	ok, window, err := h.dream.engine.ManualAllowed(r.Context(), scope, sp.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if !ok {
		wait := int(window.Seconds())
		writeError(w, &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, retryAfter: wait,
			message: "Dream has run now as often as your plan allows for this space. It still runs on its own each night.",
			details: &errorDetails{RetryAfter: wait}})
		return
	}
	if args, _, err = h.ledger.QueueDreamRun(r.Context(), scope, p.actor, p.via, sp.SpaceID, h.dream.jobs); err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusAccepted, dreamRun{Queued: true, Slot: args.Slot, Trigger: args.Trigger})
}

// GET /v2/dream/settings
func (h *Handler) getDreamSettings(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if p.actor.Kind != policy.ActorPerson {
		writeError(w, &apiError{status: http.StatusForbidden, code: codePermissionDenied,
			message: "Dream settings are a person's. Sign in on the web or the CLI."})
		return
	}
	s, err := h.ledger.GetDreamSettings(r.Context(), p.scope)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, s)
}

// PATCH /v2/dream/settings
func (h *Handler) updateDreamSettings(w http.ResponseWriter, r *http.Request) {
	p, _, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if p.actor.Kind != policy.ActorPerson || p.impersonated {
		writeError(w, &apiError{status: http.StatusForbidden, code: codePermissionDenied,
			message: "Dream settings are a person's. Change them on the web or with the CLI."})
		return
	}
	var req dreamSettingsRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	s, err := h.ledger.UpdateDreamSettings(r.Context(), p.scope, req.TimeZone, req.MorningEmail)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if h.dream != nil {
		h.dream.zones.Delete(p.scope.PersonID)
	}
	writeData(w, http.StatusOK, s)
}

// POST /v2/dream/email:unsubscribe (public: the token is the credential)
func (h *Handler) unsubscribeDreamEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" || len(token) > 128 {
		writeError(w, invalidRequest("token", "Use the unsubscribe link from the email."))
		return
	}
	if err := h.ledger.UnsubscribeDreamEmail(r.Context(), token); err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"unsubscribed": true})
}
