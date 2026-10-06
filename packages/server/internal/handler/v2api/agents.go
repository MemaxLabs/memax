package v2api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The agent endpoints (epic 1.8): the Agents, AgentDetail and
// ConnectAgent screens. Connections are made by the credential flows
// (OAuth consent, `memax connect`) and the V1 backfill; here a person
// reads them and changes what each may do. Who may change what is
// policy.DecideConnection's, through the ledger, like every write.

type agentList struct {
	Items []ledger.Connection `json:"items"`
}

type agentDetail struct {
	Agent        *ledger.Connection    `json:"agent"`
	ThisWeek     ledger.AgentWeek      `json:"this_week"`
	RecentWrites []ledger.Receipt      `json:"recent_writes"`
	Sessions     []ledger.AgentSession `json:"sessions"`
}

type agentCommandResult struct {
	Outcome  ledger.Outcome     `json:"outcome"`
	Policy   policy.Decision    `json:"policy"`
	Agent    *ledger.Connection `json:"agent"`
	Receipts []ledger.Receipt   `json:"receipts"`
}

type autonomyRequest struct {
	Autonomy   policy.Autonomy `json:"autonomy"`
	Reason     string          `json:"reason"`
	OccurredAt *time.Time      `json:"occurred_at"`
}

type agentCommandRequest struct {
	Reason     string     `json:"reason"`
	OccurredAt *time.Time `json:"occurred_at"`
}

// GET /v2/agents
func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	conns, err := h.ledger.ListConnections(r.Context(), p.scope, ledger.ConnectionQuery{})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, agentList{Items: nonNil(p.visibleAgents(conns))})
}

// GET /v2/spaces/{space}/agents
func (h *Handler) listSpaceAgents(w http.ResponseWriter, r *http.Request) {
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
	conns, err := h.ledger.ListConnections(r.Context(), p.scope, ledger.ConnectionQuery{SpaceID: sp.SpaceID})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, agentList{Items: nonNil(p.visibleAgents(conns))})
}

// GET /v2/agents/{agent}
func (h *Handler) getAgent(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := agentID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if !p.mayReadAgent(id) {
		writeError(w, notFound)
		return
	}
	d, err := h.ledger.GetConnection(r.Context(), p.scope, id)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, agentDetail{
		Agent: d.Connection, ThisWeek: d.Week, RecentWrites: nonNil(d.RecentWrites), Sessions: nonNil(d.Sessions),
	})
}

// PATCH /v2/agents/{agent}/spaces/{space}
func (h *Handler) setAgentAutonomy(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := agentID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req autonomyRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.SetAutonomy{
		Meta:       p.meta(p.scope, key, commandFields{Reason: req.Reason, OccurredAt: req.OccurredAt}),
		Connection: id, SpaceID: sp.SpaceID, Autonomy: req.Autonomy,
	})
	h.writeAgentResult(w, r, res, err)
}

// POST /v2/agents/{agent}:pause
func (h *Handler) pauseAgent(w http.ResponseWriter, r *http.Request) {
	h.agentCommand(w, r, func(m ledger.Meta, id uuid.UUID) ledger.Command {
		return &ledger.PauseAgent{Meta: m, Connection: id}
	})
}

// POST /v2/agents/{agent}:resume
func (h *Handler) resumeAgent(w http.ResponseWriter, r *http.Request) {
	h.agentCommand(w, r, func(m ledger.Meta, id uuid.UUID) ledger.Command {
		return &ledger.ResumeAgent{Meta: m, Connection: id}
	})
}

// POST /v2/agents/{agent}:disconnect
func (h *Handler) disconnectAgent(w http.ResponseWriter, r *http.Request) {
	h.agentCommand(w, r, func(m ledger.Meta, id uuid.UUID) ledger.Command {
		return &ledger.DisconnectAgent{Meta: m, Connection: id}
	})
}

// agentCommand is pause, resume and disconnect: an optional body.
func (h *Handler) agentCommand(w http.ResponseWriter, r *http.Request, build func(ledger.Meta, uuid.UUID) ledger.Command) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := agentID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req agentCommandRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	m := p.meta(p.scope, key, commandFields{Reason: req.Reason, OccurredAt: req.OccurredAt})
	res, err := h.ledger.Apply(r.Context(), build(m, id))
	h.writeAgentResult(w, r, res, err)
}

// writeAgentResult answers an agent command like writeResult answers a
// memory command.
func (h *Handler) writeAgentResult(w http.ResponseWriter, r *http.Request, res ledger.Result, err error) {
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
	writeData(w, http.StatusOK, agentCommandResult{
		Outcome: res.Outcome, Policy: res.Policy, Agent: res.Connection, Receipts: nonNil(res.Receipts),
	})
}

func agentID(r *http.Request) (uuid.UUID, *apiError) {
	id, err := uuid.Parse(r.PathValue("agent"))
	if err != nil {
		return uuid.Nil, invalidRequest("agent", "Use the agent connection's id.")
	}
	return id, nil
}

// An agent sees itself, not the other agents of the person it works for:
// a credential bound to some spaces shouldn't learn about the rest.
func (p *principal) visibleAgents(cs []ledger.Connection) []ledger.Connection {
	if p.actor.Kind != policy.ActorAgent {
		return cs
	}
	out := []ledger.Connection{}
	for _, c := range cs {
		if p.connection != nil && c.ID == p.connection.ID {
			out = append(out, c)
		}
	}
	return out
}

func (p *principal) mayReadAgent(id uuid.UUID) bool {
	return p.actor.Kind != policy.ActorAgent || (p.connection != nil && p.connection.ID == id)
}
