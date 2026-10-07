package v2api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
)

// A person's sessions: everywhere they are signed in, and signing any of
// it out (plan 25 §5.15; internal/sessions). Sessions live outside the
// record, like device codes, so these handlers call internal/sessions and
// signing out writes no receipt. Who may do what is policy.DecideSession.

// WithSessions serves /v2/sessions from store; nil (the default) answers
// 503.
func WithSessions(store *sessions.Store) Option {
	return func(h *Handler) { h.sessions = store }
}

// session is the Session schema.
type session struct {
	ID         uuid.UUID     `json:"id"`
	Surface    sessions.Kind `json:"surface"`
	Client     string        `json:"client"`
	Agent      string        `json:"agent,omitempty"`
	Address    string        `json:"address,omitempty"`
	City       string        `json:"city,omitempty"`
	SignedInAt time.Time     `json:"signed_in_at"`
	LastUsedAt time.Time     `json:"last_used_at"`
	ExpiresAt  time.Time     `json:"expires_at"`
	RevokedAt  *time.Time    `json:"revoked_at,omitempty"`
	Current    bool          `json:"current"`
}

func toSession(s sessions.Session, current uuid.UUID) session {
	out := session{
		ID: s.ID, Surface: s.Kind, Client: s.Client, Address: s.LastIP, City: s.LastCity,
		SignedInAt: s.CreatedAt, LastUsedAt: s.LastUsedAt, ExpiresAt: s.ExpiresAt, RevokedAt: s.RevokedAt,
		Current: current != uuid.Nil && s.ID == current,
	}
	if s.Kind == sessions.KindMCP {
		out.Agent = s.AgentName
	}
	if out.Address == "" {
		out.Address, out.City = s.CreatedIP, s.CreatedCity
	}
	if out.Client == "" {
		out.Client = string(s.Kind)
	}
	return out
}

type sessionList struct {
	Items []session `json:"items"`
}

type sessionsRevoked struct {
	Revoked int `json:"revoked"`
}

// sessionCaller is the person asking and the session they ask from
// (uuid.Nil when their token doesn't name one: a sign-in from before
// sessions were named).
func (h *Handler) sessionCaller(w http.ResponseWriter, r *http.Request, act policy.SessionAction) (*principal, uuid.UUID, bool) {
	if h.sessions == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Sessions aren't configured on this server."})
		return nil, uuid.Nil, false
	}
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return nil, uuid.Nil, false
	}
	if act != policy.SessionList {
		if _, e := p.command(r); e != nil {
			writeError(w, e)
			return nil, uuid.Nil, false
		}
	}
	current, _ := uuid.Parse(handler.GetGrant(r).SessionID)
	actor := policy.Actor{Kind: p.actor.Kind, Credential: p.actor.Credential, Via: p.via}
	if d := policy.DecideSession(actor, act); d.Effect == policy.EffectRefuse {
		h.writeRefusal(w, r, d)
		return nil, uuid.Nil, false
	}
	return p, current, true
}

// GET /v2/sessions
func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	p, current, ok := h.sessionCaller(w, r, policy.SessionList)
	if !ok {
		return
	}
	list, err := h.sessions.List(r.Context(), p.actor.ID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := sessionList{Items: make([]session, 0, len(list))}
	for _, s := range list {
		out.Items = append(out.Items, toSession(s, current))
	}
	writeData(w, http.StatusOK, out)
}

// POST /v2/sessions/{session}:revoke
func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("session"))
	if err != nil {
		writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "No session like that is yours. List them with GET /v2/sessions."})
		return
	}
	// Signing out the session in hand is the person's own sign-out; any
	// other needs them on the web.
	act := policy.SessionRevoke
	if current, _ := uuid.Parse(handler.GetGrant(r).SessionID); current == id {
		act = policy.SessionRevokeOwn
	}
	p, current, ok := h.sessionCaller(w, r, act)
	if !ok {
		return
	}
	reason := sessions.ReasonRevoked
	if act == policy.SessionRevokeOwn {
		reason = sessions.ReasonSignedOut
	}
	ended, err := h.sessions.Revoke(r.Context(), p.actor.ID, id, reason)
	if errors.Is(err, sessions.ErrNotFound) {
		writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "No live session like that is yours: it may have ended already. List them with GET /v2/sessions."})
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	h.log.InfoContext(r.Context(), "v2: a session was signed out", "session_id", id.String(),
		"user_id", p.actor.ID.String(), "own", act == policy.SessionRevokeOwn, "surface", string(ended.Kind))
	writeData(w, http.StatusOK, toSession(*ended, current))
}

// POST /v2/sessions:revoke-others
func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	p, current, ok := h.sessionCaller(w, r, policy.SessionRevoke)
	if !ok {
		return
	}
	if current == uuid.Nil {
		writeError(w, &apiError{status: http.StatusConflict, code: codeInvalidTransition,
			message: "Memax can't tell which session this is: it signed in before sessions were named. Sign in again, then sign the others out."})
		return
	}
	n, err := h.sessions.RevokeOthers(r.Context(), p.actor.ID, current)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	h.log.InfoContext(r.Context(), "v2: every other session was signed out", "user_id", p.actor.ID.String(), "revoked", n)
	writeData(w, http.StatusOK, sessionsRevoked{Revoked: n})
}
