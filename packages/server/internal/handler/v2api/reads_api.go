package v2api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reads (R-, plan 25 §5.3). The /v2 reads an agent makes (one memory, a
// page of memories) are handed to the read recorder after the response is
// built; they are never written on the request's path. A session-start
// compile load is written at once (it is idempotent by key), and Activity
// lists a space's reads.

// WithReads records agents' /v2 reads with rec (internal/reads). Without
// it (or with a nil one) nothing is recorded; compile loads and the read
// lists still work, since they talk to the ledger directly.
func WithReads(rec ledger.ReadRecorder) Option { return func(h *Handler) { h.reads = rec } }

type readPage struct {
	Items      []ledger.Read `json:"items"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Reads7d    int           `json:"reads_7d"`
}

type compileLoadRequest struct {
	Compile    string     `json:"compile"`
	Agent      string     `json:"agent"`
	SessionRef string     `json:"session_ref"`
	LoadedAt   *time.Time `json:"loaded_at"`
}

type compileLoadResult struct {
	Read *ledger.Read `json:"read"`
}

// GET /v2/spaces/{space}/reads
func (h *Handler) listReads(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.ListReads(r.Context(), p.scope, ledger.ReadQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, readPage{Items: nonNil(res.Reads), HasMore: res.HasMore, NextCursor: res.NextCursor, Reads7d: res.Week})
}

// POST /v2/spaces/{space}/compile-loads
func (h *Handler) recordCompileLoad(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req compileLoadRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	load := ledger.CompileLoad{Actor: p.actor, Scope: p.scope, Via: p.via, SpaceID: sp.SpaceID,
		Compile: req.Compile, Agent: req.Agent, SessionRef: req.SessionRef, IdempotencyKey: key}
	if req.LoadedAt != nil {
		load.LoadedAt = *req.LoadedAt
	}
	res, err := h.ledger.RecordCompileLoad(r.Context(), load)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Read == nil {
		h.writeRefusal(w, r, res.Policy)
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeData(w, http.StatusCreated, compileLoadResult{Read: res.Read})
}

// recordRead hands an agent's /v2 read of one space to the recorder. Only
// agents with a connection are counted, and only in spaces they are
// connected to (paused ones still read): a read never counts for a space
// its agent isn't connected to.
func (h *Handler) recordRead(p *principal, g ledger.SpaceGrant, kind ledger.ReadKind, memories []uuid.UUID) {
	if h.reads == nil || p.actor.Kind != policy.ActorAgent || p.connection == nil ||
		g.AgentStatus == policy.AgentNotConnected {
		return
	}
	h.reads.Record(ledger.ReadEvent{
		SpaceID: g.SpaceID, TenantID: g.TenantID, Reader: ledger.ReaderAgent, ConnectionID: p.connection.ID,
		PersonID: p.scope.PersonID, Agent: string(p.connection.Agent), Kind: kind, Via: p.via,
		Memories: memories, At: h.now(),
	})
}
